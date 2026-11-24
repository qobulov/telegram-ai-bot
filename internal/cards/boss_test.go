package cards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// fakeBossFight is a boss fight fakeStore keeps.
type fakeBossFight struct {
	user   int64
	week   time.Time
	at     time.Time
	damage int64
	level  int
}

func (f *fakeStore) BossWeek(_ context.Context, week time.Time) (int64, error) {
	var total int64
	for _, b := range f.boss {
		if b.week.Equal(week) {
			total += b.damage
		}
	}
	return total, nil
}

func (f *fakeStore) BossPlayerOf(_ context.Context, userID int64, rules storage.BossRules) (storage.BossPlayer, error) {
	var p storage.BossPlayer
	for _, b := range f.boss {
		if b.user != userID {
			continue
		}
		if b.week.Equal(rules.Week) {
			p.Best, p.BestLevel = max(p.Best, b.damage), max(p.BestLevel, b.level)
		}
		if !b.at.Before(rules.Day) {
			p.Tries++
		}
	}
	return p, nil
}

func (f *fakeStore) RecordBossFight(ctx context.Context, userID int64, damage int64, level int, rules storage.BossRules) (storage.BossOutcome, error) {
	p, _ := f.BossPlayerOf(ctx, userID, rules)
	if p.Tries >= rules.Tries {
		return storage.BossOutcome{}, storage.ErrBossNoTries
	}
	if f.arena.coins == nil {
		f.arena.coins = map[int64]int{}
	}
	f.boss = append(f.boss, fakeBossFight{user: userID, week: rules.Week, at: rules.Now, damage: damage, level: level})
	f.arena.coins[userID] += rules.Coins
	return storage.BossOutcome{Tries: p.Tries + 1, Coins: f.arena.coins[userID]}, nil
}

func (f *fakeStore) BossTop(_ context.Context, week time.Time, limit int, byTotal bool) ([]storage.BossRow, error) {
	best, level, sum := map[int64]int64{}, map[int64]int{}, map[int64]int64{}
	for _, b := range f.boss {
		if b.week.Equal(week) {
			best[b.user], level[b.user] = max(best[b.user], b.damage), max(level[b.user], b.level)
			sum[b.user] += b.damage
		}
	}
	var top []storage.BossRow
	for id, d := range best {
		top = append(top, storage.BossRow{UserID: id, Best: d, Level: level[id], Total: sum[id]})
	}
	slices.SortFunc(top, func(a, b storage.BossRow) int {
		if byTotal && a.Total != b.Total {
			return int(b.Total - a.Total)
		}
		if !byTotal && a.Level != b.Level {
			return b.Level - a.Level
		}
		if a.Best != b.Best {
			return int(b.Best - a.Best)
		}
		return int(a.UserID - b.UserID)
	})
	return top[:min(limit, len(top))], nil
}

func (f *fakeStore) BossPlace(ctx context.Context, week time.Time, userID int64, byTotal bool) (storage.BossRow, int, int, error) {
	top, _ := f.BossTop(ctx, week, 1<<30, byTotal)
	for i, r := range top {
		if r.UserID == userID {
			return r, i + 1, len(top), nil
		}
	}
	return storage.BossRow{}, 0, len(top), nil
}

// The week's boss is a Mythic, the same all week whatever the catalog's
// order, another the next week, and none with no card.
func TestBossCard(t *testing.T) {
	catalog := towerCatalog()
	week := arenaWeekStart(time.Unix(1_800_000_000, 0))
	a, ok := bossCard(catalog, week)
	if !ok || Rarity(a.Rarity) != Mythic {
		t.Fatalf("the boss = %+v, %v", a, ok)
	}
	reversed := slices.Clone(catalog)
	slices.Reverse(reversed)
	if b, _ := bossCard(reversed, week); b.ID != a.ID {
		t.Error("the catalog's order changed the boss")
	}
	differs := false
	for i := 1; i <= 8; i++ {
		if c, _ := bossCard(catalog, week.AddDate(0, 0, 7*i)); c.ID != a.ID {
			differs = true
		}
	}
	if !differs {
		t.Error("the boss is the same for nine weeks")
	}
	if _, ok := bossCard(nil, week); ok {
		t.Error("a boss out of no catalog")
	}
}

// The boss starts a fight at level 1 with BossHP, each level more; a
// fight's coins rise with its damage up to the most.
func TestBossGrowth(t *testing.T) {
	boss, view := bossFighter(towerCatalog()[len(towerCatalog())-1])
	if boss.Attack != BossAttack || view.Health != BossHP || bossLevels.LevelHP(2) <= bossLevels.LevelHP(1) {
		t.Errorf("the boss at level 1 = %+v, level 2 health %d", view, bossLevels.LevelHP(2))
	}
	if bossCoins(0) != BossCoinsBase || bossCoins(BossCoinsPer*3) != BossCoinsBase+3 || bossCoins(1<<40) != BossCoinsMost {
		t.Error("the boss's coins")
	}
}

