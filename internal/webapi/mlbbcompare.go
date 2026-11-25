package webapi

import (
	"context"
	"fmt"
	"strings"
)

// Each answer takes one source, as the owners asked on 2026-11-25, so no
// work is done twice: a hero's counters and builds are Rone Arena's, from
// the game's academy, and MLBBDex's only when Rone Arena gives none.

const shownDexBuilds = 5

type dexCounter struct {
	Slug      string  `json:"slug"`
	Advantage float64 `json:"advantage"`
}

type dexCounters map[string]struct {
	Strong []dexCounter `json:"strong"`
	Weak   []dexCounter `json:"weak"`
}

type dexBuild struct {
	Items    []string `json:"items"`
	Emblem   string   `json:"emblem"`
	Talents  []string `json:"talents"`
	Spell    string   `json:"spell"`
	WinRate  float64  `json:"winRate"`
	PickRate float64  `json:"pickRate"`
}

// dexBuilds are a hero's builds by lane, as "Exp", then by rank.
type dexBuilds map[string]map[string][]dexBuild

// dexSlug is the hero's MLBBDex slug, and every slug's name.
func (a *apis) dexSlug(ctx context.Context, name string) (string, map[string]string, error) {
	heroes, err := a.dexHeroes(ctx)
	if err != nil {
		return "", nil, err
	}
	names := map[string]string{}
	slug := ""
	for _, h := range heroes {
		names[h.Slug] = h.Name
		if normalizeName(h.Name) == normalizeName(name) {
			slug = h.Slug
		}
	}
	if slug == "" {
		return "", nil, fmt.Errorf("%s has no hero %q", dexService, name)
	}
	return slug, names, nil
}

// dexCountersInto gives the hero MLBBDex's counters for rank, when Rone
// Arena did not answer.
func (a *apis) dexCountersInto(ctx context.Context, out *mlbbHeroOutput, rank string) {
	slug, names, err := a.dexSlug(ctx, out.Hero)
	if err != nil {
		return
	}
	counters, err := dexGet[dexCounters](ctx, a, "/api/v1/heroes/"+slug+"/counters")
	if err != nil {
		return
	}
	ranked := counters[rank]
	out.CounteredBy = dexMatchups(ranked.Weak, names)
	out.StrongAgainst = dexMatchups(ranked.Strong, names)
	if len(out.CounteredBy)+len(out.StrongAgainst) > 0 {
		out.Note += "; counters are MLBBDex's"
	}
}

func dexMatchups(counters []dexCounter, names map[string]string) []mlbbMatchup {
	var out []mlbbMatchup
	for _, c := range counters {
		if name := names[c.Slug]; name != "" {
			out = append(out, mlbbMatchup{Hero: name, WinRateChange: fmt.Sprintf("%+.1f%%", c.Advantage)})
		}
	}
	return out
}

// dexBuildsFor are MLBBDex's builds for the hero's lane and rank, most
// played first as MLBBDex lists them.
func (a *apis) dexBuildsFor(ctx context.Context, hero, lane, rank string) []mlbbBuild {
	slug, _, err := a.dexSlug(ctx, hero)
	if err != nil {
		return nil
	}
	builds, err := dexGet[dexBuilds](ctx, a, "/api/v1/heroes/"+slug+"/builds")
	if err != nil {
		return nil
	}
	var out []mlbbBuild
	for key, byRank := range builds {
		if !strings.EqualFold(key, lane) {
			continue
		}
		for _, b := range byRank[rank] {
			out = append(out, mlbbBuild{Spell: cleanText(b.Spell, maxHeroName), Emblem: cleanText(b.Emblem, maxHeroName),
				Talents: cleanNames(b.Talents), Items: cleanNames(b.Items), WinRate: percent(b.WinRate / 100), PickRate: percent(b.PickRate / 100)})
		}
	}
	return out
}

func cleanNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, cleanText(n, maxHeroName))
	}
	return out
}
