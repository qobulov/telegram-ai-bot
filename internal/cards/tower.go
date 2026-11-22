package cards

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	mrand "math/rand/v2"
	"slices"
	"time"

	"telegram-ai-bot/internal/arena"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// The Tower of Souls, as the owners asked on 2026-11-19: floors of fixed
// enemy teams, each built from the catalog by its number alone, fought with
// the arena team saved, as many times as the player likes (the daily limit
// of 10 was taken off as the owners asked on 2026-11-19). Each week, Monday to Monday in
// Tashkent, one anime of at least TowerThemeLeast cards raises its cards'
// attack and health by TowerThemeBonus percent, in the tower only. Floor
// 1's team has TowerBasePower and each floor TowerGrowth times the one
// before (since 2026-11-21 12000 and 1.03, from 5000 and 1.07: players
// cleared floors of their own team's power by trying again, 46 floors in
// two days, and the owners asked for floors that grow slowly), up to floor
// TowerBend; above it each floor only TowerStep stronger than the one
// before, as the owners asked on 2026-11-22: the floors grew apart too fast
// (+2800 a floor by the 70th). Its cards rise in rarity every few floors and
// a star every TowerStarsEvery floors.
const (
	TowerThemeBonus = 20
	TowerThemeLeast = 5
	TowerBasePower  = 12000
	TowerGrowth     = 1.03
	TowerBend       = 30
	TowerStep       = 850
	TowerStarsEvery = 15
	// towerSalt keeps a floor's draw apart from any other seeded by a number.
	towerSalt = 0x746f776572
)

// towerRarities are the two rarities a floor's cards are drawn between.
func towerRarities(floor int) (lo, hi Rarity) {
	switch {
	case floor >= 40:
		return Legendary, Mythic
	case floor >= 20:
		return Epic, Legendary
	case floor >= 10:
		return Rare, Epic
	}
	return Common, Rare
}

// towerHigher is the chance of a floor's card being of the higher rarity,
// rising through each band of floors so that the next band comes gently.
func towerHigher(floor int) float64 {
	start, size := 1, 9
	switch {
	case floor >= 40:
		return 0.5
	case floor >= 20:
		start, size = 20, 20
	case floor >= 10:
		start, size = 10, 10
	}
	return 0.1 + 0.7*float64(floor-start)/float64(size)
}

func towerStars(floor int) int { return min(1+(floor-1)/TowerStarsEvery, MaxStars) }

// TowerPower is the power of floor's enemy team: TowerGrowth times the
// floor below's up to TowerBend, TowerStep more than it after.
func TowerPower(floor int) int {
	grown := func(f int) int { return int(math.Round(TowerBasePower * math.Pow(TowerGrowth, float64(f-1)))) }
	if floor <= TowerBend {
		return grown(floor)
	}
	return grown(TowerBend) + TowerStep*(floor-TowerBend)
}

// towerReward is what clearing floor gives, the first time, which is the
// only time: 10 + 2 × floor arena coins, and 3 card tokens on every 10th
// floor, 1 on every other 5th.
func towerReward(floor int) (coins, tokens int) {
	coins = 10 + 2*floor
	switch {
	case floor%10 == 0:
		tokens = 3
	case floor%5 == 0:
		tokens = 1
	}
	return coins, tokens
}

// towerTeam is floor's enemy team: five distinct cards of the catalog, not
// limited ones, drawn by a seed from floor alone, at the floor's stars, the
// two toughest (by health to attack) in front, their numbers scaled so
// that the team has TowerPower(floor); ok is false with too few cards.
func towerTeam(catalog []storage.Card, floor int) (fighters, bool) {
	picked, ok := towerCards(catalog, floor)
	if !ok {
		return fighters{}, false
	}
	stars := towerStars(floor)
	base := 0
	for _, c := range picked {
		attack, health := Boosted(c, stars)
		base += attack + health/4
	}
	scale := float64(TowerPower(floor)) / float64(max(base, 1))
	var slots [5]arena.Fighter
	views := make([]fighterView, len(picked))
	for i, c := range picked {
		attack, health := Boosted(c, stars)
		attack, health = int(math.Round(float64(attack)*scale)), int(math.Round(float64(health)*scale))
		f := fighterOf(c.ID, c.Name, c.Rarity, stars, attack, health)
		slots[i] = f
		views[i] = fighterView{ID: c.ID, Name: c.Name, Image: c.ImageURL, Rarity: c.Rarity, Stars: stars,
			Attack: attack, Health: health, Class: classNames[f.Class], Skill: skillNames[f.Skill], Anime: Franchise(c.Anime)}
	}
	team := arena.Team{Front: [2]arena.Fighter{slots[0], slots[1]}, Back: [3]arena.Fighter{slots[2], slots[3], slots[4]}}
	return fighters{team, views}, true
}

