package webapi

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

// mlbb_news answers what changed in Mobile Legends and what is coming, as
// the owners asked on 2026-11-25: asked which heroes the last patch buffed
// or when a skin comes, the bot searched the web five and six times over.
// Patches and events are MLBBDex's, which has a patch within days, laid
// out hero by hero; the leaks are the Mobile Legends Wiki's.

const (
	newsWikiService = "the Mobile Legends Wiki"
	mlbbNewsTTL     = 6 * time.Hour
	newsPageLimit   = 1 << 20
	// newsPatchesSearched are the patches a hero's changes are looked for in.
	newsPatchesSearched = 3
	shownPerSection     = 20
	shownIntro          = 300
	patchSource         = "MLBBDex (mlbbdex.com), from the official patch notes"
	// heroChangeNote keeps a hero's mixed change whole, as the owners saw a
	// nerfed ultimate told as "changed a little" under a buff on 2026-11-26.
	heroChangeNote = "Say for each skill whether it was a buff, nerf or adjust, as marked, with its numbers before → after; " +
		"never soften a nerf into \"changed\", and when the hero's change is a buff but a skill was nerfed, say so."
	eventsSource   = "MLBBDex (mlbbdex.com), from the Mobile Legends Wiki"
	upcomingSource = "the Mobile Legends Wiki (mobile-legends.fandom.com)"
	upcomingNote   = "Gathered by the wiki from leakers: none of it is confirmed, dates are unknown and it may change or be cancelled. Say so."
)

type mlbbNewsArgs struct {
	Action  string `json:"action"`
	Hero    string `json:"hero"`
	Version string `json:"version"`
}

func (a *apis) mlbbNewsTool() tools.Tool {
	return tools.Tool{
		Name: "mlbb_news",
		Description: "Mobile Legends news. patch: the latest patch's date, highlights, new or revamped heroes and the heroes buffed, nerfed or adjusted; with hero, its changes in the last patches; " +
			"with version (such as 2.2.16), that patch instead, alone. " +
			"events: this and next month's StarLight and Collector skins with dates. upcoming: what leakers say is coming, unconfirmed.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["patch", "events", "upcoming"]},
				"hero": {"type": "string", "description": "For patch: one hero's changes."},
				"version": {"type": "string", "description": "For patch: a patch's version, such as 2.2.16; left out, the latest."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Timeout:        patientTimeout,
		MaxOutputBytes: mlbbOutputBytes,
		Run:            a.mlbbNews,
	}
}

func (a *apis) mlbbNews(ctx context.Context, raw json.RawMessage) (any, error) {
	var args mlbbNewsArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	switch args.Action {
	case "patch":
		return a.mlbbPatch(ctx, strings.TrimSpace(args.Hero), patchVersion(args.Version))
	case "events":
		return a.mlbbEvents(ctx)
	case "upcoming":
		return a.mlbbUpcoming(ctx)
	}
	return nil, argumentError("action must be patch, events or upcoming")
}

func dexGet[T any](ctx context.Context, a *apis, path string) (T, error) {
	link := a.urls.MLBBDex + path
	return remember(a.cache, "dex-news:"+link, mlbbNewsTTL, func() (T, error) {
		var out struct {
			Data T `json:"data"`
		}
		err := a.patient.getJSON(ctx, call{service: dexService, url: link, limit: newsPageLimit}, &out)
		return out.Data, err
	})
}

type dexPatchEntry struct {
	Version string `json:"version"`
	Date    string `json:"date"`
}

type dexChange struct {
	Label  string `json:"label"`
	Before string `json:"before"`
	After  string `json:"after"`
	Text   string `json:"text"`
}

func (c dexChange) line() string {
	if c.Label != "" {
		return cleanText(c.Label+": "+c.Before+" → "+c.After, shownIntro)
	}
	return cleanText(c.Text, shownIntro)
}

type dexAdjustment struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Intro    string `json:"intro"`
	Sections []struct {
		Name     string      `json:"name"`
		Category string      `json:"category"`
		Type     string      `json:"type"`
		Changes  []dexChange `json:"changes"`
	} `json:"sections"`
}

type dexPatch struct {
	Version   string          `json:"version"`
	Date      string          `json:"date"`
	Sections  []dexSection    `json:"sections"`
	NewHeroes []dexNewHero    `json:"newHeroes"`
	Changes   []dexAdjustment `json:"adjustments"`
}

