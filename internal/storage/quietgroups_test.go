package storage

import (
	"context"
	"testing"
	"time"
)

// A group the bot has not answered keeps only its recent messages; one it
// answers keeps them all.
func TestQuietGroupsKeepOnlyRecentMessages(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const quiet, talking = -100, -200
	for _, q := range []string{
		`INSERT INTO group_messages (chat_id, message_id, author_id, author_name, text, created_at) VALUES
			(-100, 1, 7, 'Ann', 'old', now() - interval '10 days'),
			(-100, 2, 7, 'Ann', 'new', now() - interval '1 day'),
			(-200, 3, 8, 'Bob', 'old', now() - interval '10 days')`,
		`INSERT INTO group_replies (chat_id, user_id, created_at) VALUES (-200, 8, now() - interval '2 days')`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	removed, err := store.ExpireQuietGroups(ctx, now.Add(-8*24*time.Hour), now.Add(-7*24*time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("removed %d, %v; want the quiet group's old message alone", removed, err)
	}
	count := func(chat int64) (n int) {
		_ = store.pool.QueryRow(ctx, `SELECT count(*) FROM group_messages WHERE chat_id = $1`, chat).Scan(&n)
		return
	}
	if count(quiet) != 1 || count(talking) != 1 {
		t.Errorf("left %d in the quiet group and %d in the other", count(quiet), count(talking))
	}
}
