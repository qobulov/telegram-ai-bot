package storage

import (
	"context"
	"testing"
	"time"
)

// Each person counts once for the member who brought them; the table puts
// first who brought the most still in the group; a period leaves out older
// ones.
func TestInvitesAreCounted(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const chat = -100
	now := time.Now()
	add := func(user, inviter int64, name string, at time.Time) {
		t.Helper()
		if err := store.RecordInvite(ctx, Invite{ChatID: chat, UserID: user, InviterID: inviter, InviterName: name, Via: "add", At: at}); err != nil {
			t.Fatal(err)
		}
	}
	mark := func(user int64, in bool) {
		t.Helper()
		if err := store.MarkInvitee(ctx, chat, user, in, now); err != nil {
			t.Fatal(err)
		}
	}
	add(11, 1, "Ali", now)
	add(12, 1, "Ali", now)
	add(13, 1, "Ali", now)
	add(14, 1, "Ali", now)
	add(21, 2, "Vali", now)
	add(22, 2, "Vali", now)
	add(41, 4, "Soli", now)
	add(31, 3, "Gani", now.Add(-10*24*time.Hour)) // last week
	mark(12, false)
	mark(12, false) // told twice: by the update and by the message
	mark(13, false)
	mark(14, false)
	mark(14, true) // came back on their own
	add(13, 1, "Ali", now)
	mark(13, false) // brought back by the same one, and gone again: counted once
	mark(41, false) // Soli's only one left
	week := now.Add(-7 * 24 * time.Hour)
	top, err := store.InviteTop(ctx, chat, week, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || top[0].Name != "Vali" || top[0].Added != 2 || top[0].Stayed != 2 || top[1].Name != "Ali" || top[1].Added != 4 || top[1].Stayed != 2 {
		t.Fatalf("this week = %+v, want Vali and Ali, and not Soli, whose one left", top)
	}
	all, _ := store.InviteTop(ctx, chat, time.Time{}, 0, 10)
	if len(all) != 3 {
		t.Errorf("all time = %+v", all)
	}
	soli, _ := store.InviteTop(ctx, chat, week, 4, 10)
	if len(soli) != 1 || soli[0].Added != 1 || soli[0].Stayed != 0 {
		t.Errorf("Soli's own = %+v, want shown with none stayed", soli)
	}
	add(41, 4, "Soli", now) // brought back
	if back, _ := store.InviteTop(ctx, chat, week, 0, 10); len(back) != 3 {
		t.Errorf("after Soli's one came back = %+v", back)
	}
}
