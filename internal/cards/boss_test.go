package cards

import (
	"context"
	"slices"
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
			p.Best = max(p.Best, b.damage)
		}
		if !b.at.Before(rules.Day) {
			p.Tries++
		}
	}
	return p, nil
}

func (f *fakeStore) RecordBossFight(ctx context.Context, userID int64, damage int64, rules storage.BossRules) (storage.BossOutcome, error) {
	p, _ := f.BossPlayerOf(ctx, userID, rules)
	if p.Tries >= rules.Tries {
		return storage.BossOutcome{}, storage.ErrBossNoTries
	}
	if f.arena.coins == nil {
		f.arena.coins = map[int64]int{}
	}
	f.boss = append(f.boss, fakeBossFight{user: userID, week: rules.Week, at: rules.Now, damage: damage})
	f.arena.coins[userID] += rules.Coins
	return storage.BossOutcome{Tries: p.Tries + 1, Coins: f.arena.coins[userID]}, nil
}

func (f *fakeStore) BossTop(_ context.Context, week time.Time, limit int) ([]storage.BossRow, error) {
	best := map[int64]int64{}
	for _, b := range f.boss {
		if b.week.Equal(week) {
			best[b.user] = max(best[b.user], b.damage)
		}
	}
	var top []storage.BossRow
	for id, d := range best {
		top = append(top, storage.BossRow{UserID: id, Best: d})
	}
	slices.SortFunc(top, func(a, b storage.BossRow) int {
		if a.Best != b.Best {
			return int(b.Best - a.Best)
		}
		return int(a.UserID - b.UserID)
	})
	return top[:min(limit, len(top))], nil
}

func (f *fakeStore) BossPlace(ctx context.Context, week time.Time, userID int64) (storage.BossRow, int, int, error) {
	top, _ := f.BossTop(ctx, week, 1<<30)
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

// The boss's level rises every BossLevelDamage of the week, its attack with
// it; a fight's coins rise with its damage up to the most.
func TestBossGrowth(t *testing.T) {
	if bossLevel(0) != 1 || bossLevel(BossLevelDamage-1) != 1 || bossLevel(BossLevelDamage) != 2 || bossLevel(5*BossLevelDamage) != 6 {
		t.Error("the boss's levels")
	}
	if bossAttack(1) != BossAttack || bossAttack(2) <= bossAttack(1) {
		t.Errorf("attack: %d at 1, %d at 2", bossAttack(1), bossAttack(2))
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
	if b, ok := view["boss"].(map[string]any); !ok || b["boss"] != true || view["level"] != 1.0 || view["tries"] != 0.0 || len(view["team"].([]any)) != 5 {
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
	if rows := top["top"].([]any); len(rows) != 1 || rows[0].(map[string]any)["me"] != true || top["me"].(map[string]any)["place"] != 1.0 {
		t.Errorf("the table = %v", top)
	}
	next := arenaHandler(t, store, now.Add(24*time.Hour))
	if _, out := next(7, map[string]any{"action": "boss_fight"}); out["tries"] != 1.0 {
		t.Errorf("the next day's fight = %v", out)
	}
}
