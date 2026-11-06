package storage

import (
	"context"
	"testing"
	"time"
)

func TestGroupStatsCountsTheWritersWithoutTheBot(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const botID = int64(4242)
	for i, m := range []GroupMessage{
		{AuthorID: alice, AuthorName: "Ali", Text: "salom"},
		{AuthorID: bob, AuthorName: "Bek", Text: "qalay"},
		{AuthorID: alice, AuthorName: "Ali", Text: "zo'r"},
		{AuthorID: alice, AuthorName: "Ali ✨", Text: "yana"},
		{AuthorID: botID, AuthorName: "You", Text: "men ham"},
	} {
		m.ChatID, m.MessageID = group, int64(100+i)
		if err := store.AppendGroupMessage(ctx, m); err != nil {
			t.Fatalf("AppendGroupMessage: %v", err)
		}
	}
	if err := store.AppendGroupMessage(ctx, GroupMessage{ChatID: otherGroup, MessageID: 1, AuthorID: bob, AuthorName: "Bek", Text: "elsewhere"}); err != nil {
		t.Fatalf("AppendGroupMessage: %v", err)
	}
	if err := store.SeeMember(ctx, GroupMember{ChatID: group, UserID: bob, FirstName: "Bek", Status: "member"}); err != nil {
		t.Fatalf("SeeMember: %v", err)
	}
	for i := range 2 {
		if _, err := store.RecordModeration(ctx, ModerationAction{ChatID: group, Action: ActionSwear, RequesterID: botID, TargetUserID: bob, UpdateID: int64(900 + i)}); err != nil {
			t.Fatalf("RecordModeration: %v", err)
		}
	}

	stats, err := store.GroupStats(ctx, group, botID, time.Now().Add(-time.Hour), "Asia/Tashkent", 5)
	if err != nil {
		t.Fatalf("GroupStats: %v", err)
	}
	if stats.Messages != 4 || stats.Members != 2 {
		t.Errorf("messages %d, members %d; want 4 and 2", stats.Messages, stats.Members)
	}
	if len(stats.Top) != 2 || stats.Top[0] != (AuthorCount{UserID: alice, Name: "Ali ✨", Count: 3}) || stats.Top[1].UserID != bob {
		t.Errorf("top = %+v", stats.Top)
	}
	if len(stats.Swearers) != 1 || stats.Swearers[0] != (AuthorCount{UserID: bob, Name: "Bek", Count: 2}) {
		t.Errorf("swearers = %+v", stats.Swearers)
	}
	tashkent, err := time.LoadLocation("Asia/Tashkent")
	if err != nil {
		t.Fatal(err)
	}
	if hour := time.Now().In(tashkent).Hour(); stats.Hours[hour] != 4 {
		t.Errorf("hours = %v, want 4 at %d", stats.Hours, hour)
	}
}
