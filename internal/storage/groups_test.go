package storage

import (
	"context"
	"reflect"
	"strings"
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

// A new database runs the one-file-per-table migrations and is recorded at
// the version a database that ran the old ones is at, so both skip them
// from then on.
func TestNewDatabaseIsRecordedAtTheMergedVersion(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if version, err := currentVersion(ctx, store.pool); err != nil || version != mergedThrough {
		t.Errorf("version = %d, %v; want %d", version, err, mergedThrough)
	}
	if applied, err := store.Migrate(ctx); err != nil || applied != 0 {
		t.Errorf("second Migrate applied %d, %v; want nothing", applied, err)
	}
	if err := store.CheckSchema(ctx); err != nil {
		t.Errorf("CheckSchema: %v", err)
	}
}

// A database part way through the old migrations would get tables twice or
// miss some from the merged files, so it is stopped with what to do.
func TestDatabaseFromBeforeTheMergeIsStopped(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version > 12`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "merged") {
		t.Errorf("Migrate = %v, want it stopped", err)
	}
}

func TestBotRightsRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	rights, known, err := store.BotRights(ctx, group)
	if err != nil || known || len(rights) != 0 {
		t.Fatalf("a group never seen: rights = %v, known = %v, err = %v; want unknown", rights, known, err)
	}
	// A group logged before its rights were ever stored is still unknown.
	logMessage(t, store, group, "Ann", "hi")
	if _, known, err := store.BotRights(ctx, group); err != nil || known {
		t.Fatalf("a group with no stored rights: known = %v, err = %v", known, err)
	}

	if err := store.RecordGroup(ctx, GroupStatus{ChatID: group, Title: "Devs", BotStatus: "member"}); err != nil {
		t.Fatalf("RecordGroup: %v", err)
	}
	rights, known, err = store.BotRights(ctx, group)
	if err != nil || !known || len(rights) != 0 {
		t.Errorf("after a membership change without rights: rights = %v, known = %v, err = %v; want known and none", rights, known, err)
	}

	want := []string{"can_delete_messages"}
	if err := store.SetBotRights(ctx, group, "administrator", want); err != nil {
		t.Fatalf("SetBotRights: %v", err)
	}
	rights, known, err = store.BotRights(ctx, group)
	if err != nil || !known || !reflect.DeepEqual(rights, want) {
		t.Errorf("rights = %v, known = %v, err = %v; want %v", rights, known, err, want)
	}
	groups, err := store.Groups(ctx, time.Now().Add(-time.Hour), 10)
	if err != nil || len(groups) != 1 {
		t.Fatalf("groups = %+v, err = %v", groups, err)
	}
	if g := groups[0]; g.Title != "Devs" || g.BotStatus != "administrator" || !reflect.DeepEqual(g.Rights, want) {
		t.Errorf("group = %+v; SetBotRights must keep the title and show the new status and rights", g)
	}

	promoted := []string{"can_delete_messages", "can_pin_messages"}
	if err := store.RecordGroup(ctx, GroupStatus{ChatID: group, Title: "Devs", BotStatus: "administrator", Rights: promoted}); err != nil {
		t.Fatal(err)
	}
	if rights, _, _ := store.BotRights(ctx, group); !reflect.DeepEqual(rights, promoted) {
		t.Errorf("rights after a promotion = %v, want %v", rights, promoted)
	}
}

func TestSetBotRightsForAGroupNotSeenYet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SetBotRights(ctx, otherGroup, "member", nil); err != nil {
		t.Fatalf("SetBotRights: %v", err)
	}
	rights, known, err := store.BotRights(ctx, otherGroup)
	if err != nil || !known || len(rights) != 0 {
		t.Errorf("rights = %v, known = %v, err = %v; want known and none", rights, known, err)
	}
}
