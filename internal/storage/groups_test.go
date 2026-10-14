package storage

import (
	"context"
	"testing"
	"time"
)

func TestLoggingAGroupMessageRecordsTheGroup(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	err := store.AppendGroupMessage(ctx, GroupMessage{ChatID: group, ChatTitle: "Devs", ChatUsername: "devs_uz", AuthorName: "Ann", Text: "hi"})
	if err != nil {
		t.Fatalf("AppendGroupMessage: %v", err)
	}
	// The bot's own messages are logged without a title; it must survive.
	logMessage(t, store, group, "You", "hello")
	logMessage(t, store, otherGroup, "Bek", "elsewhere")

	groups, err := store.Groups(ctx, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %+v, want 2", groups)
	}
	devs := groups[0]
	if devs.ChatID != group {
		devs = groups[1]
	}
	if devs.Title != "Devs" || devs.Username != "devs_uz" || devs.MessagesToday != 2 || devs.LastSeen.IsZero() {
		t.Errorf("group = %+v", devs)
	}
}

func TestGroupsOutliveTheRetention(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	logMessage(t, store, group, "Ann", "old")
	if _, err := store.pool.Exec(ctx, `UPDATE group_messages SET created_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Expire(ctx, 24*time.Hour); err != nil {
		t.Fatalf("Expire: %v", err)
	}

	groups, err := store.Groups(ctx, time.Now().Add(-time.Hour), 10)
	if err != nil || len(groups) != 1 || groups[0].MessagesToday != 0 {
		t.Errorf("groups = %+v, err = %v; the group should stay, with no messages today", groups, err)
	}
}

func TestOverviewCountsWhatTheBotKnows(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	logMessage(t, store, group, "Ann", "a")
	logMessage(t, store, group, "Ann", "b")
	logMessage(t, store, otherGroup, "Bek", "c")
	if err := store.SetMuted(ctx, otherGroup, true, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO users (telegram_id, last_seen_at) VALUES ($1, now()), ($2, now() - interval '3 days')`, alice, bob); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReserveImage(ctx, alice, group, time.Now().Add(-time.Hour), -1); err != nil {
		t.Fatal(err)
	}

	got, err := store.Overview(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	want := Overview{Groups: 2, MutedGroups: 1, Users: 2, UsersToday: 1, GroupMessagesToday: 3, ImagesToday: 1}
	if got != want {
		t.Errorf("overview = %+v, want %+v", got, want)
	}
}

func TestRecordGroupKeepsTheBotsStatus(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.RecordGroup(ctx, GroupStatus{ChatID: group, Title: "Devs", BotStatus: "administrator"}); err != nil {
		t.Fatalf("RecordGroup: %v", err)
	}
	// A later message must not reset what Telegram said about the bot.
	logMessage(t, store, group, "Ann", "hi")

	groups, err := store.Groups(ctx, time.Now().Add(-time.Hour), 10)
	if err != nil || len(groups) != 1 || groups[0].BotStatus != "administrator" || groups[0].Title != "Devs" {
		t.Errorf("groups = %+v, err = %v", groups, err)
	}
	if err := store.RecordGroup(ctx, GroupStatus{ChatID: group, Title: "Devs", BotStatus: "kicked"}); err != nil {
		t.Fatal(err)
	}
	if groups, _ := store.Groups(ctx, time.Now().Add(-time.Hour), 10); groups[0].BotStatus != "kicked" {
		t.Errorf("status after removal = %q", groups[0].BotStatus)
	}
}

func TestMigrationRecordsGroupsSeenBefore(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	logMessage(t, store, group, "Ann", "hi")
	if err := store.SetMuted(ctx, otherGroup, true, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM groups; DELETE FROM schema_migrations WHERE version = 9`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	groups, err := store.Groups(ctx, time.Now().Add(-time.Hour), 10)
	if err != nil || len(groups) != 2 {
		t.Errorf("groups = %+v, err = %v; both groups the bot knew of should be listed", groups, err)
	}
}
