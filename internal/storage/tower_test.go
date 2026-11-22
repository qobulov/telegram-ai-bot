package storage

import (
	"context"
	"errors"
	"testing"
)

// A tower fight counts one of the day's fights, with no limit; a win clears
// the floor and gives its coins and tokens, a loss nothing; a floor not the
// next is refused; the next day counts from 0 again.
func TestRecordTowerFight(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	rules := TowerRules{Day: arenaDay, Coins: 12, Tokens: 1}

	if p, err := store.TowerPlayerOf(ctx, 1, arenaDay); err != nil || p != (TowerPlayer{}) {
		t.Fatalf("never in the tower = %+v, %v", p, err)
	}
	out, err := store.RecordTowerFight(ctx, 1, "Alice", 1, false, rules)
	if err != nil || out != (TowerOutcome{Floor: 0, Tries: 1}) {
		t.Fatalf("a loss = %+v, %v", out, err)
	}
	if arenaCoins(t, store, 1) != 0 || cardTokens(t, store, 1) != 0 {
		t.Error("a loss gave something")
	}
	out, err = store.RecordTowerFight(ctx, 1, "Alice", 1, true, rules)
	if err != nil || out != (TowerOutcome{Floor: 1, Tries: 2, Coins: 12, Tokens: 1}) {
		t.Fatalf("a win = %+v, %v", out, err)
	}
	if _, err := store.RecordTowerFight(ctx, 1, "Alice", 1, true, rules); !errors.Is(err, ErrTowerFloorMoved) {
		t.Errorf("floor 1 again = %v, want ErrTowerFloorMoved", err)
	}
	if _, err := store.RecordTowerFight(ctx, 1, "Alice", 2, false, rules); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTowerFight(ctx, 1, "Alice", 2, false, rules); err != nil {
		t.Errorf("a 4th fight = %v, want no limit", err)
	}
	p, err := store.TowerPlayerOf(ctx, 1, arenaDay)
	if err != nil || p != (TowerPlayer{Floor: 1, Tries: 4}) {
		t.Errorf("after the day = %+v, %v", p, err)
	}
	rules.Day = arenaDay.AddDate(0, 0, 1)
	if p, _ := store.TowerPlayerOf(ctx, 1, rules.Day); p.Tries != 0 {
		t.Errorf("the next day's attempts used = %d", p.Tries)
	}
	out, err = store.RecordTowerFight(ctx, 1, "Alice", 2, true, rules)
	if err != nil || out.Floor != 2 || out.Tries != 1 || out.Coins != 24 || out.Tokens != 2 {
		t.Errorf("the next day = %+v, %v", out, err)
	}
}

// The tower's table ranks those who cleared a floor, the highest first, then
// by id, and gives a player their place.
func TestTowerTable(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_players (user_id, name) VALUES (1, 'Ann'), (2, 'Bob'), (3, 'Cy'), (4, 'Di')`,
		`INSERT INTO tower_players (user_id, floor) VALUES (1, 5), (2, 30), (3, 0), (4, 30)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	top, err := store.TowerTop(ctx, 10)
	if err != nil || len(top) != 3 || top[0].Name != "Bob" || top[1].UserID != 4 || top[2].Floor != 5 {
		t.Fatalf("top = %+v, %v", top, err)
	}
	if me, place, total, err := store.TowerPlace(ctx, 1); err != nil || place != 3 || total != 3 || me.Floor != 5 {
		t.Errorf("Ann's place = %+v %d of %d, %v", me, place, total, err)
	}
	if _, place, _, err := store.TowerPlace(ctx, 3); err != nil || place != 0 {
		t.Errorf("Cy's place = %d, %v; want 0, no floor cleared", place, err)
	}
}
