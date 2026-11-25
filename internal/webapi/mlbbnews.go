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

// mlbb_news answers what changed in Mobile Legends and what is coming, from
// the Mobile Legends Wiki, as the owners asked on 2026-11-25: asked which
// heroes the last patch buffed or when a skin comes, the bot searched the
// web five and six times over.

const (
	newsWikiService = "the Mobile Legends Wiki"
	mlbbNewsTTL     = 6 * time.Hour
	newsPageLimit   = 1 << 20
	// newsPatchesSearched are the patches a hero's changes are looked for in.
	newsPatchesSearched = 3
	shownHeroChanges    = 2500
	shownPerSection     = 20
	newsSource          = "the Mobile Legends Wiki (mobile-legends.fandom.com), which adds a patch days or weeks after it comes out"
	upcomingNote        = "Gathered by the wiki from leakers: none of it is confirmed, dates are unknown and it may change or be cancelled. Say so."
)

type mlbbNewsArgs struct {
	Action string `json:"action"`
	Hero   string `json:"hero"`
}

func (a *apis) mlbbNewsTool() tools.Tool {
	return tools.Tool{
		Name: "mlbb_news",
		Description: "Mobile Legends news from its wiki. patch: the latest patch's date, highlights and the heroes buffed, nerfed or adjusted; with hero, that hero's changes in the last patches. " +
			"upcoming: heroes, revamps and skins leakers say are coming, unconfirmed. Use it for buffs, nerfs, patch notes and new heroes or skins.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["patch", "upcoming"]},
				"hero": {"type": "string", "description": "For patch: one hero's changes."}
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
		return a.mlbbPatch(ctx, strings.TrimSpace(args.Hero))
	case "upcoming":
		return a.mlbbUpcoming(ctx)
	}
	return nil, argumentError("action must be patch or upcoming")
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

// patchEntry is one patch in the wiki's list, newest first.
type patchEntry struct {
	Page       string
	Title      string
	Highlights []string
}

var patchLink = regexp.MustCompile(`\[\[(Patch Notes [^|\]]+)\|([^\]]+)\]\]`)

// patchList is the wiki's list of patches, newest first.
func patchList(text string) []patchEntry {
	var list []patchEntry
	for _, block := range strings.Split(text, "<hr>") {
		m := patchLink.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		entry := patchEntry{Page: m[1], Title: m[2]}
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "•") {
				entry.Highlights = append(entry.Highlights, wikiPlain(strings.TrimPrefix(strings.TrimSpace(line), "•")))
			}
		}
		list = append(list, entry)
	}
	return list
}

// heroChange is a hero's entry in a patch's adjustments.
type heroChange struct {
	Hero   string
	Change string
	Text   string
}

var heroHead = regexp.MustCompile(`^\{\{hi\|([^}|]+)[^}]*\}\}\s*\{\{pci\|(buff|nerf|adjust)\}\}\s*$`)

// heroChanges are the heroes a patch's page changes, in its order, each
// with its whole entry.
func heroChanges(text string) []heroChange {
	var changes []heroChange
	var current *heroChange
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := heroHead.FindStringSubmatch(trimmed); m != nil {
			changes = append(changes, heroChange{Hero: m[1], Change: m[2]})
			current = &changes[len(changes)-1]
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(trimmed, "{{clr}}") || strings.HasPrefix(trimmed, "==") {
			current = nil
			continue
		}
		if plain := wikiPlain(trimmed); plain != "" {
			if current.Text != "" {
				current.Text += " • "
			}
			current.Text += plain
		}
	}
	return changes
}

