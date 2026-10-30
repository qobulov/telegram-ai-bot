package storage

import (
	"context"
	"testing"
	"time"
)

// A player's row for a game and chat counts their plays, keeps the last
// score and the best with when it was made; other games and chats have
// rows of their own.
func TestRecordGameScore(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	at := time.Now().Truncate(time.Second)
	for i, score := range []int{40, 75, 60} {
		if err := store.RecordGameScore(ctx, GameScore{Game: "otinchi", ChatID: -5, UserID: 7, Name: "Bek", Score: score, At: at.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RecordGameScore(ctx, GameScore{Game: "ilon", ChatID: -5, UserID: 7, Score: 9, At: at}); err != nil {
		t.Fatal(err)
	}
	var name string
	var best, last, plays, rows int
	var bestAt, playedAt time.Time
	if err := store.pool.QueryRow(ctx, `SELECT name, best, last, plays, best_at, played_at FROM game_scores WHERE game = 'otinchi' AND chat_id = -5 AND user_id = 7`).
		Scan(&name, &best, &last, &plays, &bestAt, &playedAt); err != nil {
		t.Fatal(err)
	}
	if name != "Bek" || best != 75 || last != 60 || plays != 3 || !bestAt.Equal(at.Add(time.Minute)) || !playedAt.Equal(at.Add(2*time.Minute)) {
		t.Errorf("got %s best %d at %v, last %d, %d plays, played %v", name, best, bestAt, last, plays, playedAt)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM game_scores`).Scan(&rows); err != nil || rows != 2 {
		t.Errorf("%d rows, %v", rows, err)
	}
}

// A game's table ranks each player by their best in any chat, ties sharing
// a place and the earlier best first, other games apart, and adds the
// player's own row when they are below the first limit.
func TestGameTable(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	at := time.Now().Truncate(time.Second)
	for i, s := range []GameScore{
		{Game: "otinchi", ChatID: -1, UserID: 1, Name: "Ali", Score: 50},
		{Game: "otinchi", ChatID: -2, UserID: 1, Name: "Ali", Score: 120},
		{Game: "otinchi", ChatID: 0, UserID: 2, Name: "Vali", Score: 90},
		{Game: "otinchi", ChatID: -1, UserID: 3, Name: "Gani", Score: 90},
		{Game: "otinchi", ChatID: -1, UserID: 4, Name: "Bek", Score: 10},
		{Game: "ilon", ChatID: -1, UserID: 5, Name: "Sher", Score: 999},
	} {
		s.At = at.Add(time.Duration(i) * time.Second)
		if err := store.RecordGameScore(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	table, err := store.GameTable(ctx, "otinchi", 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []GameRank{{1, 1, "Ali", 120}, {2, 2, "Vali", 90}, {2, 3, "Gani", 90}, {4, 4, "Bek", 10}}
	if len(table) != len(want) {
		t.Fatalf("table = %+v", table)
	}
	for i := range want {
		if table[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, table[i], want[i])
		}
	}
	if top, _ := store.GameTable(ctx, "otinchi", 1, 2); len(top) != 2 {
		t.Errorf("a player in the first rows is not added again: %+v", top)
	}
}
