package webapi

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
)

// What makes the bot a coach: an item's stats, the heroes to pick against
// the enemy and the wins needed to reach a win rate.
const (
	shownItem = 300
	// maxEnemies is a full enemy team; draftPicks are how many picks a
	// draft answer gives.
	maxEnemies        = 5
	draftPicks        = 5
	maxWinrateMatches = 100_000

	draftNote   = "Picks scored from MLBBDex's ranked statistics: why says by how much each pick beats an enemy or loses to one, in win-rate points. Name the top picks with their reasons."
	winrateNote = "Wins in a row, with no loss, from the current matches and win rate."
)

type mlbbItemOutput struct {
	Item     string   `json:"item"`
	Category string   `json:"category,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Price    int      `json:"price_gold,omitempty"`
	Bonus    string   `json:"bonus,omitempty"`
	Unique   string   `json:"unique,omitempty"`
	Passive  string   `json:"passive,omitempty"`
	Active   string   `json:"active,omitempty"`
	Recipe   []string `json:"recipe,omitempty"`
	BestFor  string   `json:"best_for,omitempty"`
	Source   string   `json:"source"`
}

type mlbbDraftPick struct {
	Hero  string   `json:"hero"`
	Score string   `json:"score"`
	Why   []string `json:"why"`
}

type mlbbDraftOutput struct {
	Enemies []string        `json:"enemies"`
	Lane    string          `json:"lane,omitempty"`
	Rank    string          `json:"rank"`
	Picks   []mlbbDraftPick `json:"picks"`
	Source  string          `json:"source"`
	Note    string          `json:"note"`
}

type mlbbWinrateOutput struct {
	Matches    int    `json:"matches"`
	WinRate    string `json:"win_rate"`
	Target     string `json:"target_win_rate"`
	WinsInARow int    `json:"wins_in_a_row"`
	Note       string `json:"note"`
}

type dexItem struct {
	Name     string   `json:"name"`
	Summary  string   `json:"summary"`
	Category string   `json:"category"`
	Price    int      `json:"price"`
	Bonus    string   `json:"bonus"`
	Unique   string   `json:"unique"`
	Passive  string   `json:"passive"`
	Active   string   `json:"active"`
	Recipe   []string `json:"recipe"`
	BestFor  string   `json:"bestFor"`
}

// mlbbItem is an item's stats from MLBBDex.
func (a *apis) mlbbItem(ctx context.Context, name string) (mlbbItemOutput, error) {
	items, err := remember(a.cache, "mlbbdex:items", mlbbHeroesTTL, func() ([]dexItem, error) {
		var answer struct {
			Data []dexItem `json:"data"`
		}
		err := a.patient.getJSON(ctx, call{service: dexService, url: a.urls.MLBBDex + "/api/v1/items", limit: largeBodyLimit}, &answer)
		return answer.Data, err
	})
	if err != nil {
		return mlbbItemOutput{}, err
	}
	itemNames := make([]string, len(items))
	for i, it := range items {
		itemNames[i] = it.Name
	}
	found, err := matchName(itemNames, name, "item")
	if err != nil {
		return mlbbItemOutput{}, err
	}
	it := items[slices.Index(itemNames, found)]
	recipe := []string{}
	for _, part := range it.Recipe {
		if part = cleanText(part, maxHeroName); part != "" {
			recipe = append(recipe, part)
		}
	}
	return mlbbItemOutput{
		Item: cleanText(it.Name, maxHeroName), Category: cleanText(it.Category, maxHeroName), Summary: cleanText(it.Summary, shownTip),
		Price: it.Price, Bonus: cleanText(it.Bonus, shownTip), Unique: cleanText(it.Unique, shownItem),
		Passive: cleanText(it.Passive, shownItem), Active: cleanText(it.Active, shownItem), Recipe: recipe,
		BestFor: cleanText(it.BestFor, maxHeroName), Source: dexSource,
	}, nil
}

// mlbbDraft scores the heroes to pick against the enemy's, from each enemy's
// counters at rank on MLBBDex: a hero an enemy loses to counts for it by
// the win-rate points of that matchup, one an enemy beats against it. An
// enemy whose counters will not load is left out rather than failing the
// draft.
func (a *apis) mlbbDraft(ctx context.Context, enemies []string, lane, rank string) (mlbbDraftOutput, error) {
	if len(enemies) == 0 || len(enemies) > maxEnemies {
		return mlbbDraftOutput{}, argumentError("give 1 to %d enemy heroes picked so far", maxEnemies)
	}
	if lane != "" && !slices.Contains(mlbbLanes, lane) {
		return mlbbDraftOutput{}, argumentError("lane must be one of %v", mlbbLanes)
	}
	heroes, err := a.dexHeroes(ctx)
	if err != nil {
		return mlbbDraftOutput{}, err
	}
	nameOf, slugOf := map[string]string{}, map[string]string{}
	for _, h := range heroes {
		nameOf[h.Slug], slugOf[h.Name] = h.Name, h.Slug
	}
	var picked []string
	for _, e := range enemies {
		found, err := matchHero(dexNames(heroes), e)
		if err != nil {
			return mlbbDraftOutput{}, err
		}
		if !slices.Contains(picked, found) {
			picked = append(picked, found)
		}
	}
	against := make([]dexCounters, len(picked))
	var wg sync.WaitGroup
	for i, enemy := range picked {
		wg.Go(func() { against[i], _ = dexGet[dexCounters](ctx, a, "/api/v1/heroes/"+slugOf[enemy]+"/counters") })
	}
	wg.Wait()

	score := map[string]float64{}
	why := map[string][]string{}
	add := func(hero string, points float64, reason string) {
		if hero != "" {
			score[hero] += points
			why[hero] = append(why[hero], reason)
		}
	}
	for i, counters := range against {
		ranked := counters[rank]
		for _, c := range ranked.Weak {
			add(nameOf[c.Slug], -c.Advantage, fmt.Sprintf("counters %s (%+.1f%%)", picked[i], -c.Advantage))
		}
		for _, c := range ranked.Strong {
			add(nameOf[c.Slug], -c.Advantage, fmt.Sprintf("weak against %s (%+.1f%%)", picked[i], -c.Advantage))
		}
	}
	if len(score) == 0 {
		return mlbbDraftOutput{}, fmt.Errorf("%s gave no statistics for these heroes now", dexService)
	}
	inLane := a.laneFilter(ctx, lane)
	var candidates []string
	for hero, points := range score {
		if points > 0 && !slices.Contains(picked, hero) && inLane(hero) {
			candidates = append(candidates, hero)
		}
	}
	slices.SortFunc(candidates, func(x, y string) int { return cmp.Or(cmp.Compare(score[y], score[x]), cmp.Compare(x, y)) })
	out := mlbbDraftOutput{Enemies: picked, Lane: lane, Rank: rank, Picks: []mlbbDraftPick{}, Source: statsSource, Note: draftNote}
	for _, hero := range candidates[:min(len(candidates), draftPicks)] {
		out.Picks = append(out.Picks, mlbbDraftPick{Hero: hero, Score: fmt.Sprintf("%+.1f%%", score[hero]), Why: why[hero]})
	}
	return out, nil
}

// laneFilter keeps the heroes MLBBDex lists in lane, or every hero when no
// lane is asked or MLBBDex does not answer.
func (a *apis) laneFilter(ctx context.Context, lane string) func(string) bool {
	everyone := func(string) bool { return true }
	if lane == "" {
		return everyone
	}
	heroes, err := a.dexHeroes(ctx)
	if err != nil {
		return everyone
	}
	var keep []string
	for _, h := range heroes {
		if slices.ContainsFunc(h.Lanes, func(l string) bool { return mainLane([]string{l}) == lane }) {
			keep = append(keep, normalizeName(h.Name))
		}
	}
	return func(hero string) bool { return slices.Contains(keep, normalizeName(hero)) }
}

// mlbbWinrate is how many wins in a row lift winRate over matches to
// target, both in percent.
func mlbbWinrate(matches int, winRate, target float64) (mlbbWinrateOutput, error) {
	if matches < 1 || matches > maxWinrateMatches || winRate < 0 || winRate > 100 || target <= 0 || target >= 100 {
		return mlbbWinrateOutput{}, argumentError("matches must be 1 to %d, win_rate 0 to 100 and target_win_rate above 0 and below 100", maxWinrateMatches)
	}
	out := mlbbWinrateOutput{Matches: matches, WinRate: fmt.Sprintf("%.1f%%", winRate), Target: fmt.Sprintf("%.1f%%", target), Note: winrateNote}
	if winRate >= target {
		return out, nil
	}
	// (wins + w) / (matches + w) >= target, with wins = matches × winRate.
	n := float64(matches)
	needed := (target*n - winRate*n) / (100 - target)
	out.WinsInARow = int(math.Ceil(needed - 1e-9))
	return out, nil
}
