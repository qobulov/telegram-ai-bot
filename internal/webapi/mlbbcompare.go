package webapi

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
)

// MLBBDex and Rone Arena both count the game's ranked matches, as the
// owners asked on 2026-11-25: what one lacks the other gives, and where
// both give it, a hero's rates, counters and builds, the two are set side
// by side so that Maria says when they agree and when they do not.

const (
	// ratesApart is how far apart, in points, two win rates are taken to
	// differ.
	ratesApart     = 2.0
	shownDexBuilds = 3
	compareNote    = "compared: where MLBBDex and Rone Arena both count it. in_both is what they agree on, the more sure; say when rates differ, giving both"
)

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

// mlbbCompared is a hero's rates and counters from both sources.
type mlbbCompared struct {
	Rates           map[string]mlbbRates `json:"rates,omitempty"`
	RatesDiffer     bool                 `json:"rates_differ,omitempty"`
	CounteredByBoth []string             `json:"countered_by_in_both,omitempty"`
	StrongVsBoth    []string             `json:"strong_against_in_both,omitempty"`
	CounteredByDex  []string             `json:"countered_by_mlbbdex_only,omitempty"`
	StrongVsDex     []string             `json:"strong_against_mlbbdex_only,omitempty"`
	Note            string               `json:"note"`
}

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

func (a *apis) dexCounters(ctx context.Context, slug string) (dexCounters, error) {
	return dexGet[dexCounters](ctx, a, "/api/v1/heroes/"+slug+"/counters")
}

func (a *apis) dexHeroBuilds(ctx context.Context, slug string) (dexBuilds, error) {
	return dexGet[dexBuilds](ctx, a, "/api/v1/heroes/"+slug+"/builds")
}

// compareHero sets a hero's rates and counters from Rone Arena, in out and
// rone, beside MLBBDex's rates in dex and its counters.
func (a *apis) compareHero(ctx context.Context, out mlbbHeroOutput, rone, dex mlbbRates, rank string) *mlbbCompared {
	c := &mlbbCompared{Note: compareNote}
	if rone.WinRate != "" && dex.WinRate != "" {
		c.Rates = map[string]mlbbRates{"mlbbdex": dex, "rone_arena": rone}
		c.RatesDiffer = math.Abs(rateValue(rone.WinRate)-rateValue(dex.WinRate)) >= ratesApart
	}
	if slug, names, err := a.dexSlug(ctx, out.Hero); err == nil {
		if counters, err := a.dexCounters(ctx, slug); err == nil {
			ranked := counters[rank]
			c.CounteredByBoth, c.CounteredByDex = splitByBoth(dexNamesOf(ranked.Weak, names), out.CounteredBy)
			c.StrongVsBoth, c.StrongVsDex = splitByBoth(dexNamesOf(ranked.Strong, names), out.StrongAgainst)
		}
	}
	if c.Rates == nil && len(c.CounteredByBoth)+len(c.CounteredByDex)+len(c.StrongVsBoth)+len(c.StrongVsDex) == 0 {
		return nil
	}
	return c
}

func dexNamesOf(counters []dexCounter, names map[string]string) []string {
	var out []string
	for _, c := range counters {
		if name := names[c.Slug]; name != "" {
			out = append(out, name)
		}
	}
	return out
}

// splitByBoth parts MLBBDex's heroes into those Rone Arena lists too and
// those it does not.
func splitByBoth(dex []string, rone []mlbbMatchup) (both, dexOnly []string) {
	for _, name := range dex {
		if slices.ContainsFunc(rone, func(m mlbbMatchup) bool { return normalizeName(m.Hero) == normalizeName(name) }) {
			both = append(both, name)
		} else {
			dexOnly = append(dexOnly, name)
		}
	}
	return both, dexOnly
}

// rateValue is "52.3%" as 52.3.
func rateValue(rate string) float64 {
	var v float64
	fmt.Sscanf(strings.TrimSuffix(rate, "%"), "%g", &v)
	return v
}

// dexBuildsFor are MLBBDex's top builds for the hero's lane and rank, and
// the items found in both sources' builds.
func (a *apis) dexBuildsFor(ctx context.Context, hero, lane, rank string, rone []mlbbBuild) ([]mlbbBuild, []string) {
	slug, _, err := a.dexSlug(ctx, hero)
	if err != nil {
		return nil, nil
	}
	builds, err := a.dexHeroBuilds(ctx, slug)
	if err != nil {
		return nil, nil
	}
	var byRank map[string][]dexBuild
	for key, b := range builds {
		if strings.EqualFold(key, lane) {
			byRank = b
		}
	}
	var out []mlbbBuild
	for _, b := range byRank[rank][:min(len(byRank[rank]), shownDexBuilds)] {
		out = append(out, mlbbBuild{Spell: cleanText(b.Spell, maxHeroName), Emblem: cleanText(b.Emblem, maxHeroName),
			Talents: cleanNames(b.Talents), Items: cleanNames(b.Items), WinRate: percent(b.WinRate / 100), PickRate: percent(b.PickRate / 100)})
	}
	return out, itemsInBoth(out, rone)
}

func cleanNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, cleanText(n, maxHeroName))
	}
	return out
}

// itemsInBoth are the items of MLBBDex's builds that Rone Arena's builds
// also have, in MLBBDex's order.
func itemsInBoth(dex, rone []mlbbBuild) []string {
	var both []string
	for _, b := range dex {
		for _, item := range b.Items {
			inRone := slices.ContainsFunc(rone, func(r mlbbBuild) bool {
				return slices.ContainsFunc(r.Items, func(x string) bool { return strings.EqualFold(x, item) })
			})
			if inRone && !slices.Contains(both, item) {
				both = append(both, item)
			}
		}
	}
	return both
}
