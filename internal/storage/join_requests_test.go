package storage

import (
	"context"
	"slices"
	"testing"
	"time"
)

// A request is kept once, taken once, and the late ones are taken oldest
// first, never one whose time is still running.
func TestJoinRequestsAreKeptOnceAndTakenOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	add := func(r JoinRequest) bool {
		t.Helper()
		added, err := store.AddJoinRequest(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		return added
	}
	if !add(JoinRequest{ChatID: -1, UserID: 7, UserChatID: 7, Expires: now.Add(10 * time.Minute)}) {
		t.Fatal("a new request was not added")
	}
	if add(JoinRequest{ChatID: -1, UserID: 7, UserChatID: 7, Expires: now.Add(20 * time.Minute)}) {
		t.Error("the same request was added twice")
	}

	r, found, err := store.TakeJoinRequest(ctx, -1, 7)
	if err != nil || !found || r.UserChatID != 7 || !r.Expires.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("taken = %+v %v %v", r, found, err)
	}
	if _, found, err := store.TakeJoinRequest(ctx, -1, 7); err != nil || found {
		t.Errorf("taken again = %v %v, want nothing", found, err)
	}

	add(JoinRequest{ChatID: -1, UserID: 8, UserChatID: 8, Expires: now.Add(-time.Minute)})
	add(JoinRequest{ChatID: -2, UserID: 9, UserChatID: 9, Expires: now.Add(-2 * time.Minute)})
	add(JoinRequest{ChatID: -1, UserID: 10, UserChatID: 10, Expires: now.Add(time.Minute)})
	late, err := store.TakeLateJoinRequests(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	var users []int64
	for _, r := range late {
		users = append(users, r.UserID)
	}
	if !slices.Equal(users, []int64{9, 8}) {
		t.Errorf("late = %v, want 9 then 8", users)
	}
	if late, _ := store.TakeLateJoinRequests(ctx, now, 10); len(late) != 0 {
		t.Errorf("late again = %+v, want none", late)
	}
	if _, found, _ := store.TakeJoinRequest(ctx, -1, 10); !found {
		t.Error("a request still in time was taken as late")
	}
}

// The joins guard is a rule kind the database takes.
func TestJoinCaptchaIsAGroupRule(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.SetGroupRule(ctx, GroupRule{ChatID: -1, Kind: RuleJoinCaptcha, SetBy: 7}); err != nil {
		t.Fatalf("SetGroupRule: %v", err)
	}
	rules, err := store.GroupRules(ctx, -1)
	if err != nil || len(rules) != 1 || rules[0].Kind != RuleJoinCaptcha {
		t.Errorf("rules = %+v %v", rules, err)
	}
}

// Purge finds one member's logged messages in a group since a moment, each
// once, and nobody else's.
func TestAuthorMessageIDsAreOneMembersSince(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, m := range []GroupMessage{
		{ChatID: -1, MessageID: 11, AuthorID: 7, AuthorName: "Sam", Text: "a"},
		{ChatID: -1, MessageID: 12, AuthorID: 8, AuthorName: "Ann", Text: "b"},
		{ChatID: -1, MessageID: 13, AuthorID: 7, AuthorName: "Sam", Text: "c"},
		{ChatID: -2, MessageID: 14, AuthorID: 7, AuthorName: "Sam", Text: "d"},
		{ChatID: -1, AuthorID: 7, AuthorName: "Sam", Text: "no ID"},
	} {
		if err := store.AppendGroupMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := store.AuthorMessageIDs(ctx, -1, 7, time.Now().Add(-time.Hour), 100)
	if err != nil || !slices.Equal(ids, []int64{11, 13}) {
		t.Errorf("ids = %v %v, want 11 and 13", ids, err)
	}
	if ids, _ := store.AuthorMessageIDs(ctx, -1, 7, time.Now().Add(time.Hour), 100); len(ids) != 0 {
		t.Errorf("ids after now = %v, want none", ids)
	}
	if ids, _ := store.AuthorMessageIDs(ctx, -1, 7, time.Now().Add(-time.Hour), 1); !slices.Equal(ids, []int64{13}) {
		t.Errorf("ids limited to one = %v, want the newest", ids)
	}
}

// A message the swear guard caught is known in every version; another
// message, or another kind of action on it, is not.
func TestSwornInFindsACaughtMessage(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, a := range []ModerationAction{
		{ChatID: -1, Action: ActionSwear, RequesterID: 1, TargetUserID: 7, TargetMessageID: 30, UpdateID: 1},
		{ChatID: -1, Action: "delete", RequesterID: 1, TargetUserID: 7, TargetMessageID: 31, UpdateID: 2},
	} {
		if _, err := store.RecordModeration(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	for messageID, want := range map[int64]bool{30: true, 31: false, 32: false} {
		if caught, err := store.SwornIn(ctx, -1, messageID); err != nil || caught != want {
			t.Errorf("SwornIn(%d) = %v %v, want %v", messageID, caught, err, want)
		}
	}
	if caught, _ := store.SwornIn(ctx, -2, 30); caught {
		t.Error("a message of another group was found")
	}
}