// The boss is fought with the saved team BossTries times a day; each fight
// gives coins by its damage, adds it to the week and the table, and the
// next day gives fights again.
func TestBossFights(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{catalog: towerCatalog(), arena: fakeArena{held: map[int64][]storage.OwnedCard{7: arenaCards()}}}
	call := arenaHandler(t, store, now)
	if _, out := call(7, map[string]any{"action": "boss_fight"}); out["refused"] != "no_team" {
		t.Fatalf("with no team = %v", out)
	}
	call(7, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	_, view := call(7, map[string]any{"action": "boss"})
	if b, ok := view["boss"].(map[string]any); !ok || b["boss"] != true || view["best_level"] != 0.0 || view["levels"] == nil || view["tries"] != 0.0 || len(view["team"].([]any)) != 5 {
		t.Fatalf("the boss = %v", view)
	}
	var dealt float64
	for i := range BossTries {
		_, out := call(7, map[string]any{"action": "boss_fight"})
		damage, _ := out["damage"].(float64)
		if out["boss"] != true || damage <= 0 || out["tries"] != float64(i+1) || out["coins_won"] != float64(bossCoins(int(damage))) || len(out["defender"].([]any)) != 1 {
			t.Fatalf("fight %d = %v", i, out)
		}
		dealt += damage
	}
	if _, out := call(7, map[string]any{"action": "boss_fight"}); out["refused"] != "no_tries" {
		t.Errorf("a fight past the day's = %v", out)
	}
	if _, view := call(7, map[string]any{"action": "boss"}); view["week_damage"] != dealt || view["tries"] != float64(BossTries) {
		t.Errorf("after the day = %v, want %v dealt", view, dealt)
	}
	_, top := call(7, map[string]any{"action": "boss_top"})
	if week, _ := top["week"].(map[string]any); week == nil || week["place"] != 1.0 || len(week["prizes"].([]any)) != len(BossWeekPrizes) {
		t.Errorf("the table's prizes = %v", top["week"])
	}
	if rows := top["top"].([]any); len(rows) != 1 || rows[0].(map[string]any)["me"] != true || rows[0].(map[string]any)["level"].(float64) < 1 || top["me"].(map[string]any)["place"] != 1.0 {
		t.Errorf("the table = %v", top)
	}
	next := arenaHandler(t, store, now.Add(24*time.Hour))
	if _, out := next(7, map[string]any{"action": "boss_fight"}); out["tries"] != 1.0 {
		t.Errorf("the next day's fight = %v", out)
	}
}

func (f *fakeStore) PayBossWeek(_ context.Context, week time.Time, _ []storage.ArenaPrize, _ func(int) string) (int, bool, error) {
	if slices.ContainsFunc(f.bossWeeksPaid, week.Equal) {
		return 0, false, nil
	}
	f.bossWeeksPaid = append(f.bossWeeksPaid, week)
	return 1, true, nil
}

// Opening the boss pays the week before, once, and shows the player's place
// and prize this week.
func TestBossWeekPrizes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{catalog: towerCatalog(), arena: fakeArena{held: map[int64][]storage.OwnedCard{7: arenaCards()}}}
	call := arenaHandler(t, store, now)
	call(7, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	call(7, map[string]any{"action": "boss_fight"})
	_, view := call(7, map[string]any{"action": "boss"})
	call(7, map[string]any{"action": "boss_top"})
	last := arenaWeekStart(now).AddDate(0, 0, -7)
	if len(store.bossWeeksPaid) != 1 || !store.bossWeeksPaid[0].Equal(last) {
		t.Errorf("weeks paid = %v, want only %v", store.bossWeeksPaid, last)
	}
	week := view["week"].(map[string]any)
	if week["place"] != 1.0 || week["prize"] != float64(BossWeekPrizes[0].Tokens) || week["boxes"] != float64(BossWeekPrizes[0].Boxes) || len(week["prizes"].([]any)) != len(BossWeekPrizes) {
		t.Errorf("the week = %v", week)
	}
}

// An owner fights the boss past the day's limit; anyone else is held to it.
func TestBossOwnerIsUnlimited(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{catalog: towerCatalog(), arena: fakeArena{held: map[int64][]storage.OwnedCard{7: arenaCards(), 8: arenaCards()}}}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }, Unlimited: func(id int64) bool { return id == 7 }})
	call := func(user int64, fields map[string]any) map[string]any {
		fields["init_data"] = signedInit(user, now)
		body, _ := json.Marshal(fields)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	for _, user := range []int64{7, 8} {
		call(user, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	}
	for i := range BossTries + 3 {
		if out := call(7, map[string]any{"action": "boss_fight"}); out["boss"] != true || out["unlimited"] != true {
			t.Fatalf("the owner's fight %d = %v", i, out)
		}
	}
	for range BossTries {
		call(8, map[string]any{"action": "boss_fight"})
	}
	if out := call(8, map[string]any{"action": "boss_fight"}); out["refused"] != "no_tries" {
		t.Errorf("a player past the limit = %v", out)
	}
	if view := call(7, map[string]any{"action": "boss"}); view["unlimited"] != true {
		t.Errorf("the owner's boss page = %v", view)
	}
}
