package storage

import (
	"context"
	"testing"
	"time"
)

func mustRecord(t *testing.T, store *Store, a ModerationAction) bool {
	t.Helper()
	recorded, err := store.RecordModeration(context.Background(), a)
	if err != nil {
		t.Fatalf("RecordModeration(%+v): %v", a, err)
	}
	return recorded
}

func TestModerationAuditIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mute := ModerationAction{ChatID: group, Action: "mute", RequesterID: alice, TargetUserID: bob, TargetMessageID: 40, Minutes: 60, Reason: "spam", UpdateID: 500}

	if !mustRecord(t, store, mute) {
		t.Fatal("the first mute was not recorded")
	}
	if mustRecord(t, store, mute) {
		t.Error("the same update recorded the same action twice")
	}
	// Another action in the same update is an action of its own.
	deletion := ModerationAction{ChatID: group, Action: "delete", RequesterID: alice, TargetUserID: bob, TargetMessageID: 40, TargetText: "buy crypto", UpdateID: 500}
	if !mustRecord(t, store, deletion) {
		t.Error("a delete in the same update was taken for the mute")
	}
	// A confirmed ban acts on a member, not a message.
	ban := ModerationAction{ChatID: group, Action: "ban", RequesterID: alice, TargetUserID: bob, UpdateID: 502}
	mustRecord(t, store, ban)
	mustRecord(t, store, ModerationAction{ChatID: group, Action: "poll", RequesterID: alice, UpdateID: 501})
	mustRecord(t, store, ModerationAction{ChatID: group, Action: "dice", RequesterID: alice, UpdateID: 503})

	var (
		text            string
		minutes, target *int64
	)
	err := store.pool.QueryRow(ctx, `SELECT target_text, minutes FROM moderation_actions WHERE update_id = 500 AND action = 'delete'`).Scan(&text, &minutes)
	if err != nil || text != "buy crypto" || minutes != nil {
		t.Errorf("delete stored text %q, minutes %v, err %v; want the text and no minutes", text, minutes, err)
	}
	err = store.pool.QueryRow(ctx, `SELECT minutes, reason FROM moderation_actions WHERE update_id = 500 AND action = 'mute'`).Scan(&minutes, &text)
	if err != nil || minutes == nil || *minutes != 60 || text != "spam" {
		t.Errorf("mute stored minutes %v, reason %q, err %v", minutes, text, err)
	}
	err = store.pool.QueryRow(ctx, `SELECT target_message_id FROM moderation_actions WHERE action = 'ban'`).Scan(&target)
	if err != nil || target != nil {
		t.Errorf("ban stored message %v, err %v; want none", target, err)
	}

	since := time.Now().Add(-10 * time.Minute)
	if n, err := store.ModerationBy(ctx, alice, since); err != nil || n != 3 {
		t.Errorf("ModerationBy(alice) = %d, %v; want 3, polls and dice left out", n, err)
	}
	if n, _ := store.ModerationBy(ctx, bob, since); n != 0 {
		t.Errorf("ModerationBy(bob) = %d, want 0", n)
	}
	if n, _ := store.ModerationBy(ctx, alice, time.Now().Add(time.Minute)); n != 0 {
		t.Errorf("ModerationBy(alice) from a minute ahead = %d, want 0", n)
	}
	if n, err := store.ChatModeration(ctx, group, "poll", since); err != nil || n != 1 {
		t.Errorf("ChatModeration(group, poll) = %d, %v; want 1", n, err)
	}
	if n, _ := store.ChatModeration(ctx, otherGroup, "poll", since); n != 0 {
		t.Errorf("ChatModeration(otherGroup, poll) = %d, want 0", n)
	}

	// A call Telegram refused is forgotten, so a redelivery may try again.
	if err := store.ForgetModeration(ctx, 500, "mute"); err != nil {
		t.Fatalf("ForgetModeration: %v", err)
	}
	if n := countRows(t, store, "moderation_actions"); n != 3 {
		t.Errorf("audit rows = %d, want 3: forgetting the mute left the delete alone", n)
	}
	if !mustRecord(t, store, mute) {
		t.Error("a forgotten mute could not be recorded again")
	}
}

// The swear guard counts one member's caught swears in one chat, recent
// ones only, and a redelivered update once.
func TestSwearStrikesCountOneMembersRecentSwears(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	swear := func(chatID, userID, updateID int64) {
		mustRecord(t, store, ModerationAction{ChatID: chatID, Action: ActionSwear, RequesterID: alice, TargetUserID: userID, Reason: "swearing", UpdateID: updateID})
	}
	swear(group, bob, 900)
	swear(group, bob, 900) // delivered again
	swear(group, bob, 901)
	swear(otherGroup, bob, 902)
	swear(group, alice, 903)
	mustRecord(t, store, ModerationAction{ChatID: group, Action: "mute", RequesterID: alice, TargetUserID: bob, Minutes: 60, UpdateID: 904})

	if n, err := store.SwearStrikes(ctx, group, bob, ActionSwear, time.Now().Add(-time.Hour)); err != nil || n != 2 {
		t.Errorf("strikes = %d, %v; want 2", n, err)
	}
	if n, err := store.SwearStrikes(ctx, group, bob, ActionSwear, time.Now().Add(time.Minute)); err != nil || n != 0 {
		t.Errorf("strikes since later = %d, %v; want 0", n, err)
	}
}
