package webapi

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"net/url"
	"slices"
	"sync"
)

// What makes the bot a coach: how to play a hero, an item's stats, the
// heroes to pick against the enemy and with the team, and the wins needed to
// reach a win rate.
const (
	shownCombo = 600
	shownItem  = 300
	// maxEnemies and maxAllies are a team of five with one pick still to
	// make; draftPicks are how many picks a draft answer gives.
	maxEnemies = 5
	maxAllies  = 4
	draftPicks = 5
	// synergyWeight makes a good teammate count half as much as a counter.
	synergyWeight     = 0.5
	maxWinrateMatches = 100_000

	comboNote   = "The game's academy's advice, in English: give it in the asker's language and your own words."
	draftNote   = "Picks scored from ranked games of the last 7 days: why says how each pick changes an enemy's or a teammate's win rate. Name the top picks with their reasons."
	winrateNote = "Wins in a row, with no loss, from the current matches and win rate."
)

type mlbbCombo struct {
	Title string `json:"title"`
	How   string `json:"how"`
}

type mlbbCombosOutput struct {
	Hero   string      `json:"hero"`
	Combos []mlbbCombo `json:"combos"`
	Source string      `json:"source"`
	Note   string      `json:"note"`
}

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
	Enemies []string        `json:"enemies,omitempty"`
	Allies  []string        `json:"allies,omitempty"`
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

type arenaCombo struct {
	Title string `json:"title"`
	Desc  string `json:"desc"`
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

// mlbbCombos is the academy's advice on how to play a hero, which MLBBDex
// does not have.
func (a *apis) mlbbCombos(ctx context.Context, name string) (mlbbCombosOutput, error) {
	names, err := a.mlbbHeroes(ctx)
	if err != nil {
		return mlbbCombosOutput{}, err
	}
	id, err := findHero(names, name)
	if err != nil {
		return mlbbCombosOutput{}, err
	}
	combos, err := remember(a.cache, fmt.Sprintf("mlbb:combos:%d", id), mlbbHeroesTTL, func() ([]arenaCombo, error) {
		return arenaList[arenaCombo](ctx, a, fmt.Sprintf("/api/heroes/%d/skill-combos", id))
	})
	if err != nil {
		return mlbbCombosOutput{}, err
	}
	out := mlbbCombosOutput{Hero: names[id], Combos: []mlbbCombo{}, Source: arenaService + ", the game's academy", Note: comboNote}
	for _, c := range combos {
		if how := cleanText(c.Desc, shownCombo); how != "" {
			out.Combos = append(out.Combos, mlbbCombo{Title: cleanText(c.Title, maxHeroName), How: how})
		}
	}
	if len(out.Combos) == 0 {
		return mlbbCombosOutput{}, fmt.Errorf("%s has no advice on playing %s", arenaService, names[id])
	}
	return out, nil
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

// mlbbDraft scores the heroes to pick against enemies and with allies, by
// how each changes their win rate in ranked games: a hero that lowers an
// enemy's win rate counts for it, one an enemy beats against it, and one
// that raises a teammate's half as much. A hero whose statistics will not
// load is left out rather than failing the draft.
func (a *apis) mlbbDraft(ctx context.Context, enemies, allies []string, lane, rank string) (mlbbDraftOutput, error) {
	if len(enemies)+len(allies) == 0 || len(enemies) > maxEnemies || len(allies) > maxAllies {
		return mlbbDraftOutput{}, argumentError("give 1 to %d enemies and up to %d allies picked so far", maxEnemies, maxAllies)
	}
	if lane != "" && !slices.Contains(mlbbLanes, lane) {
		return mlbbDraftOutput{}, argumentError("lane must be one of %v", mlbbLanes)
	}
	names, err := a.mlbbHeroes(ctx)
	if err != nil {
		return mlbbDraftOutput{}, err
	}
	enemyIDs, err := heroIDs(names, enemies)
	if err != nil {
		return mlbbDraftOutput{}, err
	}
	allyIDs, err := heroIDs(names, allies)
	if err != nil {
		return mlbbDraftOutput{}, err
	}
	against := make([]arenaStats, len(enemyIDs))
	with := make([]arenaStats, len(allyIDs))
	var wg sync.WaitGroup
	for i, id := range enemyIDs {
		wg.Go(func() { against[i], _ = a.arenaCounters(ctx, id, rank) })
	}
	for i, id := range allyIDs {
		wg.Go(func() { with[i], _ = a.arenaCompatibility(ctx, id, rank) })
	}
	wg.Wait()

	score := map[int]float64{}
	why := map[int][]string{}
	add := func(id int, points float64, reason string) {
		score[id] += points
		why[id] = append(why[id], reason)
	}
	for i, e := range against {
		for _, s := range e.Lowering {
			add(s.ID, -s.Change, fmt.Sprintf("counters %s (%s)", names[enemyIDs[i]], signedPercent(-s.Change)))
		}
		for _, s := range e.Raising {
			add(s.ID, -s.Change, fmt.Sprintf("weak against %s (%s)", names[enemyIDs[i]], signedPercent(-s.Change)))
		}
	}
	for i, t := range with {
		for _, s := range t.Raising {
			add(s.ID, synergyWeight*s.Change, fmt.Sprintf("good with %s (%s)", names[allyIDs[i]], signedPercent(s.Change)))
		}
	}
	if len(score) == 0 {
		return mlbbDraftOutput{}, fmt.Errorf("%s gave no statistics for these heroes now", arenaService)
	}
	picked := slices.Concat(enemyIDs, allyIDs)
	inLane := a.laneFilter(ctx, lane)
	var candidates []int
	for id, points := range score {
		if points > 0 && names[id] != "" && !slices.Contains(picked, id) && inLane(names[id]) {
			candidates = append(candidates, id)
		}
	}
	slices.SortFunc(candidates, func(x, y int) int { return cmp.Or(cmp.Compare(score[y], score[x]), cmp.Compare(x, y)) })
	out := mlbbDraftOutput{Enemies: heroNamesOf(names, enemyIDs), Allies: heroNamesOf(names, allyIDs), Lane: lane, Rank: rank,
		Picks: []mlbbDraftPick{}, Source: mlbbSource, Note: draftNote}
	for _, id := range candidates[:min(len(candidates), draftPicks)] {
		out.Picks = append(out.Picks, mlbbDraftPick{Hero: names[id], Score: signedPercent(score[id]), Why: why[id]})
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

func (a *apis) arenaCompatibility(ctx context.Context, id int, rank string) (arenaStats, error) {
	return remember(a.cache, fmt.Sprintf("mlbb:compatibility:%d:%s", id, rank), mlbbTTL, func() (arenaStats, error) {
		query := url.Values{"days": {mlbbDays}, "rank": {rank}}
		stats, err := arenaList[arenaStats](ctx, a, fmt.Sprintf("/api/heroes/%d/compatibility?%s", id, query.Encode()))
		if err != nil || len(stats) == 0 {
			return arenaStats{}, err
		}
		return stats[0], nil
	})
}

func heroIDs(names map[int]string, heroes []string) ([]int, error) {
	var ids []int
	for _, h := range heroes {
		id, err := findHero(names, h)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func heroNamesOf(names map[int]string, ids []int) []string {
	var out []string
	for _, id := range ids {
		out = append(out, names[id])
	}
	return out
}

func signedPercent(change float64) string {
	return fmt.Sprintf("%+.1f%%", change*100)
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
