package storage

import (
	"context"
	"testing"
	"time"
)

func TestGroupMembersAreTheOnesSeenAndStillThere(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	see := func(m GroupMember) {
		t.Helper()
		if err := store.SeeMember(ctx, m); err != nil {
			t.Fatalf("SeeMember: %v", err)
		}
	}
	see(GroupMember{ChatID: group, UserID: alice, FirstName: "Ann", Username: "ann"})
	see(GroupMember{ChatID: group, UserID: bob, FirstName: "Bek", Status: "administrator"})
	see(GroupMember{ChatID: group, UserID: 4242, FirstName: "Gone"})
	see(GroupMember{ChatID: group, UserID: 4242, FirstName: "Gone", Status: "left"})
	see(GroupMember{ChatID: otherGroup, UserID: 5151, FirstName: "Elsewhere"})
	// A later message keeps a known status and the newest name.
	see(GroupMember{ChatID: group, UserID: bob, FirstName: "Bekzod", Username: "bekzod"})

	members, total, err := store.GroupMembers(ctx, group, 10)
	if err != nil {
		t.Fatalf("GroupMembers: %v", err)
	}
	if total != 2 || len(members) != 2 {
		t.Fatalf("members = %+v, total %d; want Ann and Bek only", members, total)
	}
	if m := members[0]; m.UserID != bob || m.FirstName != "Bekzod" || m.Username != "bekzod" || m.Status != "administrator" || m.LastSeen.IsZero() {
		t.Errorf("most recent = %+v, want Bekzod, still an administrator", m)
	}
	if m := members[1]; m.UserID != alice || m.Status != "member" {
		t.Errorf("second = %+v, want Ann as a member", m)
	}

	// Someone who left and writes again is back.
	see(GroupMember{ChatID: group, UserID: 4242, FirstName: "Back"})
	if members, total, _ := store.GroupMembers(ctx, group, 1); len(members) != 1 || total != 3 {
		t.Errorf("limit 1: %d members, total %d; want 1 of 3", len(members), total)
	}
}

// A group passes one message a day to its owners; one given back can be
// taken again, and another group has its own.
func TestOwnerNoticeIsOnceADayPerGroup(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	today := time.Now().Add(-time.Hour)
	id, ok, err := store.ReserveOwnerNotice(ctx, group, alice, today)
	if err != nil || !ok || id == 0 {
		t.Fatalf("first = %d, %v, %v", id, ok, err)
	}
	if _, ok, err := store.ReserveOwnerNotice(ctx, group, bob, today); err != nil || ok {
		t.Errorf("second the same day = %v, %v; want refused", ok, err)
	}
	if _, ok, _ := store.ReserveOwnerNotice(ctx, otherGroup, bob, today); !ok {
		t.Error("another group was refused")
	}
	if err := store.ReleaseOwnerNotice(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.ReserveOwnerNotice(ctx, group, bob, today); !ok {
		t.Error("a released notice could not be taken again")
	}
	if _, ok, _ := store.ReserveOwnerNotice(ctx, group, bob, time.Now().Add(time.Minute)); !ok {
		t.Error("a new day was refused")
	}
}

// A profile is kept for the person across chats until they change or clear
// it; one that asks for nothing is not kept.
func TestProfileIsSetChangedAndCleared(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if p, err := store.Profile(ctx, alice); err != nil || !p.Empty() || p.UserID != alice {
		t.Fatalf("no profile = %+v, %v", p, err)
	}
	if err := store.SetProfile(ctx, UserProfile{UserID: alice, CallName: "Aziz aka", Siz: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProfile(ctx, UserProfile{UserID: alice, CallName: "Aziz", Siz: true}); err != nil {
		t.Fatal(err)
	}
	if p, _ := store.Profile(ctx, alice); p.CallName != "Aziz" || !p.Siz {
		t.Errorf("profile = %+v, want Aziz with siz", p)
	}
	if p, _ := store.Profile(ctx, bob); !p.Empty() {
		t.Errorf("another user has %+v", p)
	}
	if err := store.SetProfile(ctx, UserProfile{UserID: alice}); err != nil {
		t.Fatal(err)
	}
	if p, _ := store.Profile(ctx, alice); !p.Empty() {
		t.Errorf("an empty profile was kept: %+v", p)
	}
}

// Members who left are forgotten after 90 days; those still there are kept.
func TestExpireForgetsMembersWhoLeftLongAgo(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, m := range []GroupMember{
		{ChatID: group, UserID: alice, FirstName: "Ann"},
		{ChatID: group, UserID: bob, FirstName: "Bek", Status: "kicked"},
		{ChatID: group, UserID: 4242, FirstName: "Recent", Status: "left"},
	} {
		if err := store.SeeMember(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(ctx, `UPDATE group_members SET last_seen_at = now() - interval '91 days' WHERE user_id IN ($1, $2)`, alice, bob); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Expire(ctx, 24*time.Hour); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	var kept []int64
	rows, err := store.pool.Query(ctx, `SELECT user_id FROM group_members ORDER BY user_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		kept = append(kept, id)
	}
	rows.Close()
	if len(kept) != 2 || kept[0] != alice || kept[1] != 4242 {
		t.Errorf("kept %v; want Ann (still there) and the recent leaver, not Bek", kept)
	}
}
