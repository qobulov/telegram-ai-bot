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
	Section string `json:"section"`
}

func (a *apis) mlbbNewsTool() tools.Tool {
	return tools.Tool{
		Name: "mlbb_news",
		Description: "Mobile Legends news. patch: the latest patch's highlights, new or revamped heroes and heroes buffed, nerfed or adjusted; " +
			"hero: its changes in the last patches; version: that patch alone; section: a part of the notes, such as Battlefield or Brawl. " +
			"events: this and next month's StarLight and Collector skins. upcoming: leaks, unconfirmed.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["patch", "events", "upcoming"]},
				"hero": {"type": "string", "description": "For patch: one hero's changes."},
				"version": {"type": "string", "description": "For patch: a patch's version, such as 2.2.16; left out, the latest."},
				"section": {"type": "string", "description": "For patch: a part of its notes by title, as other_sections lists them."}
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
		return a.mlbbPatch(ctx, patchAsk{hero: strings.TrimSpace(args.Hero), version: patchVersion(args.Version), section: strings.TrimSpace(args.Section)})
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

// patchAsk is what mlbb_news is asked of a patch: a hero's changes, a
// version, a part of its notes.
type patchAsk struct {
	hero, version, section string
}

func (a *apis) mlbbPatch(ctx context.Context, ask patchAsk) (any, error) {
	hero, version := ask.hero, ask.version
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
	if ask.section != "" {
		return patchSection(latest, ask.section), nil
	}
	out := map[string]any{"patch": latest.Version, "date": latest.Date, "source": patchSource,
		"note": "For what changed for a hero, call again with hero; for a part of the notes, with section."}
	if len(latest.Sections) > 0 && latest.Sections[0].Title == nil {
		out["highlights"] = cleanText(latest.Sections[0].HTML, 600)
	}
	var revamped []string
	for _, h := range latest.NewHeroes {
		revamped = append(revamped, strings.TrimSpace(h.Name+", "+h.Epithet))
	}
	for _, r := range revampedHeroes(latest) {
		revamped = append(revamped, r.title)
	}
	if len(revamped) > 0 {
		out["new_or_revamped_heroes"] = revamped
	}
	var heroes []string
	for _, c := range latest.Changes {
		if line := c.Name + ": " + c.Type; !slices.Contains(heroes, line) {
			heroes = append(heroes, line)
		}
	}
	out["heroes"] = heroes
	if others := otherSections(latest); len(others) > 0 {
		out["other_sections"] = others
	}
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
		if c, ok := heroAdjustment(patch, want); ok {
			out := map[string]any{"hero": c.Name, "patch": patch.Version, "date": patch.Date, "change": c.Type,
				"changes": adjustmentLines(c), "note": heroChangeNote, "source": patchSource}
			if why := cleanText(c.Intro, shownIntro); why != "" {
				out["why"] = why
			}
			return out, nil
		}
		for _, h := range patch.NewHeroes {
			if strings.Contains(lettersOf(h.Name), want) {
				return map[string]any{"hero": h.Name, "patch": patch.Version, "date": patch.Date, "change": "revamp",
					"what": cleanText(h.Feature, shownIntro), "source": patchSource}, nil
			}
		}
		for _, r := range revampedHeroes(patch) {
			if strings.Contains(lettersOf(r.title), want) {
				return map[string]any{"hero": r.title, "patch": patch.Version, "date": patch.Date, "change": "revamp",
					"what": r.text, "note": heroChangeNote, "source": patchSource}, nil
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

// heroAdjustment is the patch's adjustment of the hero, the fullest when it
// is listed more than once: the notes list some heroes twice, once empty.
func heroAdjustment(patch dexPatch, want string) (dexAdjustment, bool) {
	var best dexAdjustment
	found := false
	for _, c := range patch.Changes {
		if lettersOf(c.Name) == want && (!found || len(c.Sections) > len(best.Sections)) {
			best, found = c, true
		}
	}
	return best, found
}

// A revamp is a hero remade in a patch, which the notes tell in the text of
// their Revamped Hero part, one heading a hero, and not hero by hero.
type revamp struct {
	title, text string
}

var (
	patchHeading = regexp.MustCompile(`(?s)<h3[^>]*>(.*?)</h3>`)
	// unshownSections are parts of the notes with nothing of their own.
	unshownSections = []string{"references", "undocumented changes", "other", "hero adjustments"}
)

const (
	shownRevamp  = 3000
	shownSection = 4000
)

func revampedHeroes(patch dexPatch) []revamp {
	var out []revamp
	for _, sec := range patch.Sections {
		if sec.Title == nil || !strings.Contains(strings.ToLower(*sec.Title), "revamped hero") {
			continue
		}
		for _, part := range headedParts(sec.HTML, shownRevamp) {
			if at := strings.LastIndex(part.title, "Hero:"); at >= 0 {
				part.title = strings.TrimSpace(part.title[at+len("Hero:"):])
			}
			out = append(out, part)
		}
	}
	return out
}

// headedParts are a section's parts under its headings, as text.
func headedParts(text string, limit int) []revamp {
	var parts []revamp
	heads := patchHeading.FindAllStringSubmatchIndex(text, -1)
	for i, h := range heads {
		end := len(text)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		parts = append(parts, revamp{title: cleanText(text[h[2]:h[3]], shownIntro), text: cleanText(text[h[1]:end], limit)})
	}
	return parts
}

// otherSections are the titles of the notes' parts besides the heroes'.
func otherSections(patch dexPatch) []string {
	var titles []string
	for _, sec := range patch.Sections {
		if sec.Title == nil || slices.Contains(unshownSections, strings.ToLower(trimNumbering(*sec.Title))) ||
			strings.Contains(strings.ToLower(*sec.Title), "revamped hero") || len(cleanText(sec.HTML, 10)) < 3 {
			continue
		}
		titles = append(titles, *sec.Title)
	}
	return titles
}

// trimNumbering drops a title's "III. " numbering.
func trimNumbering(title string) string {
	if at := strings.Index(title, ". "); at >= 0 && at <= 5 {
		return title[at+2:]
	}
	return title
}

// patchSection is the part of the notes whose title, or a heading in it,
// holds want, as text.
func patchSection(patch dexPatch, want string) map[string]any {
	const note = "Tell what is asked from it; buffs as buffs, nerfs as nerfs, with numbers."
	want = lettersOf(want)
	for _, sec := range patch.Sections {
		if sec.Title != nil && want != "" && strings.Contains(lettersOf(*sec.Title), want) {
			return map[string]any{"patch": patch.Version, "section": *sec.Title, "text": cleanText(sec.HTML, shownSection),
				"note": note, "source": patchSource}
		}
	}
	for _, sec := range patch.Sections {
		for _, part := range headedParts(sec.HTML, shownSection) {
			if want != "" && strings.Contains(lettersOf(part.title), want) {
				return map[string]any{"patch": patch.Version, "section": part.title, "text": part.text, "note": note, "source": patchSource}
			}
		}
	}
	return map[string]any{"patch": patch.Version, "section": want, "found": false, "other_sections": otherSections(patch),
		"note": "No such part in these notes: say so."}
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
