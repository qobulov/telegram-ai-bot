package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

var bossRules = BossRules{Week: arenaDay.AddDate(0, 0, -2), Day: arenaDay, Now: arenaNow, Tickets: 2, TicketEvery: time.Hour, Coins: 7}

// A boss fight takes one of the player's tickets, ErrBossNoTries with none,
// gives its coins and adds its damage to the week; a ticket comes back each
// hour, up to the most; an unlimited player takes none.
func TestRecordBossFight(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)

	if p, err := store.BossPlayerOf(ctx, 1, bossRules); err != nil || p != (BossPlayer{Tickets: 2}) {
		t.Fatalf("never fought = %+v, %v", p, err)
	}
	if out, err := store.RecordBossFight(ctx, 1, 500, 3, bossRules); err != nil || out != (BossOutcome{Tickets: 1, Coins: 7, NextTicket: time.Hour}) {
		t.Fatalf("a fight = %+v, %v", out, err)
	}
	later := bossRules
	later.Now = arenaNow.Add(20 * time.Minute)
	if out, err := store.RecordBossFight(ctx, 1, 300, 2, later); err != nil || out != (BossOutcome{Tickets: 0, Coins: 14, NextTicket: 40 * time.Minute}) {
		t.Fatalf("a second = %+v, %v", out, err)
	}
	if _, err := store.RecordBossFight(ctx, 1, 900, 5, later); !errors.Is(err, ErrBossNoTries) {
		t.Errorf("a third = %v, want ErrBossNoTries", err)
	}
	if p, err := store.BossPlayerOf(ctx, 1, later); err != nil || p != (BossPlayer{Best: 500, BestLevel: 3, Tries: 2, Tickets: 0, NextTicket: 40 * time.Minute}) {
		t.Errorf("out of tickets = %+v, %v", p, err)
	}
	hour := bossRules
	hour.Now = arenaNow.Add(time.Hour + time.Minute)
	if p, _ := store.BossPlayerOf(ctx, 1, hour); p.Tickets != 1 {
		t.Errorf("an hour on = %+v, want a ticket back", p)
	}
	if out, err := store.RecordBossFight(ctx, 1, 100, 1, hour); err != nil || out.Tickets != 0 {
		t.Errorf("the ticket back = %+v, %v", out, err)
	}
	free := later
	free.Unlimited = true
	if _, err := store.RecordBossFight(ctx, 2, 1000, 4, free); err != nil {
		t.Errorf("an unlimited fight = %v", err)
	}
	if top, _ := store.BossTop(ctx, bossRules.Week, 10, true); len(top) != 1 || top[0].UserID != 1 {
		t.Errorf("the table = %+v, want the unlimited fight left out", top)
	}
	if total, err := store.BossWeek(ctx, bossRules.Week); err != nil || total != 1900 {
		t.Errorf("the week = %d, %v; want 1900", total, err)
	}
	if total, _ := store.BossWeek(ctx, bossRules.Week.AddDate(0, 0, 7)); total != 0 {
		t.Errorf("the next week = %d, want 0", total)
	}
}

// The boss's table ranks the week's players by their best fight and gives a
// player their place; a higher level beats more damage.
func TestBossTable(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	for _, f := range []struct {
		user   int64
		damage int64
		level  int
	}{{1, 100, 6}, {1, 900, 4}, {2, 5000, 5}} {
		rules := bossRules
		rules.Tickets = 5
		if _, err := store.RecordBossFight(ctx, f.user, f.damage, f.level, rules); err != nil {
			t.Fatal(err)
		}
	}
	top, err := store.BossTop(ctx, bossRules.Week, 10, false)
	if err != nil || len(top) != 2 || top[0].Name != "Alice" || top[0].Level != 6 || top[0].Best != 900 || top[1].Level != 5 || top[1].Best != 5000 {
		t.Fatalf("top = %+v, %v", top, err)
	}
	if me, place, total, err := store.BossPlace(ctx, bossRules.Week, 2, false); err != nil || place != 2 || total != 2 || me.Best != 5000 || me.Level != 5 {
		t.Errorf("Bob's place = %+v %d of %d, %v", me, place, total, err)
	}
	// by all the damage of the week, Bob's 5000 beats Alice's 100 and 900
	if total, err := store.BossTop(ctx, bossRules.Week, 10, true); err != nil || len(total) != 2 || total[0].UserID != 2 || total[0].Total != 5000 || total[1].Total != 1000 {
		t.Errorf("top by total = %+v, %v", total, err)
	}
	if me, place, _, err := store.BossPlace(ctx, bossRules.Week, 1, true); err != nil || place != 2 || me.Total != 1000 {
		t.Errorf("Alice's place by total = %+v %d, %v", me, place, err)
	}
	if _, place, _, err := store.BossPlace(ctx, bossRules.Week, 3, false); err != nil || place != 0 {
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
	rules.Tickets = 9
	for _, f := range []struct {
		user   int64
		damage int64
		level  int
	}{{1, 100, 1}, {2, 900, 3}, {1, 300, 2}} {
		if _, err := store.RecordBossFight(ctx, f.user, f.damage, f.level, rules); err != nil {
			t.Fatal(err)
		}
	}
	later := rules
	later.Week = rules.Week.AddDate(0, 0, 7)
	if _, err := store.RecordBossFight(ctx, 1, 5000, 9, later); err != nil {
		t.Fatal(err)
	}
	owner := rules
	owner.Unlimited = true
	if _, err := store.RecordBossFight(ctx, 3, 9000, 20, owner); err != nil {
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
