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
	add(11, 1, "Ali", now)
	add(12, 1, "Ali", now)
	add(12, 1, "Ali", now) // came back: once
	add(13, 1, "Ali", now)
	add(21, 2, "Vali", now)
	add(22, 2, "Vali", now)
	add(31, 3, "Gani", now.Add(-10*24*time.Hour)) // last week
	if _, err := store.pool.Exec(ctx, `INSERT INTO group_members (chat_id, user_id, first_name, status) VALUES (-100, 12, 'x', 'left'), (-100, 13, 'y', 'kicked')`); err != nil {
		t.Fatal(err)
	}
	week := now.Add(-7 * 24 * time.Hour)
	top, err := store.InviteTop(ctx, chat, week, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || top[0].Name != "Vali" || top[0].Added != 2 || top[0].Stayed != 2 || top[1].Name != "Ali" || top[1].Added != 3 || top[1].Stayed != 1 {
		t.Fatalf("this week = %+v", top)
	}
	all, _ := store.InviteTop(ctx, chat, time.Time{}, 0, 10)
	if len(all) != 3 {
		t.Errorf("all time = %+v", all)
	}
	one, _ := store.InviteTop(ctx, chat, week, 1, 10)
	if len(one) != 1 || one[0].UserID != 1 || one[0].Added != 3 {
		t.Errorf("Ali's = %+v", one)
	}
}
