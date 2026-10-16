package storage

import (
	"context"
	"errors"
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
		t.Errorf("ModerationBy(alice) = %d, %v; want 3, polls left out", n, err)
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
	if !mustRecord(t, store, mute) {
		t.Error("a forgotten mute could not be recorded again")
	}
	if n := countRows(t, store, "moderation_actions"); n != 4 {
		t.Errorf("audit rows = %d, want 4: forgetting the mute left the delete alone", n)
	}
}

func TestPendingBanMustBeConfirmedLater(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	save := func(b PendingBan) {
		t.Helper()
		if err := store.SavePendingBan(ctx, b); err != nil {
			t.Fatalf("SavePendingBan: %v", err)
		}
	}
	take := func(requesterID, chatID, updateID int64) (PendingBan, error) {
		return store.TakePendingBan(ctx, requesterID, chatID, updateID, 5*time.Minute)
	}
	asked := PendingBan{RequesterID: alice, ChatID: group, TargetUserID: bob, TargetName: "Bob @bob", UpdateID: 10}
	save(asked)

	for name, try := range map[string][3]int64{
		"in the same message": {alice, group, 10},
		"by someone else":     {bob, group, 11},
		"in another chat":     {alice, otherGroup, 11},
	} {
		if _, err := take(try[0], try[1], try[2]); !errors.Is(err, ErrNoPendingBan) {
			t.Errorf("confirmed %s: err = %v, want ErrNoPendingBan", name, err)
		}
	}
	got, err := take(alice, group, 11)
	if err != nil || got != asked {
		t.Fatalf("confirmed = %+v, %v; want %+v", got, err, asked)
	}
	if _, err := take(alice, group, 12); !errors.Is(err, ErrNoPendingBan) {
		t.Errorf("confirmed twice: err = %v", err)
	}

	// Asking again replaces the ban asked for before.
	save(asked)
	newer := PendingBan{RequesterID: alice, ChatID: group, TargetUserID: 1003, TargetName: "Cy", UpdateID: 20}
	save(newer)
	if got, err := take(alice, group, 21); err != nil || got != newer {
		t.Errorf("confirmed = %+v, %v; want the newer ban %+v", got, err, newer)
	}

	save(PendingBan{RequesterID: alice, ChatID: group, TargetUserID: bob, TargetName: "Bob", UpdateID: 30})
	if _, err := store.pool.Exec(ctx, `UPDATE pending_bans SET created_at = now() - interval '10 minutes'`); err != nil {
		t.Fatal(err)
	}
	if _, err := take(alice, group, 31); !errors.Is(err, ErrNoPendingBan) {
		t.Errorf("a ban asked for 10 minutes ago was confirmed: err = %v", err)
	}
}

func TestCancelPendingBan(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SavePendingBan(ctx, PendingBan{RequesterID: alice, ChatID: group, TargetUserID: bob, TargetName: "Bob", UpdateID: 40}); err != nil {
		t.Fatal(err)
	}

	if cancelled, err := store.CancelPendingBan(ctx, alice, otherGroup); err != nil || cancelled {
		t.Errorf("cancelled in another chat = %v, %v", cancelled, err)
	}
	if cancelled, err := store.CancelPendingBan(ctx, alice, group); err != nil || !cancelled {
		t.Fatalf("cancelled = %v, %v", cancelled, err)
	}
	if cancelled, _ := store.CancelPendingBan(ctx, alice, group); cancelled {
		t.Error("cancelled twice")
	}
	if _, err := store.TakePendingBan(ctx, alice, group, 41, 5*time.Minute); !errors.Is(err, ErrNoPendingBan) {
		t.Errorf("a cancelled ban was confirmed: err = %v", err)
	}
}
