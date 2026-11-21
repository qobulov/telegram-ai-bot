package storage

import (
	"context"
	"testing"
	"time"
)

// Telegram's IDs pass int4's range: a user's 8 292 769 065, a group's
// -1001392088384. Every query that compares a parameter with 0 must still
// take them.
// Both query modes: production's pooler types a parameter as the simple
// protocol does, which the exec mode alone missed (member_info failed for a
// member with a big ID, 2026-11-21).
func TestQueriesTakeBigIDs(t *testing.T) {
	for _, mode := range []string{"exec", "simple_protocol"} {
		t.Run(mode, func(t *testing.T) { queriesTakeBigIDs(t, newTestStoreIn(t, mode)) })
	}
}

func queriesTakeBigIDs(t *testing.T, store *Store) {
	ctx := context.Background()
	const chat, user, other = int64(-1001392088384), int64(8292769065), int64(7366268491)

	if _, _, err := store.ResolveDuel(ctx, chat, 77, user, other, 0); err != nil {
		t.Errorf("ResolveDuel: %v", err)
	}
	if _, err := store.Warns(ctx, chat, user, 5); err != nil {
		t.Errorf("Warns: %v", err)
	}
	if _, err := store.RankLog(ctx, chat, user, 5); err != nil {
		t.Errorf("RankLog: %v", err)
	}
	if _, err := store.InviteTop(ctx, chat, InviteQuery{Since: time.Now().Add(-time.Hour), Inviter: user, Limit: 5}); err != nil {
		t.Errorf("InviteTop: %v", err)
	}
	if _, err := store.InviteAwards(ctx, chat); err != nil {
		t.Errorf("InviteAwards: %v", err)
	}
	if _, _, err := store.OpenPoll(ctx, chat, 3000000000); err != nil {
		t.Errorf("OpenPoll: %v", err)
	}
}
