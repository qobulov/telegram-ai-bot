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

	moderated := func(updateID int64) bool {
		t.Helper()
		ok, err := store.UpdateModerated(ctx, updateID)
		if err != nil {
			t.Fatalf("UpdateModerated(%d): %v", updateID, err)
		}
		return ok
	}
	if !moderated(500) || !moderated(502) {
		t.Error("an update that recorded an action was not found")
	}
	// A poll is not moderation.
	if moderated(501) || moderated(503) {
		t.Error("an action found in an update that did not record one")
	}

	// A call Telegram refused is forgotten, so a redelivery may try again.
	if err := store.ForgetModeration(ctx, 500, "mute"); err != nil {
		t.Fatalf("ForgetModeration: %v", err)
	}
	if err := store.ForgetModeration(ctx, 500, "delete"); err != nil {
		t.Fatalf("ForgetModeration: %v", err)
	}
	if moderated(500) {
		t.Error("an update whose actions were all forgotten was still found")
	}
	mustRecord(t, store, deletion)
	if !mustRecord(t, store, mute) {
		t.Error("a forgotten mute could not be recorded again")
	}
	if n := countRows(t, store, "moderation_actions"); n != 4 {
		t.Errorf("audit rows = %d, want 4: forgetting the mute left the delete alone", n)
	}
}

// asked is what alice asked the bot to do to bob in group, in update 10.
func asked() PendingModeration {
	return PendingModeration{
		RequesterID: alice, ChatID: group, Action: "mute", TargetUserID: bob, TargetMessageID: 40,
		TargetName: "Bob @bob", TargetText: "buy crypto", Minutes: 90, Reason: "spam", UpdateID: 10,
	}
}

func mustSavePending(t *testing.T, store *Store, p PendingModeration) bool {
	t.Helper()
	saved, err := store.SavePendingModeration(context.Background(), p)
	if err != nil {
		t.Fatalf("SavePendingModeration: %v", err)
	}
	return saved
}

func mustSetPrompt(t *testing.T, store *Store, p PendingModeration, promptID int64) {
	t.Helper()
	if err := store.SetModerationPrompt(context.Background(), p.RequesterID, p.ChatID, p.UpdateID, promptID); err != nil {
		t.Fatalf("SetModerationPrompt: %v", err)
	}
}

func TestPendingModerationIsConfirmedByAReplyToItsQuestion(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	take := func(requesterID, chatID, promptID int64) (PendingModeration, error) {
		return store.TakePendingModeration(ctx, requesterID, chatID, promptID, 5*time.Minute)
	}
	want := asked()
	if !mustSavePending(t, store, want) {
		t.Fatal("the action was not saved")
	}
	// Before the question is posted, nothing can confirm it.
	if _, err := take(alice, group, 0); !errors.Is(err, ErrNoPendingModeration) {
		t.Errorf("confirmed before the question was asked: err = %v", err)
	}
	mustSetPrompt(t, store, want, 77)

	for name, try := range map[string][3]int64{
		"by a reply to another message": {alice, group, 76},
		"by someone else":               {bob, group, 77},
		"in another chat":               {alice, otherGroup, 77},
	} {
		if _, err := take(try[0], try[1], try[2]); !errors.Is(err, ErrNoPendingModeration) {
			t.Errorf("confirmed %s: err = %v, want ErrNoPendingModeration", name, err)
		}
	}
	got, err := take(alice, group, 77)
	if err != nil || got != want {
		t.Fatalf("confirmed = %+v, %v\nwant        %+v", got, err, want)
	}
	if _, err := take(alice, group, 77); !errors.Is(err, ErrNoPendingModeration) {
		t.Errorf("confirmed twice: err = %v", err)
	}
}

func TestPendingModerationRedeliveredIsSavedOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	first := asked()
	mustSavePending(t, store, first)
	mustSetPrompt(t, store, first, 77)

	// Telegram delivered the update that asked again: it neither replaces
	// the action nor forgets the question already asked.
	if mustSavePending(t, store, first) {
		t.Error("the same update saved its action twice")
	}
	if got, err := store.TakePendingModeration(ctx, alice, group, 77, 5*time.Minute); err != nil || got != first {
		t.Errorf("confirmed = %+v, %v; want the first %+v", got, err, first)
	}
}