// towerCards draws floor's five cards, toughest first.
func towerCards(catalog []storage.Card, floor int) ([]storage.Card, bool) {
	lo, hi := towerRarities(floor)
	pools := map[Rarity][]storage.Card{}
	for _, c := range catalog {
		if r := Rarity(c.Rarity); (r == lo || r == hi) && !storage.CardLimited(c.ID) && c.Attack > 0 {
			pools[r] = append(pools[r], c)
		}
	}
	if len(pools[lo])+len(pools[hi]) < 5 {
		return nil, false
	}
	for _, pool := range pools {
		slices.SortFunc(pool, func(a, b storage.Card) int { return int(a.ID - b.ID) })
	}
	rng := mrand.New(mrand.NewPCG(uint64(floor), towerSalt))
	used := map[int64]bool{}
	var picked []storage.Card
	for len(picked) < 5 {
		pool := pools[lo]
		if rng.Float64() < towerHigher(floor) && len(pools[hi]) > 0 || len(pool) == 0 {
			pool = pools[hi]
		}
		if c := pool[rng.IntN(len(pool))]; !used[c.ID] {
			used[c.ID] = true
			picked = append(picked, c)
		}
	}
	slices.SortStableFunc(picked, func(a, b storage.Card) int {
		ra, rb := float64(a.Health)/float64(a.Attack), float64(b.Health)/float64(b.Attack)
		switch {
		case ra > rb:
			return -1
		case ra < rb:
			return 1
		}
		return 0
	})
	return picked, true
}

// towerTheme is the week's anime, as Franchise counts it, among those of
// at least TowerThemeLeast cards, picked by the week's date; "" for none.
func towerTheme(catalog []storage.Card, week time.Time) string {
	count := map[string]int{}
	for _, c := range catalog {
		if a := Franchise(c.Anime); a != "" {
			count[a]++
		}
	}
	var animes []string
	for a, n := range count {
		if n >= TowerThemeLeast {
			animes = append(animes, a)
		}
	}
	if len(animes) == 0 {
		return ""
	}
	slices.Sort(animes)
	h := fnv.New32a()
	h.Write([]byte(week.Format(time.DateOnly)))
	return animes[h.Sum32()%uint32(len(animes))]
}

// towerSeed is a tower fight's seed, from the player, the floor and the
// day's attempt, so that a fight is the same however it is asked for.
func towerSeed(userID int64, floor, try int) int64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "tower:%d:%d:%d", userID, floor, try)
	return int64(h.Sum64())
}

// tower is what the tower's page needs: the player's floor and attempts,
// the week's theme, and the next floor's team and reward beside the
// player's own team, raised by the theme.
type tower struct {
	player     storage.TowerPlayer
	theme      string
	enemy      fighters
	mine       fighters
	hasEnemy   bool
	hasTeam    bool
	next       int
	dayStarted time.Time
}

func (h *handler) towerOf(ctx context.Context, userID int64) (tower, error) {
	if err := h.catalog.Ensure(ctx); err != nil {
		return tower{}, fmt.Errorf("fill the card catalog: %w", err)
	}
	catalog, err := h.Store.CardCatalog(ctx)
	if err != nil {
		return tower{}, fmt.Errorf("read the card catalog: %w", err)
	}
	now := h.Now()
	t := tower{theme: towerTheme(catalog, arenaWeekStart(now)), dayStarted: DayStart(now)}
	if t.player, err = h.Store.TowerPlayerOf(ctx, userID, t.dayStarted); err != nil {
		return tower{}, err
	}
	t.next = t.player.Floor + 1
	t.enemy, t.hasEnemy = towerTeam(catalog, t.next)
	if t.mine, t.hasTeam, err = h.themedSaved(ctx, userID, t.theme); err != nil {
		return tower{}, err
	}
	return t, nil
}

// towerView is the tower for the player.
func (h *handler) towerView(ctx context.Context, user miniapp.User) (any, failure) {
	t, err := h.towerOf(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read the tower", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	coins, tokens := towerReward(t.next)
	view := map[string]any{
		"floor": t.player.Floor, "next": t.next,
		"theme":  map[string]any{"anime": t.theme, "bonus": TowerThemeBonus},
		"reward": map[string]any{"coins": coins, "tokens": tokens},
		"enemy":  nil, "enemy_power": 0, "team": nil, "power": 0,
	}
	if t.hasEnemy {
		view["enemy"], view["enemy_power"] = t.enemy.views, t.enemy.team.Power()
	}
	if t.hasTeam {
		view["team"], view["power"] = t.mine.views, t.mine.team.Power()
	}
	return view, failure{}
}

// towerFight fights the player's next floor with their saved team, raised
// by the week's theme. A fight refused says why: "no_team", "no_floor" (the catalog too small for it) or "moved" (another fight
// cleared it meanwhile).
func (h *handler) towerFight(ctx context.Context, user miniapp.User) (any, failure) {
	t, err := h.towerOf(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read the tower", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	switch {
	case !t.hasTeam:
		return map[string]any{"refused": "no_team"}, failure{}
	case !t.hasEnemy:
		return map[string]any{"refused": "no_floor"}, failure{}
	}
	result := arena.Battle(t.mine.team, t.enemy.team, towerSeed(user.ID, t.next, t.player.Tries))
	coins, tokens := towerReward(t.next)
	rules := storage.TowerRules{Day: t.dayStarted, Coins: coins, Tokens: tokens}
	out, err := h.Store.RecordTowerFight(ctx, user.ID, name(user), t.next, result.AttackerWon, rules)
	switch {
	case errors.Is(err, storage.ErrTowerFloorMoved):
		return map[string]any{"refused": "moved"}, failure{}
	case err != nil:
		h.Logger.Error("could not record a tower fight", "user_id", user.ID, "floor", t.next, "error", err)
		return nil, errUnavailable
	}
	fight := map[string]any{
		"won": result.AttackerWon, "events": eventViews(result.Events), "attacker": t.mine.views, "defender": t.enemy.views,
		"fought": t.next, "floor": out.Floor,
	}
	if result.AttackerWon {
		fight["coins_won"], fight["tokens_won"], fight["coins"], fight["tokens"] = coins, tokens, out.Coins, out.Tokens
	}
	return fight, failure{}
}
