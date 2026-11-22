package cards

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// fakeTower keeps the tower of fakeStore.
type fakeTower struct {
	floor map[int64]int
	tries map[int64]int // on any day
}

func (f *fakeStore) TowerTop(_ context.Context, limit int) ([]storage.TowerRow, error) {
	var top []storage.TowerRow
	for id, floor := range f.tower.floor {
		if floor > 0 {
			top = append(top, storage.TowerRow{UserID: id, Name: fmt.Sprintf("P%d", id), Floor: floor})
		}
	}
	slices.SortFunc(top, func(a, b storage.TowerRow) int {
		if a.Floor != b.Floor {
			return b.Floor - a.Floor
		}
		return int(a.UserID - b.UserID)
	})
	return top[:min(limit, len(top))], nil
}

func (f *fakeStore) TowerPlace(ctx context.Context, userID int64) (storage.TowerRow, int, int, error) {
	top, _ := f.TowerTop(ctx, 1<<30)
	for i, r := range top {
		if r.UserID == userID {
			return r, i + 1, len(top), nil
		}
	}
	return storage.TowerRow{}, 0, len(top), nil
}

func (f *fakeStore) TowerPlayerOf(_ context.Context, userID int64, _ time.Time) (storage.TowerPlayer, error) {
	return storage.TowerPlayer{Floor: f.tower.floor[userID], Tries: f.tower.tries[userID]}, nil
}

func (f *fakeStore) RecordTowerFight(_ context.Context, userID int64, _ string, floor int, won bool, rules storage.TowerRules) (storage.TowerOutcome, error) {
	if f.tower.floor == nil {
		f.tower = fakeTower{floor: map[int64]int{}, tries: map[int64]int{}}
	}
	switch {
	case f.tower.floor[userID]+1 != floor:
		return storage.TowerOutcome{}, storage.ErrTowerFloorMoved
	}
	f.tower.tries[userID]++
	out := storage.TowerOutcome{Floor: f.tower.floor[userID], Tries: f.tower.tries[userID]}
	if won {
		if f.arena.coins == nil {
			f.arena.coins = map[int64]int{}
		}
		f.tower.floor[userID] = floor
		f.arena.coins[userID] += rules.Coins
		f.tokens += rules.Tokens
		out.Floor, out.Coins, out.Tokens = floor, f.arena.coins[userID], f.tokens
	}
	return out, nil
}

// towerCatalog is 30 cards of each rarity, numbered by CardOf, of seven
// animes, Naruto's among them.
func towerCatalog() []storage.Card {
	var catalog []storage.Card
	for r := Common; r <= Mythic; r++ {
		for i := range 30 {
			id := int64(1000*int(r) + i + 10)
			catalog = append(catalog, CardOf(id, fmt.Sprintf("C%d", id), fmt.Sprintf("Anime %d", i%7), "", 0, r))
		}
	}
	catalog[0].Anime, catalog[1].Anime, catalog[2].Anime = "Naruto", "Naruto Shippuden", "Boruto"
	return catalog
}

func TestTowerTeamIsDeterministic(t *testing.T) {
	catalog := towerCatalog()
	a, ok := towerTeam(catalog, 7)
	b, _ := towerTeam(catalog, 7)
	if !ok || !reflect.DeepEqual(a, b) {
		t.Fatal("the same floor gave different teams")
	}
	reversed := append([]storage.Card(nil), catalog...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if c, _ := towerTeam(reversed, 7); !reflect.DeepEqual(a, c) {
		t.Error("the catalog's order changed the floor's team")
	}
	if c, _ := towerTeam(catalog, 8); reflect.DeepEqual(a.views, c.views) {
		t.Error("floors 7 and 8 have the same team")
	}
	seen := map[int64]bool{}
	for _, v := range a.views {
		if seen[v.ID] {
			t.Errorf("card %d twice", v.ID)
		}
		seen[v.ID] = true
	}
	if _, ok := towerTeam(catalog[:4], 1); ok {
		t.Error("a team of four cards")
	}
}

// Each floor's team is about TowerGrowth stronger than the one before up to
// TowerBend and TowerStep stronger after, of rarer cards and more stars
// higher up.
func TestTowerPowerGrowsWithTheFloor(t *testing.T) {
	catalog := towerCatalog()
	last := 0
	for floor := 1; floor <= 80; floor++ {
		team, _ := towerTeam(catalog, floor)
		power := team.team.Power()
		if diff := float64(power)/float64(TowerPower(floor)) - 1; diff > 0.01 || diff < -0.01 {
			t.Errorf("floor %d: power %d, want about %d", floor, power, TowerPower(floor))
		}
		if last > 0 && floor <= TowerBend {
			if g := float64(power) / float64(last); g < TowerGrowth-0.02 || g > TowerGrowth+0.02 {
				t.Errorf("floor %d: power ×%.3f on the floor below", floor, g)
			}
		}
		if last > 0 && floor > TowerBend {
			if d := power - last; d < TowerStep-300 || d > TowerStep+300 {
				t.Errorf("floor %d: power %+d on the floor below, want about +%d", floor, d, TowerStep)
			}
		}
		last = power
		lo, hi := towerRarities(floor)
		for _, v := range team.views {
			if Rarity(v.Rarity) != lo && Rarity(v.Rarity) != hi || v.Stars != towerStars(floor) {
				t.Errorf("floor %d: %+v", floor, v)
			}
		}
	}
	for floor, want := range map[int]int{1: 1, 15: 1, 16: 2, 31: 3, 61: 5, 99: 5} {
		if got := towerStars(floor); got != want {
			t.Errorf("floor %d: ★%d, want ★%d", floor, got, want)
		}
	}
	if lo, _ := towerRarities(45); lo != Legendary {
		t.Errorf("floor 45 draws from %v", lo)
	}
}

func TestTowerReward(t *testing.T) {
	for floor, want := range map[int][2]int{1: {12, 0}, 5: {20, 1}, 7: {24, 0}, 10: {30, 3}, 15: {40, 1}, 20: {50, 3}} {
		if coins, tokens := towerReward(floor); coins != want[0] || tokens != want[1] {
			t.Errorf("floor %d: %d coins, %d tokens, want %v", floor, coins, tokens, want)
		}
	}
}

// The week's theme is the same all week, one of the animes of at least
// TowerThemeLeast cards, and not the same every week.
func TestTowerTheme(t *testing.T) {
	catalog := towerCatalog()
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, Location)
	theme := towerTheme(catalog, arenaWeekStart(monday.Add(50*time.Hour)))
	if theme == "" || theme != towerTheme(catalog, arenaWeekStart(monday.Add(150*time.Hour))) {
		t.Fatalf("the week's theme = %q", theme)
	}
	themes := map[string]bool{}
	for week := range 20 {
		th := towerTheme(catalog, monday.AddDate(0, 0, 7*week))
		themes[th] = true
		if th == "Naruto" || th == "Boruto" {
			t.Errorf("week %d: %q has under %d cards", week, th, TowerThemeLeast)
		}
	}
	if len(themes) < 2 {
		t.Errorf("20 weeks had themes %v", themes)
	}
	if got := towerTheme(catalog[:3], monday); got != "" {
		t.Errorf("no anime of 5 cards, yet a theme %q", got)
	}
}