// "Delete this and ban him" asks for two actions in one update. The second
// is not a redelivery: it is told apart, and the first still waits.
func TestPendingModerationSecondActionOfAnUpdateIsToldApart(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	first := asked()
	mustSavePending(t, store, first)
	mustSetPrompt(t, store, first, 77)

	second := first
	second.Action = "ban"
	saved, err := store.SavePendingModeration(ctx, second)
	if saved || !errors.Is(err, ErrOtherModerationPending) {
		t.Errorf("second action = %v, %v; want ErrOtherModerationPending", saved, err)
	}
	if got, err := store.TakePendingModeration(ctx, alice, group, 77, 5*time.Minute); err != nil || got != first {
		t.Errorf("confirmed = %+v, %v; want the first %+v", got, err, first)
	}
}

func TestPendingModerationIsReplacedAndExpires(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	old := asked()
	mustSavePending(t, store, old)
	mustSetPrompt(t, store, old, 77)
	age := func() {
		t.Helper()
		if _, err := store.pool.Exec(ctx, `UPDATE pending_moderation SET created_at = now() - interval '10 minutes'`); err != nil {
			t.Fatal(err)
		}
	}
	age()

	// Asking again replaces what was asked before, and waits from now: the
	// old question no longer confirms anything.
	newer := PendingModeration{RequesterID: alice, ChatID: group, Action: "ban", TargetUserID: 1003, TargetMessageID: 41, TargetName: "Cy", UpdateID: 20}
	if !mustSavePending(t, store, newer) {
		t.Fatal("a later request was not saved")
	}
	mustSetPrompt(t, store, newer, 88)
	if _, err := store.TakePendingModeration(ctx, alice, group, 77, 5*time.Minute); !errors.Is(err, ErrNoPendingModeration) {
		t.Errorf("the replaced question confirmed something: err = %v", err)
	}
	if got, err := store.TakePendingModeration(ctx, alice, group, 88, 5*time.Minute); err != nil || got != newer {
		t.Errorf("confirmed = %+v, %v; want the newer %+v", got, err, newer)
	}

	mustSavePending(t, store, PendingModeration{RequesterID: alice, ChatID: group, Action: "pin", TargetUserID: bob, TargetMessageID: 42, TargetName: "Bob", UpdateID: 30})
	mustSetPrompt(t, store, PendingModeration{RequesterID: alice, ChatID: group, UpdateID: 30}, 99)
	age()
	if _, err := store.TakePendingModeration(ctx, alice, group, 99, 5*time.Minute); !errors.Is(err, ErrNoPendingModeration) {
		t.Errorf("an action asked for 10 minutes ago was confirmed: err = %v", err)
	}
}

func TestCancelPendingModeration(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mustSavePending(t, store, asked())
	mustSetPrompt(t, store, asked(), 77)

	if cancelled, err := store.CancelPendingModeration(ctx, alice, otherGroup); err != nil || cancelled {
		t.Errorf("cancelled in another chat = %v, %v", cancelled, err)
	}
	if cancelled, err := store.CancelPendingModeration(ctx, alice, group); err != nil || !cancelled {
		t.Fatalf("cancelled = %v, %v", cancelled, err)
	}
	if cancelled, _ := store.CancelPendingModeration(ctx, alice, group); cancelled {
		t.Error("cancelled twice")
	}
	if _, err := store.TakePendingModeration(ctx, alice, group, 77, 5*time.Minute); !errors.Is(err, ErrNoPendingModeration) {
		t.Errorf("a cancelled action was confirmed: err = %v", err)
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

	if n, err := store.SwearStrikes(ctx, group, bob, time.Now().Add(-time.Hour)); err != nil || n != 2 {
		t.Errorf("strikes = %d, %v; want 2", n, err)
	}
	if n, err := store.SwearStrikes(ctx, group, bob, time.Now().Add(time.Minute)); err != nil || n != 0 {
		t.Errorf("strikes since later = %d, %v; want 0", n, err)
	}
}
