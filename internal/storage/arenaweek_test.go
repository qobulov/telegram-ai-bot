package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

var weekPrizes = []ArenaPrize{{1, 1, 50}, {2, 3, 30}, {4, 5, 10}}

// The week is paid once, by rating, to those who attacked within it only,
// as far as the prizes go.
func TestPayArenaWeek(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	since := arenaDay.AddDate(0, 0, -2)
	until := since.AddDate(0, 0, 7)
	// Players 1..7, rating 1700 down to 1100; 6 attacked before the week,
	// 3 not at all, 7 after it.
	for i := int64(1); i <= 7; i++ {
		if _, err := store.pool.Exec(ctx, `INSERT INTO arena_players (user_id, rating) VALUES ($1, $2)`, i, 1800-100*i); err != nil {
			t.Fatal(err)
		}
		at := since.Add(time.Hour)
		switch i {
		case 3:
			continue
		case 6:
			at = since.Add(-time.Hour)
		case 7:
			at = until
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO arena_battles (attacker, defender, seed, attacker_won, rating_change, at) VALUES ($1, 99, 1, true, 0, $2)`, i, at); err != nil {
			t.Fatal(err)
		}
	}
	note := func(place int) string { return fmt.Sprintf("place %d", place) }

	players, paid, err := store.PayArenaWeek(ctx, since, until, weekPrizes, note)
	if err != nil || !paid || players != 4 {
		t.Fatalf("paid %d (%v), %v; want 4", players, paid, err)
	}
	want := map[int64]string{1: "place 1/50", 2: "place 2/30", 4: "place 3/30", 5: "place 4/10"}
	rows, _ := store.pool.Query(ctx, `SELECT user_id, note, tokens FROM card_mail`)
	got := map[int64]string{}
	for rows.Next() {
		var id int64
		var n string
		var tok int
		if err := rows.Scan(&id, &n, &tok); err != nil {
			t.Fatal(err)
		}
		got[id] = fmt.Sprintf("%s/%d", n, tok)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("mail = %v, want %v", got, want)
	}

	if players, paid, err := store.PayArenaWeek(ctx, since, until, weekPrizes, note); err != nil || paid || players != 0 {
		t.Errorf("paid again: %d %v %v", players, paid, err)
	}

	for user, want := range map[int64]int{1: 1, 4: 3, 5: 4, 3: 0, 6: 0} {
		if got, err := store.ArenaWeekPlace(ctx, user, since); err != nil || got != want {
			t.Errorf("place of %d = %d (%v), want %d", user, got, err, want)
		}
	}
}
