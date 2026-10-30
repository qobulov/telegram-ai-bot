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