type dexSection struct {
	Title *string `json:"title"`
	HTML  string  `json:"html"`
}

type dexNewHero struct {
	Name    string `json:"name"`
	Epithet string `json:"epithet"`
	Feature string `json:"feature"`
}

// patchVersion is a version as players write it: "v2.2.16" or "Patch
// 2.2.16" is 2.2.16.
func patchVersion(version string) string {
	version = strings.ToLower(strings.TrimSpace(version))
	version = strings.TrimSpace(strings.TrimPrefix(version, "patch"))
	return strings.TrimPrefix(version, "v")
}

func (a *apis) mlbbPatch(ctx context.Context, hero, version string) (any, error) {
	patches, err := dexGet[[]dexPatchEntry](ctx, a, "/api/v1/patches")
	if err != nil {
		return nil, err
	}
	if len(patches) == 0 {
		return nil, argumentError("%s lists no patches", dexService)
	}
	searched := patches[:min(len(patches), newsPatchesSearched)]
	if version != "" {
		i := slices.IndexFunc(patches, func(p dexPatchEntry) bool { return strings.EqualFold(p.Version, version) })
		if i < 0 {
			return map[string]any{"version": version, "known": false, "latest": patches[0].Version,
				"note": "No such patch: say so, and that the latest is " + patches[0].Version + "."}, nil
		}
		patches, searched = patches[i:], patches[i:i+1]
	}
	if hero != "" {
		return a.heroInPatches(ctx, searched, hero)
	}
	latest, err := a.dexPatch(ctx, patches[0].Version)
	if err != nil {
		if version != "" {
			return map[string]any{"patch": patches[0].Version, "date": patches[0].Date, "notes": false,
				"note": "This patch's notes are not to be had: say so.", "source": patchSource}, nil
		}
		return nil, err
	}
	out := map[string]any{"patch": latest.Version, "date": latest.Date, "source": patchSource,
		"note": "For what changed for a hero, call again with hero."}
	if len(latest.Sections) > 0 && latest.Sections[0].Title == nil {
		out["highlights"] = cleanText(latest.Sections[0].HTML, 600)
	}
	var revamped []string
	for _, h := range latest.NewHeroes {
		revamped = append(revamped, strings.TrimSpace(h.Name+", "+h.Epithet))
	}
	if len(revamped) > 0 {
		out["new_or_revamped_heroes"] = revamped
	}
	var heroes []string
	for _, c := range latest.Changes {
		heroes = append(heroes, c.Name+": "+c.Type)
	}
	out["heroes"] = heroes
	if len(patches) > 1 {
		out["previous"] = patches[1].Version + " (" + patches[1].Date + ")"
	}
	return out, nil
}

func (a *apis) dexPatch(ctx context.Context, version string) (dexPatch, error) {
	return dexGet[dexPatch](ctx, a, "/api/v1/patches/"+url.PathEscape(version))
}

// heroInPatches looks for hero's changes in the patches searched, the
// latest first.
func (a *apis) heroInPatches(ctx context.Context, searched []dexPatchEntry, hero string) (any, error) {
	want := lettersOf(hero)
	for _, p := range searched {
		patch, err := a.dexPatch(ctx, p.Version)
		if err != nil {
			continue
		}
		for _, c := range patch.Changes {
			if lettersOf(c.Name) == want {
				out := map[string]any{"hero": c.Name, "patch": patch.Version, "date": patch.Date, "change": c.Type,
					"changes": adjustmentLines(c), "note": heroChangeNote, "source": patchSource}
				if why := cleanText(c.Intro, shownIntro); why != "" {
					out["why"] = why
				}
				return out, nil
			}
		}
		for _, h := range patch.NewHeroes {
			if strings.Contains(lettersOf(h.Name), want) {
				return map[string]any{"hero": h.Name, "patch": patch.Version, "date": patch.Date, "change": "revamp",
					"what": cleanText(h.Feature, shownIntro), "source": patchSource}, nil
			}
		}
	}
	var titles []string
	for _, p := range searched {
		titles = append(titles, p.Version+" ("+p.Date+")")
	}
	return map[string]any{"hero": hero, "changed": false, "patches_searched": titles,
		"note": "No change to this hero in these patches.", "source": patchSource}, nil
}

