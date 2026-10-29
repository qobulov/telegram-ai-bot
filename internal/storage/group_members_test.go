package storage

import (
	"context"
	"testing"
	"time"

	"telegram-ai-bot/internal/ai"
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

// A person's own exchanges with the bot in other chats, with the answers;
// never someone else's, never this chat's, never older than since.
func TestExchangesElsewhereAreThePersonsOwn(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	save := func(scope Scope, update, msgID, author int64, name, asked, answer string) {
		t.Helper()
		if _, err := store.SaveTurn(ctx, Turn{Scope: scope, UpdateID: update, MessageID: msgID, AuthorID: author, AuthorName: name,
			Messages: []ai.Message{{Role: ai.RoleUser, Content: asked}, {Role: ai.RoleAssistant, Content: answer}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AppendGroupMessage(ctx, GroupMessage{ChatID: otherGroup, ChatTitle: "Do'stlar", MessageID: 1, AuthorID: alice, AuthorName: "Ann", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	save(Scope{ChatID: otherGroup}, 1, 10, alice, "Ann", "Ann: qiz bilan qanday yarashsa bo'ladi", "Aybingni tan ol")
	save(Scope{ChatID: otherGroup}, 2, 11, bob, "Bob", "Bob: menga ham maslahat", "Senga yo'q")
	save(Scope{ChatID: group}, 3, 12, alice, "Ann", "Ann: bu guruhda", "shu yerda")
	save(private(alice), 4, 13, alice, "", "shaxsiyda", "ha")

	got, err := store.ExchangesElsewhere(ctx, alice, group, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("ExchangesElsewhere: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("exchanges = %+v, want Ann's in the other group and in private", got)
	}
	if e := got[0]; e.ChatID != otherGroup || e.ChatTitle != "Do'stlar" || e.Asked != "Ann: qiz bilan qanday yarashsa bo'ladi" || e.Answer != "Aybingni tan ol" {
		t.Errorf("first = %+v", e)
	}
	if e := got[1]; e.ChatID != alice || e.ChatTitle != "" || e.Asked != "shaxsiyda" {
		t.Errorf("second = %+v", e)
	}
	if late, _ := store.ExchangesElsewhere(ctx, alice, group, time.Now().Add(time.Hour), 10); len(late) != 0 {
		t.Errorf("since the future = %+v", late)
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

// The bots seen in a group, besides the bot itself and those that left.
func TestOtherBotsCountsTheBotsStillThere(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const self = int64(777)
	for _, m := range []GroupMember{
		{ChatID: group, UserID: self, FirstName: "Maria", IsBot: true},
		{ChatID: group, UserID: 801, FirstName: "Navo", IsBot: true},
		{ChatID: group, UserID: 802, FirstName: "Gone", IsBot: true, Status: "left"},
		{ChatID: group, UserID: alice, FirstName: "Alice"},
		{ChatID: otherGroup, UserID: 803, FirstName: "Elsewhere", IsBot: true},
	} {
		if err := store.SeeMember(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := store.OtherBots(ctx, group, self); err != nil || n != 1 {
		t.Errorf("OtherBots = %d, %v; want 1", n, err)
	}
}

// Active members are the people seen writing since then who are still in
// the group: not bots, not those seen before, not those who left.
func TestActiveMembersCountsRecentPeople(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const group = int64(-100700)
	now := time.Now()
	for _, m := range []GroupMember{
		{ChatID: group, UserID: 1, FirstName: "A", LastSeen: now},
		{ChatID: group, UserID: 2, FirstName: "B", LastSeen: now.Add(-24 * time.Hour)},
		{ChatID: group, UserID: 3, FirstName: "Old", LastSeen: now.Add(-5 * 24 * time.Hour)},
		{ChatID: group, UserID: 4, FirstName: "Bot", IsBot: true, LastSeen: now},
		{ChatID: group, UserID: 5, FirstName: "Gone", Status: "left", LastSeen: now},
		{ChatID: -100800, UserID: 6, FirstName: "Elsewhere", LastSeen: now},
	} {
		if err := store.SeeMember(ctx, m); err != nil {
			t.Fatal(err)
		}
		// SeeMember stamps now; the test dates the earlier sightings.
		if _, err := store.pool.Exec(ctx, `UPDATE group_members SET last_seen_at = $3 WHERE chat_id = $1 AND user_id = $2`, m.ChatID, m.UserID, m.LastSeen); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := store.ActiveMembers(ctx, group, now.Add(-3*24*time.Hour)); err != nil || count != 2 {
		t.Errorf("active = %d, %v; want 2", count, err)
	}
}

// The dashboard offers a user the groups they are still in, where the bot
// is too, the latest seen first.
func TestMemberGroupsAreTheOnesStillShared(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const leftGroup, botGone = int64(-1007777777777), int64(-1006666666666)
	for _, g := range []GroupStatus{
		{ChatID: group, Title: "Choyxona", BotStatus: "administrator"},
		{ChatID: otherGroup, Title: "Kitobxonlar", BotStatus: "member"},
		{ChatID: leftGroup, Title: "Left", BotStatus: "member"},
		{ChatID: botGone, Title: "Bot gone", BotStatus: "kicked"},
	} {
		if err := store.RecordGroup(ctx, g); err != nil {
			t.Fatalf("RecordGroup: %v", err)
		}
	}
	now := time.Now()
	for _, m := range []GroupMember{
		{ChatID: group, UserID: alice, FirstName: "Ann", LastSeen: now},
		{ChatID: otherGroup, UserID: alice, FirstName: "Ann", LastSeen: now.Add(-time.Hour)},
		{ChatID: leftGroup, UserID: alice, FirstName: "Ann", Status: "left", LastSeen: now},
		{ChatID: botGone, UserID: alice, FirstName: "Ann", LastSeen: now},
		{ChatID: group, UserID: bob, FirstName: "Bek", LastSeen: now},
	} {
		if err := store.SeeMember(ctx, m); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `UPDATE group_members SET last_seen_at = $3 WHERE chat_id = $1 AND user_id = $2`, m.ChatID, m.UserID, m.LastSeen); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := store.MemberGroups(ctx, alice, 10)
	if err != nil {
		t.Fatalf("MemberGroups: %v", err)
	}
	want := []MemberGroup{{ChatID: group, Title: "Choyxona"}, {ChatID: otherGroup, Title: "Kitobxonlar"}}
	if len(groups) != 2 || groups[0] != want[0] || groups[1] != want[1] {
		t.Errorf("groups = %+v, want %+v", groups, want)
	}
}

// A member known from a reply is added as of when they wrote; a known one
// keeps their status, is seen later only by a later message, and does not
// count as active for an old one.
func TestKnowMemberKeepsStatusAndActivity(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const chat = -100777
	old := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	if err := store.KnowMember(ctx, GroupMember{ChatID: chat, UserID: 1, FirstName: "Quiet", LastSeen: old}); err != nil {
		t.Fatal(err)
	}
	if err := store.SeeMember(ctx, GroupMember{ChatID: chat, UserID: 2, FirstName: "Gone", Status: "left"}); err != nil {
		t.Fatal(err)
	}
	if err := store.KnowMember(ctx, GroupMember{ChatID: chat, UserID: 2, FirstName: "Gone again", LastSeen: old}); err != nil {
		t.Fatal(err)
	}
	members, known, err := store.GroupMembers(ctx, chat, 10)
	if err != nil || known != 1 || members[0].UserID != 1 || !members[0].LastSeen.Equal(old) || members[0].Status != "member" {
		t.Fatalf("members = %+v, %d, %v; want Quiet alone, as of a month ago, and Gone still gone", members, known, err)
	}
	if active, _ := store.ActiveMembers(ctx, chat, time.Now().Add(-3*24*time.Hour)); active != 0 {
		t.Errorf("active = %d, want 0: neither wrote lately", active)
	}
}
