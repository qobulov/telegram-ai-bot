package storage

import (
	"context"
	"errors"
	"testing"
)

var bossRules = BossRules{Week: arenaDay.AddDate(0, 0, -2), Day: arenaDay, Now: arenaNow, Tries: 2, Coins: 7}

// A boss fight counts one of the day's, up to its Tries, gives its coins and
// adds its damage to the week; the next day counts from 0 again.
func TestRecordBossFight(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)

	if p, err := store.BossPlayerOf(ctx, 1, bossRules); err != nil || p != (BossPlayer{}) {
		t.Fatalf("never fought = %+v, %v", p, err)
	}
	if out, err := store.RecordBossFight(ctx, 1, 500, bossRules); err != nil || out != (BossOutcome{Tries: 1, Coins: 7}) {
		t.Fatalf("a fight = %+v, %v", out, err)
	}
	if out, err := store.RecordBossFight(ctx, 1, 300, bossRules); err != nil || out != (BossOutcome{Tries: 2, Coins: 14}) {
		t.Fatalf("a second = %+v, %v", out, err)
	}
	if _, err := store.RecordBossFight(ctx, 1, 900, bossRules); !errors.Is(err, ErrBossNoTries) {
		t.Errorf("a third = %v, want ErrBossNoTries", err)
	}
	if p, err := store.BossPlayerOf(ctx, 1, bossRules); err != nil || p != (BossPlayer{Best: 500, Tries: 2}) {
		t.Errorf("after the day = %+v, %v", p, err)
	}
	next := bossRules
	next.Day, next.Now = arenaDay.AddDate(0, 0, 1), arenaNow.AddDate(0, 0, 1)
	if out, err := store.RecordBossFight(ctx, 2, 1000, next); err != nil || out.Tries != 1 {
		t.Errorf("Bob the next day = %+v, %v", out, err)
	}
	if p, _ := store.BossPlayerOf(ctx, 1, next); p.Tries != 0 || p.Best != 500 {
		t.Errorf("Alice the next day = %+v", p)
	}
	if total, err := store.BossWeek(ctx, bossRules.Week); err != nil || total != 1800 {
		t.Errorf("the week = %d, %v; want 1800", total, err)
	}
	if total, _ := store.BossWeek(ctx, bossRules.Week.AddDate(0, 0, 7)); total != 0 {
		t.Errorf("the next week = %d, want 0", total)
	}
}

// The boss's table ranks the week's players by their best fight and gives a
// player their place.
func TestBossTable(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	for _, f := range []struct {
		user   int64
		damage int64
	}{{1, 100}, {1, 900}, {2, 500}} {
		rules := bossRules
		rules.Tries = 5
		if _, err := store.RecordBossFight(ctx, f.user, f.damage, rules); err != nil {
			t.Fatal(err)
		}
	}
	top, err := store.BossTop(ctx, bossRules.Week, 10)
	if err != nil || len(top) != 2 || top[0].Name != "Alice" || top[0].Best != 900 || top[1].Best != 500 {
		t.Fatalf("top = %+v, %v", top, err)
	}
	if me, place, total, err := store.BossPlace(ctx, bossRules.Week, 2); err != nil || place != 2 || total != 2 || me.Best != 500 {
		t.Errorf("Bob's place = %+v %d of %d, %v", me, place, total, err)
	}
	if _, place, _, err := store.BossPlace(ctx, bossRules.Week, 3); err != nil || place != 0 {
		t.Errorf("a stranger's place = %d, %v", place, err)
	}
}
