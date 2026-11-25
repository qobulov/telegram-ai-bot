package webapi

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
)

// MLBBDex and Rone Arena both count the game's ranked matches, as the
// owners asked on 2026-11-25: what one lacks the other gives, and where
// both give it, the code takes the better and gives Maria one answer. A
// hero's rates are MLBBDex's for all ranks, measured that day, else Rone
// Arena's; its counters are those both name first, then the rest; its
// builds are both sources' together, the most played first, each once.

const shownMergedBuilds = 5

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

// mergeCounters puts MLBBDex's counters for rank into the hero's: those
// both sources name first, then Rone Arena's other ones, then MLBBDex's.
func (a *apis) mergeCounters(ctx context.Context, out *mlbbHeroOutput, rank string) {
	slug, names, err := a.dexSlug(ctx, out.Hero)
	if err != nil {
		return
	}
	counters, err := dexGet[dexCounters](ctx, a, "/api/v1/heroes/"+slug+"/counters")
	if err != nil {
		return
	}
	ranked := counters[rank]
	out.CounteredBy = mergeMatchups(out.CounteredBy, dexMatchups(ranked.Weak, names))
	out.StrongAgainst = mergeMatchups(out.StrongAgainst, dexMatchups(ranked.Strong, names))
	if len(ranked.Weak)+len(ranked.Strong) > 0 {
		out.Note += "; counters are MLBBDex's and Rone Arena's together, those both name first"
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

// mergeMatchups is the heroes in both lists first, in Rone Arena's order,
// then the rest of Rone Arena's, then the rest of MLBBDex's.
func mergeMatchups(rone, dex []mlbbMatchup) []mlbbMatchup {
	inDex := func(m mlbbMatchup) bool {
		return slices.ContainsFunc(dex, func(d mlbbMatchup) bool { return normalizeName(d.Hero) == normalizeName(m.Hero) })
	}
	var both, roneOnly, dexOnly []mlbbMatchup
	for _, m := range rone {
		if inDex(m) {
			both = append(both, m)
		} else {
			roneOnly = append(roneOnly, m)
		}
	}
	for _, d := range dex {
		if !slices.ContainsFunc(rone, func(m mlbbMatchup) bool { return normalizeName(m.Hero) == normalizeName(d.Hero) }) {
			dexOnly = append(dexOnly, d)
		}
	}
	return slices.Concat(both, roneOnly, dexOnly)
}

// dexBuildsFor are MLBBDex's builds for the hero's lane and rank.
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

// mergeBuilds is both sources' builds together, the most played first: a
// build both give, by its items, spell and emblem, is kept once, with the
// higher pick rate.
func mergeBuilds(rone, dex []mlbbBuild) []mlbbBuild {
	var merged []mlbbBuild
	for _, b := range slices.Concat(rone, dex) {
		i := slices.IndexFunc(merged, func(m mlbbBuild) bool { return sameBuild(m, b) })
		switch {
		case i < 0:
			merged = append(merged, b)
		case rateValue(b.PickRate) > rateValue(merged[i].PickRate):
			merged[i] = b
		}
	}
	slices.SortStableFunc(merged, func(x, y mlbbBuild) int { return cmp.Compare(rateValue(y.PickRate), rateValue(x.PickRate)) })
	return merged[:min(len(merged), shownMergedBuilds)]
}

func sameBuild(x, y mlbbBuild) bool {
	key := func(b mlbbBuild) string {
		items := slices.Clone(b.Items)
		slices.Sort(items)
		return strings.ToLower(strings.Join(items, "|") + "/" + b.Spell + "/" + b.Emblem)
	}
	return key(x) == key(y)
}

// rateValue is "52.3%" as 52.3.
func rateValue(rate string) float64 {
	var v float64
	fmt.Sscanf(strings.TrimSuffix(rate, "%"), "%g", &v)
	return v
}
