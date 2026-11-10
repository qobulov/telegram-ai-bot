package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

const reminderGroup, carol = int64(-1001), int64(1003)

func addReminder(t *testing.T, store *Store, userID, chatID int64, due time.Time) Reminder {
	t.Helper()
	r, err := store.AddReminder(context.Background(), Reminder{ChatID: chatID, UserID: userID, UserName: "Alice @alice", MessageID: 7, Text: "suv ich", Language: "uz", DueAt: due}, 100, 100)
	if err != nil {
		t.Fatalf("AddReminder: %v", err)
	}
	return r
}

// Calls that overlap share the due reminders: each is claimed by one of
// them, once, and a reminder not yet due by none.
func TestRemindersAreClaimedOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	for range 30 {
		addReminder(t, store, alice, alice, now.Add(-time.Minute))
	}
	later := addReminder(t, store, alice, alice, now.Add(time.Hour))

	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				claimed, err := store.ClaimDueReminders(ctx, now, 4)
				if err != nil {
					t.Errorf("ClaimDueReminders: %v", err)
					return
				}
				if len(claimed) == 0 {
					return
				}
				mu.Lock()
				for _, r := range claimed {
					seen[r.ID]++
					if r.Text != "suv ich" || r.MessageID != 7 || r.Attempts != 1 || r.Kind != ReminderText {
						t.Errorf("claimed = %+v", r)
					}
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(seen) != 30 {
		t.Errorf("claimed %d reminders, want 30", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("reminder %d claimed %d times", id, n)
		}
	}
	if seen[later.ID] != 0 {
		t.Error("a reminder not yet due was claimed")
	}
	if again, err := store.ClaimDueReminders(ctx, now, 50); err != nil || len(again) != 0 {
		t.Errorf("claiming again = %v, %v; want none", again, err)
	}

	// One given back is due again, and claimed again later.
	var id int64
	for id = range seen {
		break
	}
	if err := store.RetryReminder(ctx, id, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.ClaimDueReminders(ctx, now, 50); err != nil || len(claimed) != 0 {
		t.Errorf("before its new time = %v, %v", claimed, err)
	}
	claimed, err := store.ClaimDueReminders(ctx, now.Add(2*time.Minute), 50)
	if err != nil || len(claimed) != 1 || claimed[0].ID != id || claimed[0].Attempts != 2 {
		t.Errorf("after its new time = %+v, %v", claimed, err)
	}
	if err := store.FinishReminder(ctx, id, ReminderSent); err != nil {
		t.Fatal(err)
	}

	if deleted, err := store.DeleteOldReminders(ctx, time.Now().Add(time.Hour)); err != nil || deleted != 30 {
		t.Errorf("deleted = %d, %v; want the 30 claimed", deleted, err)
	}
	if waiting, err := store.WaitingReminders(ctx, alice, alice); err != nil || len(waiting) != 1 || waiting[0].ID != later.ID {
		t.Errorf("waiting = %+v, %v; want the later one", waiting, err)
	}
}

// A member, and a group, may have only so many reminders waiting; and only
// their own, in that chat, can be listed and cancelled.
func TestReminderLimitsAndCancelling(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	due := time.Now().Add(time.Hour)
	add := func(userID, chatID int64) error {
		_, err := store.AddReminder(ctx, Reminder{ChatID: chatID, UserID: userID, Text: "x", Language: "uz", DueAt: due}, 2, 3)
		return err
	}
	for _, step := range []struct {
		user, chat int64
		want       error
	}{
		{alice, alice, nil},
		{alice, reminderGroup, nil},
		{alice, reminderGroup, ErrReminderLimit}, // Alice has 2
		{bob, reminderGroup, nil},
		{carol, reminderGroup, nil},
		{carol, reminderGroup, ErrReminderLimit}, // the group has 3
		{carol, carol, nil},
	} {
		if err := add(step.user, step.chat); !errors.Is(err, step.want) {
			t.Fatalf("user %d in %d: err = %v, want %v", step.user, step.chat, err, step.want)
		}
	}

	mine, err := store.WaitingReminders(ctx, alice, reminderGroup)
	if err != nil || len(mine) != 1 {
		t.Fatalf("Alice's in the group = %+v, %v", mine, err)
	}
	id := mine[0].ID
	if ok, err := store.CancelReminder(ctx, id, bob, reminderGroup); err != nil || ok {
		t.Errorf("Bob cancelling Alice's = %v, %v", ok, err)
	}
	if ok, err := store.CancelReminder(ctx, id, alice, alice); err != nil || ok {
		t.Errorf("Alice cancelling it from another chat = %v, %v", ok, err)
	}
	if ok, err := store.CancelReminder(ctx, id, alice, reminderGroup); err != nil || !ok {
		t.Errorf("Alice cancelling her own = %v, %v", ok, err)
	}
	if err := add(alice, reminderGroup); err != nil {
		t.Errorf("after cancelling, Alice may add one again: %v", err)
	}
}

// A card reminder is one per player; turned on for good, each draw
// schedules the next, and the claim says when they last drew; turned off,
// the one waiting goes too.
func TestCardRemindersFollowDraws(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SaveCardCatalog(ctx, []Card{{ID: 1, Name: "Gojo", Rarity: 0, Attack: 1, Health: 1, Value: 100}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)

	if _, err := store.DrawCard(ctx, alice, "Alice", rarity(0), time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if waiting, _ := store.WaitingReminders(ctx, alice, alice); len(waiting) != 0 {
		t.Fatalf("a draw with the reminders off scheduled %+v", waiting)
	}

	for _, due := range []time.Time{now.Add(30 * time.Minute), now.Add(time.Hour)} {
		if _, err := store.SetCardReminder(ctx, Reminder{ChatID: alice, UserID: alice, Language: "uz", DueAt: due}); err != nil {
			t.Fatal(err)
		}
	}
	waiting, err := store.WaitingReminders(ctx, alice, alice)
	if err != nil || len(waiting) != 1 || !waiting[0].DueAt.Equal(now.Add(time.Hour)) || waiting[0].Kind != ReminderCard {
		t.Fatalf("card reminders = %+v, %v; want one, the later", waiting, err)
	}

	if err := store.SetCardAutoRemind(ctx, alice, "Alice", reminderGroup, "ru"); err != nil {
		t.Fatal(err)
	}
	if chat, err := store.CardAutoRemind(ctx, alice); err != nil || chat != reminderGroup {
		t.Fatalf("auto chat = %d, %v", chat, err)
	}
	next := now.Add(time.Hour)
	if _, err := store.DrawCard(ctx, alice, "Alice", rarity(0), time.Hour, next); err != nil {
		t.Fatal(err)
	}
	if waiting, _ := store.WaitingReminders(ctx, alice, alice); len(waiting) != 0 {
		t.Errorf("the private one stayed after the draw moved it: %+v", waiting)
	}
	waiting, err = store.WaitingReminders(ctx, alice, reminderGroup)
	if err != nil || len(waiting) != 1 || !waiting[0].DueAt.Equal(next.Add(time.Hour)) || waiting[0].Language != "ru" {
		t.Fatalf("after a draw = %+v, %v; want one in the group an hour on", waiting, err)
	}

	claimed, err := store.ClaimDueReminders(ctx, next.Add(2*time.Hour), 10)
	if err != nil || len(claimed) != 1 || !claimed[0].LastDraw.Equal(next) {
		t.Fatalf("claimed = %+v, %v; want the last draw with it", claimed, err)
	}
	// Claimed, it no longer holds the player's one slot.
	if _, err := store.SetCardReminder(ctx, Reminder{ChatID: alice, UserID: alice, Language: "uz", DueAt: next.Add(3 * time.Hour)}); err != nil {
		t.Fatalf("a new card reminder after the claim: %v", err)
	}

	if err := store.SetCardAutoRemind(ctx, alice, "Alice", 0, "uz"); err != nil {
		t.Fatal(err)
	}
	if chat, _ := store.CardAutoRemind(ctx, alice); chat != 0 {
		t.Errorf("auto chat after off = %d", chat)
	}
	if waiting, _ := store.WaitingReminders(ctx, alice, alice); len(waiting) != 0 {
		t.Errorf("turning off left %+v", waiting)
	}
	if chat, err := store.CardAutoRemind(ctx, bob); err != nil || chat != 0 {
		t.Errorf("a stranger's auto chat = %d, %v", chat, err)
	}
}