// adjustmentLines are a hero's changes, skill by skill: "Skill 2,
// Contract: Explosion [buff]: Cooldown: 12-9.5s → 8s at all levels".
func adjustmentLines(c dexAdjustment) []string {
	var lines []string
	for _, s := range c.Sections {
		var changes []string
		for _, ch := range s.Changes {
			changes = append(changes, ch.line())
		}
		head := strings.Trim(s.Category+", "+s.Name, ", ")
		lines = append(lines, head+" ["+s.Type+"]: "+strings.Join(changes, "; "))
	}
	return lines
}

type dexEvent struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Start string `json:"start"`
	End   string `json:"end"`
}

func (a *apis) mlbbEvents(ctx context.Context) (any, error) {
	events, err := dexGet[[]dexEvent](ctx, a, "/api/v1/events")
	if err != nil {
		return nil, err
	}
	var list []map[string]string
	for _, e := range events {
		list = append(list, map[string]string{"event": cleanText(e.Name, 120), "kind": e.Kind, "from": day(e.Start), "until": day(e.End)})
	}
	return map[string]any{"events": list, "source": eventsSource,
		"note": "Only the monthly StarLight and Collector skins; for other events say you do not know them."}, nil
}

// day is a timestamp's date, "2026-11-30".
func day(stamp string) string {
	if len(stamp) >= len("2006-01-02") {
		return stamp[:len("2006-01-02")]
	}
	return stamp
}

// wikiText is a wiki page's source.
func (a *apis) wikiText(ctx context.Context, page string) (string, error) {
	query := url.Values{"action": {"parse"}, "page": {page}, "prop": {"wikitext"}, "format": {"json"}, "redirects": {"1"}}
	link := a.urls.MLBBWiki + "/api.php?" + query.Encode()
	return remember(a.cache, "mlwiki:"+page, mlbbNewsTTL, func() (string, error) {
		var answer struct {
			Parse struct {
				Wikitext struct {
					Text string `json:"*"`
				} `json:"wikitext"`
			} `json:"parse"`
		}
		err := a.patient.getJSON(ctx, call{service: newsWikiService, url: link, limit: newsPageLimit}, &answer)
		if err == nil && answer.Parse.Wikitext.Text == "" {
			err = argumentError("%s has no page %q", newsWikiService, page)
		}
		return answer.Parse.Wikitext.Text, err
	})
}

var (
	wikiComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	wikiHeading = regexp.MustCompile(`^(=+)\s*(.*?)\s*=+\s*$`)
	galleryItem = regexp.MustCompile(`^File:[^|]*\|(.+)$`)
)

func (a *apis) mlbbUpcoming(ctx context.Context) (any, error) {
	text, err := a.wikiText(ctx, "Upcoming content")
	if err != nil {
		return nil, err
	}
	return map[string]any{"upcoming": upcomingSections(text), "note": upcomingNote, "source": upcomingSource}, nil
}

// upcomingSections are the page's galleries by their headings, those the
// wiki commented out left out.
func upcomingSections(text string) []map[string]any {
	sections := map[string][]string{}
	var order []string
	heading := ""
	for _, line := range strings.Split(wikiComment.ReplaceAllString(text, ""), "\n") {
		line = strings.TrimSpace(line)
		if m := wikiHeading.FindStringSubmatch(line); m != nil {
			heading = wikiPlain(m[2])
			continue
		}
		m := galleryItem.FindStringSubmatch(line)
		if m == nil || heading == "" {
			continue
		}
		item := wikiPlain(m[1])
		if item == "" {
			continue
		}
		if _, seen := sections[heading]; !seen {
			order = append(order, heading)
		}
		if len(sections[heading]) < shownPerSection {
			sections[heading] = append(sections[heading], item)
		}
	}
	upcoming := make([]map[string]any, 0, len(order))
	for _, h := range order {
		upcoming = append(upcoming, map[string]any{"section": h, "items": sections[h]})
	}
	return upcoming
}

var (
	wikiTemplate = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	wikiLinkText = regexp.MustCompile(`\[\[(?:[^|\]]*\|)?([^\]]+)\]\]`)
	wikiTag      = regexp.MustCompile(`<[^>]+>`)
)

// wikiPlain is wiki markup as plain text: "”'[[Dori]] (Mage)”'" becomes
// "Dori (Mage)".
func wikiPlain(text string) string {
	text = wikiTemplate.ReplaceAllString(text, "")
	text = wikiLinkText.ReplaceAllString(text, "$1")
	text = wikiTag.ReplaceAllString(text, " ")
	text = strings.NewReplacer("'''", "", "''", "").Replace(text)
	return strings.Join(strings.Fields(text), " ")
}