// The theme raises its cards' attack and health by TowerThemeBonus, on top
// of their synergy, and no other card.
func TestThemeRaisesOnlyItsCards(t *testing.T) {
	held := arenaCards()
	held[0].Anime, held[1].Anime = "Naruto", "Naruto Shippuden" // one franchise: a pair
	ids := [5]int64{10, 11, 12, 13, 14}
	plain, _ := fightersOf(held, ids)
	themed, _ := themedFighters(held, ids, "Naruto")
	for i := range ids {
		p, th := plain.views[i], themed.views[i]
		want := 0
		if i < 2 {
			want = TowerThemeBonus
		}
		attack, health := Boosted(held[i].Card, 2)
		if th.Theme != want || th.Attack != raised(attack, p.Synergy+want) || th.Health != raised(health, p.Synergy+want) {
			t.Errorf("slot %d: plain %+v, themed %+v", i, p, th)
		}
	}
}

// The tower fights the next floor with the saved team: a win clears it and
// gives its reward, a loss nothing, and there is no daily limit.
func TestTowerFights(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	strong := arenaCards()
	for i := range strong {
		strong[i].Attack, strong[i].Health, strong[i].Stars = 100_000, 400_000, 5
	}
	store := &fakeStore{catalog: towerCatalog(), arena: fakeArena{held: map[int64][]storage.OwnedCard{7: strong, 8: arenaCards()}}}
	call := arenaHandler(t, store, now)
	if _, out := call(7, map[string]any{"action": "tower_fight"}); out["refused"] != "no_team" {
		t.Fatalf("with no team = %v", out)
	}
	for _, user := range []int64{7, 8} {
		call(user, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	}
	_, view := call(7, map[string]any{"action": "tower"})
	if view["next"] != float64(1) || len(view["enemy"].([]any)) != 5 || view["enemy_power"].(float64) < float64(TowerPower(1))*0.99 {
		t.Fatalf("the tower = %v", view)
	}
	_, out := call(7, map[string]any{"action": "tower_fight"})
	if out["won"] != true || out["floor"] != float64(1) || out["coins_won"] != float64(12) || out["coins"] != float64(12) || len(out["events"].([]any)) == 0 {
		t.Fatalf("a strong team on floor 1 = %v", out)
	}
	for range 4 {
		call(7, map[string]any{"action": "tower_fight"})
	}
	if store.tower.floor[7] != 5 || store.tokens != 1 {
		t.Errorf("after five wins: floor %d, tokens %d", store.tower.floor[7], store.tokens)
	}

	store.tower.floor[8] = 30
	coins := store.arena.coins[8]
	for i := range 25 {
		_, out := call(8, map[string]any{"action": "tower_fight"})
		if out["won"] != false || out["floor"] != float64(30) || out["coins_won"] != nil {
			t.Fatalf("a weak team's attempt %d = %v", i, out)
		}
	}
	if store.arena.coins[8] != coins {
		t.Error("a loss gave coins")
	}
}

// The tower's table lists the players highest in it, the highest first, and
// the player's own place.
func TestTowerTable(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{tower: fakeTower{floor: map[int64]int{7: 12, 8: 40, 9: 0, 10: 40}, tries: map[int64]int{}}}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	_, out := post(t, h, signedInit(7, now), "tower_top")
	top := out["top"].([]any)
	if len(top) != 3 {
		t.Fatalf("table = %v, want the three who cleared a floor", top)
	}
	first, last := top[0].(map[string]any), top[2].(map[string]any)
	if first["floor"] != 40.0 || first["name"] != "P8" || last["floor"] != 12.0 || last["me"] != true {
		t.Errorf("table = %v", top)
	}
	if me := out["me"].(map[string]any); me["place"] != 3.0 || me["total"] != 3.0 || me["floor"] != 12.0 {
		t.Errorf("my place = %v", me)
	}
}
