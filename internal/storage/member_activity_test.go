package storage

import (
	"context"
	"testing"
	"time"
)

// A member's activity counts what they wrote and what was done to them, in
// their chat alone; one never seen reads as nothing.
func TestMemberActivityOf(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const chat, user = int64(-1001392088384), int64(8292769065)
	for _, a := range []string{"mute", "mute", "ban"} {
		if _, err := store.RecordModeration(ctx, ModerationAction{ChatID: chat, Action: a, RequesterID: 1, TargetUserID: user, UpdateID: time.Now().UnixNano()}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := store.MemberActivityOf(ctx, chat, user, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if a.Muted != 2 || a.Banned != 1 || a.MessagesToday != 0 || !a.LastSeen.IsZero() {
		t.Errorf("activity = %+v", a)
	}
}
