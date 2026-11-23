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

// A boss week is paid once: its players by their best fight get the prizes
// of their places in the mail, and another week's fights count for nothing.
func TestPayBossWeek(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	rules := bossRules
	rules.Tries = 9
	for _, f := range []struct {
		user   int64
		damage int64
	}{{1, 100}, {2, 900}, {1, 300}} {
		if _, err := store.RecordBossFight(ctx, f.user, f.damage, rules); err != nil {
			t.Fatal(err)
		}
	}
	later := rules
	later.Week = rules.Week.AddDate(0, 0, 7)
	if _, err := store.RecordBossFight(ctx, 1, 5000, later); err != nil {
		t.Fatal(err)
	}
	prizes := []ArenaPrize{{From: 1, To: 1, Tokens: 10, Boxes: 1, Box: "box"}, {From: 2, To: 2, Tokens: 4}}
	note := func(place int) string { return "place " + string(rune('0'+place)) }
	players, paid, err := store.PayBossWeek(ctx, rules.Week, prizes, note)
	if err != nil || !paid || players != 2 {
		t.Fatalf("paid %d, %v, %v", players, paid, err)
	}
	var bobTokens, bobBoxes, aliceTokens int
	var bobNote string
	if err := store.pool.QueryRow(ctx, `SELECT tokens, boxes, note FROM card_mail WHERE user_id = 2`).Scan(&bobTokens, &bobBoxes, &bobNote); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT tokens FROM card_mail WHERE user_id = 1`).Scan(&aliceTokens); err != nil {
		t.Fatal(err)
	}
	if bobTokens != 10 || bobBoxes != 1 || bobNote != "place 1" || aliceTokens != 4 {
		t.Errorf("Bob %d tokens %d boxes %q, Alice %d tokens", bobTokens, bobBoxes, bobNote, aliceTokens)
	}
	if _, paid, err := store.PayBossWeek(ctx, rules.Week, prizes, note); err != nil || paid {
		t.Errorf("paid again: %v, %v", paid, err)
	}
	if countRows(t, store, "card_mail") != 2 {
		t.Error("a second payment sent mail")
	}
}
