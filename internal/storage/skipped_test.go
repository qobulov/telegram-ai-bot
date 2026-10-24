package storage

import (
	"context"
	"testing"
	"time"
)

// Skips are kept with their reason, and expire with the history.
func TestSkipsAreKeptThenExpire(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	for _, reason := range []string{"group_quota", "overloaded"} {
		if err := store.NoteSkip(ctx, Skip{ChatID: -100, UserID: 7, MessageID: 42, Reason: reason}); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM skipped_messages WHERE chat_id = -100 AND reason IN ('group_quota', 'overloaded')`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("count = %d, %v", count, err)
	}

	if _, err := store.pool.Exec(ctx, `UPDATE skipped_messages SET created_at = now() - interval '10 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Expire(ctx, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM skipped_messages`).Scan(&count); err != nil || count != 0 {
		t.Errorf("after expiry count = %d, %v", count, err)
	}
}
