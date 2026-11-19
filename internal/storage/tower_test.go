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
