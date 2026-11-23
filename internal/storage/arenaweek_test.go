package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

var weekPrizes = []ArenaPrize{{From: 1, To: 1, Tokens: 50, Boxes: 3, Box: "box"}, {From: 2, To: 3, Tokens: 30}, {From: 4, To: 5, Tokens: 10, Boxes: 1, Box: "box"}}

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
	want := map[int64]string{1: "place 1/50/box3", 2: "place 2/30/0", 4: "place 3/30/0", 5: "place 4/10/box1"}
	rows, _ := store.pool.Query(ctx, `SELECT user_id, note, tokens, box, boxes FROM card_mail`)
	got := map[int64]string{}
	for rows.Next() {
		var id int64
		var n, box string
		var tok, boxes int
		if err := rows.Scan(&id, &n, &tok, &box, &boxes); err != nil {
			t.Fatal(err)
		}
		got[id] = fmt.Sprintf("%s/%d/%s%d", n, tok, box, boxes)
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

// Once a week is paid every rating not frozen starts again at
// ArenaRatingStart; a frozen one keeps its own.
func TestArenaWeekResetsRatings(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_players (user_id, name) VALUES (1, 'Ann'), (2, 'Bob'), (3, 'Cy')`,
		`INSERT INTO arena_players (user_id, rating, rating_frozen) VALUES (1, 1800, false), (2, 400, false), (3, 1200, true)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	since := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	if _, paid, err := store.PayArenaWeek(ctx, since, since.AddDate(0, 0, 7), nil, func(int) string { return "" }); err != nil || !paid {
		t.Fatalf("pay = %v %v", paid, err)
	}
	for id, want := range map[int64]int{1: ArenaRatingStart, 2: ArenaRatingStart, 3: 1200} {
		var rating int
		if err := store.pool.QueryRow(ctx, `SELECT rating FROM arena_players WHERE user_id = $1`, id).Scan(&rating); err != nil || rating != want {
			t.Errorf("player %d's rating = %d %v, want %d", id, rating, err, want)
		}
	}
}