func (a *apis) mlbbPatch(ctx context.Context, hero string) (any, error) {
	index, err := a.wikiText(ctx, "Patch notes")
	if err != nil {
		return nil, err
	}
	patches := patchList(index)
	if len(patches) == 0 {
		return nil, argumentError("%s's list of patches could not be read", newsWikiService)
	}
	if hero != "" {
		return a.heroInPatches(ctx, patches, hero)
	}
	latest := patches[0]
	out := map[string]any{"patch": latest.Title, "highlights": latest.Highlights, "source": newsSource}
	if text, err := a.wikiText(ctx, latest.Page); err == nil {
		var heroes []string
		kinds := map[string][]string{}
		for _, c := range heroChanges(text) {
			if _, seen := kinds[c.Hero]; !seen {
				heroes = append(heroes, c.Hero)
			}
			if !slices.Contains(kinds[c.Hero], c.Change) {
				kinds[c.Hero] = append(kinds[c.Hero], c.Change)
			}
		}
		for i, h := range heroes {
			heroes[i] = h + ": " + strings.Join(kinds[h], ", ")
		}
		if len(heroes) > 0 {
			out["heroes"] = heroes
			out["note"] = "For what changed for a hero, call again with hero."
		}
	}
	if len(patches) > 1 {
		out["previous"] = patches[1].Title
	}
	return out, nil
}

func (a *apis) heroInPatches(ctx context.Context, patches []patchEntry, hero string) (any, error) {
	want := lettersOf(hero)
	for _, p := range patches[:min(len(patches), newsPatchesSearched)] {
		text, err := a.wikiText(ctx, p.Page)
		if err != nil {
			continue
		}
		var found []heroChange
		for _, c := range heroChanges(text) {
			if lettersOf(c.Hero) == want {
				found = append(found, c)
			}
		}
		if len(found) == 0 {
			continue
		}
		var kinds, texts []string
		for _, c := range found {
			if !slices.Contains(kinds, c.Change) {
				kinds = append(kinds, c.Change)
			}
			texts = append(texts, c.Text)
		}
		return map[string]any{"hero": found[0].Hero, "patch": p.Title, "change": strings.Join(kinds, ", "),
			"changes": cleanText(strings.Join(texts, " • "), shownHeroChanges), "source": newsSource}, nil
	}
	titles := make([]string, 0, newsPatchesSearched)
	for _, p := range patches[:min(len(patches), newsPatchesSearched)] {
		titles = append(titles, p.Title)
	}
	return map[string]any{"hero": hero, "changed": false, "patches_searched": titles,
		"note": "The wiki lists no change to this hero in these patches; a newer patch may not be on it yet.", "source": newsSource}, nil
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
	return map[string]any{"upcoming": upcomingSections(text), "note": upcomingNote, "source": newsSource}, nil
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
	wikiTemplateArgs = regexp.MustCompile(`\{\{(?:ai|hi|ii|si)\|([^}|]+)(?:\|([^}|]+))?[^}]*\}\}`)
	wikiChangeTag    = regexp.MustCompile(`\{\{pci\|(buff|nerf|adjust)\}\}`)
	wikiTemplate     = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	wikiLinkText     = regexp.MustCompile(`\[\[(?:[^|\]]*\|)?([^\]]+)\]\]`)
	wikiExternal     = regexp.MustCompile(`\[https?://\S+\s+([^\]]+)\]`)
	wikiTag          = regexp.MustCompile(`<[^>]+>`)
)

// wikiPlain is wiki markup as plain text: "{{ai|Skill 2}} {{pci|buff}}"
// becomes "Skill 2 [buff]", "[[Fanny]]" becomes "Fanny".
func wikiPlain(text string) string {
	text = wikiChangeTag.ReplaceAllString(text, "[$1]")
	text = wikiTemplateArgs.ReplaceAllStringFunc(text, func(m string) string {
		parts := wikiTemplateArgs.FindStringSubmatch(m)
		if parts[2] != "" {
			return parts[1] + " (" + parts[2] + ")"
		}
		return parts[1]
	})
	text = wikiTemplate.ReplaceAllString(text, "")
	text = wikiLinkText.ReplaceAllString(text, "$1")
	text = wikiExternal.ReplaceAllString(text, "$1")
	text = wikiTag.ReplaceAllString(text, " ")
	text = strings.NewReplacer("'''", "", "''", "").Replace(text)
	text = strings.TrimLeft(strings.TrimSpace(text), ":*# ")
	return strings.Join(strings.Fields(text), " ")
}
