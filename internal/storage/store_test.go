package storage

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/ai"
)

const alice, bob = int64(1001), int64(1002)

// group is a supergroup chat ID; Telegram's are negative.
const group = int64(-1001234567890)

// private is the scope of a user's one-to-one chat with the bot, where the
// chat ID equals the user ID.
func private(userID int64) Scope {
	return Scope{ChatID: userID, UserID: userID}
}

func inGroup(userID int64) Scope {
	return Scope{ChatID: group, UserID: userID}
}

// newTestStore connects to the database named by TEST_db_POSTGRES_URL and
// rebuilds the schema from scratch. It DROPS the bot's tables, so point it
// only at a disposable database. Without the variable the test is skipped.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_db_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_db_POSTGRES_URL is not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(store.Close)

	if _, err := store.pool.Exec(ctx, `DROP TABLE IF EXISTS messages, conversations, users, chats, schema_migrations CASCADE`); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	if _, err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func turn(question, answer string) []ai.Message {
	return []ai.Message{
		{Role: ai.RoleUser, Content: question},
		{Role: ai.RoleAssistant, Content: answer},
	}
}

func mustSave(t *testing.T, store *Store, userID, updateID int64, msgs []ai.Message) {
	t.Helper()
	if _, err := store.SaveTurn(context.Background(), Turn{Scope: private(userID), UpdateID: updateID, Messages: msgs}); err != nil {
		t.Fatalf("SaveTurn: %v", err)
	}
}

func mustHistory(t *testing.T, store *Store, userID int64, limit int) []ai.Message {
	t.Helper()
	history, err := store.History(context.Background(), private(userID), limit)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	return history
}

func countRows(t *testing.T, store *Store, table string) int {
	t.Helper()
	var count int
	// table is a constant from the test, never user input.
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestMigrateIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	applied, err := store.Migrate(ctx)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if applied != 0 {
		t.Errorf("second Migrate applied %d migrations, want 0", applied)
	}
	if err := store.CheckSchema(ctx); err != nil {
		t.Errorf("CheckSchema: %v", err)
	}
}

func TestMigrateReportsAppliedCount(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `DROP TABLE messages, conversations, users, chats, schema_migrations CASCADE`); err != nil {
		t.Fatal(err)
	}

	if err := store.CheckSchema(ctx); !errors.Is(err, ErrSchemaOutdated) {
		t.Errorf("CheckSchema on empty database = %v, want ErrSchemaOutdated", err)
	}
	applied, err := store.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if applied < 1 {
		t.Errorf("applied = %d, want at least 1", applied)
	}
}

func TestSaveTurnAndHistoryRoundTrip(t *testing.T) {
	store := newTestStore(t)
	saved := []ai.Message{
		{Role: ai.RoleUser, Content: "what is 2+2?"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{
			ID:       "call_1",
			Type:     "function",
			Function: ai.FunctionCall{Name: "calculator", Arguments: `{"expression":"2+2"}`},
		}}},
		{Role: ai.RoleTool, ToolCallID: "call_1", Content: `{"ok":true,"result":4}`},
		{Role: ai.RoleAssistant, Content: "4"},
	}
	mustSave(t, store, alice, 1, saved)

	if got := mustHistory(t, store, alice, 20); !reflect.DeepEqual(got, saved) {
		t.Errorf("history = %+v\nwant      %+v", got, saved)
	}
}

func TestHistoryReturnsNewestMessagesOldestFirst(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, alice, 1, turn("q1", "a1"))
	mustSave(t, store, alice, 2, turn("q2", "a2"))
	mustSave(t, store, alice, 3, turn("q3", "a3"))

	got := mustHistory(t, store, alice, 3)
	want := []string{"a2", "q3", "a3"}
	if len(got) != len(want) {
		t.Fatalf("history = %+v", got)
	}
	for i, content := range want {
		if got[i].Content != content {
			t.Errorf("history[%d] = %q, want %q", i, got[i].Content, content)
		}
	}
}

func TestHistoryOfUnknownUserIsEmpty(t *testing.T) {
	store := newTestStore(t)

	if got := mustHistory(t, store, alice, 20); len(got) != 0 {
		t.Errorf("history = %+v, want empty", got)
	}
}

func TestUsersNeverSeeEachOthersMessages(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, alice, 1, turn("alice secret", "alice answer"))
	mustSave(t, store, bob, 2, turn("bob secret", "bob answer"))

	for _, msg := range mustHistory(t, store, alice, 20) {
		if msg.Content == "bob secret" || msg.Content == "bob answer" {
			t.Errorf("alice sees bob's message %q", msg.Content)
		}
	}
	if got := mustHistory(t, store, bob, 20); len(got) != 2 || got[0].Content != "bob secret" {
		t.Errorf("bob history = %+v", got)
	}
}

func TestResetRemovesOnlyTheCallersHistory(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, alice, 1, turn("q", "a"))
	mustSave(t, store, bob, 2, turn("q", "a"))

	if err := store.Reset(context.Background(), private(alice)); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got := mustHistory(t, store, alice, 20); len(got) != 0 {
		t.Errorf("alice history after reset = %+v", got)
	}
	if got := mustHistory(t, store, bob, 20); len(got) != 2 {
		t.Errorf("bob history after alice's reset = %+v", got)
	}
	if rows := countRows(t, store, "messages"); rows != 2 {
		t.Errorf("messages rows = %d, want 2", rows)
	}

	mustSave(t, store, alice, 3, turn("again", "sure"))
	if got := mustHistory(t, store, alice, 20); len(got) != 2 {
		t.Errorf("alice history after new turn = %+v", got)
	}
}

func TestResetOfUnknownUserIsNoOp(t *testing.T) {
	store := newTestStore(t)

	if err := store.Reset(context.Background(), private(999)); err != nil {
		t.Errorf("Reset: %v", err)
	}
}

func TestSaveTurnRejectsDuplicateUpdate(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, alice, 7, turn("q", "a"))

	_, err := store.SaveTurn(context.Background(), Turn{Scope: private(alice), UpdateID: 7, Messages: turn("q", "a")})
	if !errors.Is(err, ErrDuplicateUpdate) {
		t.Fatalf("err = %v, want ErrDuplicateUpdate", err)
	}
	if rows := countRows(t, store, "messages"); rows != 2 {
		t.Errorf("messages rows = %d, want 2 (the failed turn must roll back)", rows)
	}
}

func TestUpdateProcessed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mustSave(t, store, alice, 7, turn("q", "a"))

	for updateID, want := range map[int64]bool{7: true, 8: false} {
		got, err := store.UpdateProcessed(ctx, updateID)
		if err != nil {
			t.Fatalf("UpdateProcessed: %v", err)
		}
		if got != want {
			t.Errorf("UpdateProcessed(%d) = %v, want %v", updateID, got, want)
		}
	}
}

func TestSaveTurnRejectsEmptyTurn(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.SaveTurn(context.Background(), Turn{Scope: private(alice), UpdateID: 1}); err == nil {
		t.Error("empty turn accepted")
	}
}

func TestDeleteIdleRemovesOnlyOldConversations(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mustSave(t, store, alice, 1, turn("old", "old"))
	mustSave(t, store, bob, 2, turn("fresh", "fresh"))
	if _, err := store.pool.Exec(ctx,
		`UPDATE conversations SET updated_at = now() - interval '40 days' WHERE user_id = $1`, alice); err != nil {
		t.Fatal(err)
	}

	deleted, err := store.DeleteIdle(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("DeleteIdle: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if got := mustHistory(t, store, alice, 20); len(got) != 0 {
		t.Errorf("alice history = %+v, want empty", got)
	}
	if got := mustHistory(t, store, bob, 20); len(got) != 2 {
		t.Errorf("bob history = %+v", got)
	}
}

func TestSchemaConstraintsRejectInvalidMessages(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mustSave(t, store, alice, 1, turn("q", "a"))

	invalid := map[string]string{
		"unknown role":             `INSERT INTO messages (conversation_id, role, content) SELECT id, 'system', 'x' FROM conversations`,
		"tool without call id":     `INSERT INTO messages (conversation_id, role, content) SELECT id, 'tool', 'x' FROM conversations`,
		"user with call id":        `INSERT INTO messages (conversation_id, role, content, tool_call_id) SELECT id, 'user', 'x', 'c' FROM conversations`,
		"user with tool calls":     `INSERT INTO messages (conversation_id, role, content, tool_calls) SELECT id, 'user', 'x', '[]' FROM conversations`,
		"message without a parent": `INSERT INTO messages (conversation_id, role, content) VALUES (987654, 'user', 'x')`,
	}
	for name, statement := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := store.pool.Exec(ctx, statement); err == nil {
				t.Error("invalid row accepted")
			}
		})
	}
}

func saveIn(t *testing.T, store *Store, scope Scope, updateID int64, msgs []ai.Message) int64 {
	t.Helper()
	answerID, err := store.SaveTurn(context.Background(), Turn{Scope: scope, UpdateID: updateID, Messages: msgs})
	if err != nil {
		t.Fatalf("SaveTurn: %v", err)
	}
	return answerID
}

func historyIn(t *testing.T, store *Store, scope Scope) []ai.Message {
	t.Helper()
	history, err := store.History(context.Background(), scope, 20)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	return history
}

func TestPrivateAndGroupHistoriesAreSeparate(t *testing.T) {
	store := newTestStore(t)
	saveIn(t, store, private(alice), 1, turn("private question", "private answer"))
	saveIn(t, store, inGroup(alice), 2, turn("group question", "group answer"))

	privateHistory := historyIn(t, store, private(alice))
	groupHistory := historyIn(t, store, inGroup(alice))
	if len(privateHistory) != 2 || privateHistory[0].Content != "private question" {
		t.Errorf("private history = %+v", privateHistory)
	}
	if len(groupHistory) != 2 || groupHistory[0].Content != "group question" {
		t.Errorf("group history = %+v", groupHistory)
	}
}

func TestGroupMembersHaveSeparateHistories(t *testing.T) {
	store := newTestStore(t)
	saveIn(t, store, inGroup(alice), 1, turn("alice asks", "to alice"))
	saveIn(t, store, inGroup(bob), 2, turn("bob asks", "to bob"))

	if got := historyIn(t, store, inGroup(alice)); len(got) != 2 || got[0].Content != "alice asks" {
		t.Errorf("alice's group history = %+v", got)
	}
	if got := historyIn(t, store, inGroup(bob)); len(got) != 2 || got[0].Content != "bob asks" {
		t.Errorf("bob's group history = %+v", got)
	}
}

func TestResetClearsOneScopeOnly(t *testing.T) {
	store := newTestStore(t)
	saveIn(t, store, private(alice), 1, turn("q", "a"))
	saveIn(t, store, inGroup(alice), 2, turn("q", "a"))
	saveIn(t, store, inGroup(bob), 3, turn("q", "a"))

	if err := store.Reset(context.Background(), inGroup(alice)); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got := historyIn(t, store, inGroup(alice)); len(got) != 0 {
		t.Errorf("alice's group history after reset = %+v", got)
	}
	if got := historyIn(t, store, private(alice)); len(got) != 2 {
		t.Errorf("alice's private history after a group reset = %+v", got)
	}
	if got := historyIn(t, store, inGroup(bob)); len(got) != 2 {
		t.Errorf("bob's group history after alice's reset = %+v", got)
	}
}

func TestDuplicateUpdateIsRejectedAcrossScopes(t *testing.T) {
	store := newTestStore(t)
	saveIn(t, store, private(alice), 9, turn("q", "a"))

	_, err := store.SaveTurn(context.Background(), Turn{Scope: inGroup(alice), UpdateID: 9, Messages: turn("q", "a")})
	if !errors.Is(err, ErrDuplicateUpdate) {
		t.Fatalf("err = %v, want ErrDuplicateUpdate", err)
	}
}

type storedIDs struct {
	role             string
	messageID, reply *int64
}

func readIDs(t *testing.T, store *Store) []storedIDs {
	t.Helper()
	rows, err := store.pool.Query(context.Background(),
		`SELECT role, telegram_message_id, reply_to_message_id FROM messages ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storedIDs
	for rows.Next() {
		var row storedIDs
		if err := rows.Scan(&row.role, &row.messageID, &row.reply); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	return out
}

func equalID(got *int64, want int64) bool {
	if want == 0 {
		return got == nil
	}
	return got != nil && *got == want
}

func TestSaveTurnStoresMessageAndReplyIDs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	answerID, err := store.SaveTurn(ctx, Turn{
		Scope:            inGroup(alice),
		UpdateID:         1,
		MessageID:        30,
		ReplyToMessageID: 29,
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "q"},
			{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c", Type: "function", Function: ai.FunctionCall{Name: "calculator"}}}},
			{Role: ai.RoleTool, ToolCallID: "c", Content: "{}"},
			{Role: ai.RoleAssistant, Content: "a"},
		},
	})
	if err != nil {
		t.Fatalf("SaveTurn: %v", err)
	}

	rows := readIDs(t, store)
	if len(rows) != 4 {
		t.Fatalf("rows = %+v", rows)
	}
	if !equalID(rows[0].messageID, 30) || !equalID(rows[0].reply, 29) {
		t.Errorf("user row: message %v, reply %v; want 30 and 29", rows[0].messageID, rows[0].reply)
	}
	for _, middle := range rows[1:3] {
		if middle.messageID != nil || middle.reply != nil {
			t.Errorf("%s row carries Telegram IDs", middle.role)
		}
	}
	if !equalID(rows[3].messageID, 0) || !equalID(rows[3].reply, 30) {
		t.Errorf("answer row: message %v, reply %v; want unset and 30", rows[3].messageID, rows[3].reply)
	}

	if err := store.SetSentMessageID(ctx, answerID, 501); err != nil {
		t.Fatalf("SetSentMessageID: %v", err)
	}
	if rows := readIDs(t, store); !equalID(rows[3].messageID, 501) || rows[0].messageID == nil || *rows[0].messageID != 30 {
		t.Errorf("after SetSentMessageID: rows = %+v", rows)
	}
}

func TestSaveTurnStoresNoIDsWhenNoneGiven(t *testing.T) {
	store := newTestStore(t)
	saveIn(t, store, private(alice), 1, turn("q", "a"))

	for _, row := range readIDs(t, store) {
		if row.messageID != nil || row.reply != nil {
			t.Errorf("%s row has IDs although none were given: %+v", row.role, row)
		}
	}
}

// Conversations saved by the first schema version had no chat ID. Migration
// 0002 must keep them reachable as private conversations.
func TestMigrationKeepsExistingPrivateConversations(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `DROP TABLE messages, conversations, users, chats, schema_migrations CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.migrateTo(ctx, 1); err != nil {
		t.Fatalf("migrate to version 1: %v", err)
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO users (telegram_id) VALUES (1001);
		INSERT INTO conversations (user_id) VALUES (1001);
		INSERT INTO messages (conversation_id, role, content, update_id)
			SELECT id, 'user', 'from before the migration', 5 FROM conversations;
		INSERT INTO messages (conversation_id, role, content)
			SELECT id, 'assistant', 'old answer' FROM conversations;`)
	if err != nil {
		t.Fatalf("seed version 1 data: %v", err)
	}

	applied, err := store.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if applied < 1 {
		t.Errorf("applied = %d, want at least 1", applied)
	}
	got := historyIn(t, store, private(alice))
	if len(got) != 2 || got[0].Content != "from before the migration" || got[1].Content != "old answer" {
		t.Errorf("history after migration = %+v", got)
	}
	if got := historyIn(t, store, inGroup(alice)); len(got) != 0 {
		t.Errorf("group history after migration = %+v, want empty", got)
	}
	saveIn(t, store, private(alice), 6, turn("after", "ok"))
	if got := historyIn(t, store, private(alice)); len(got) != 4 {
		t.Errorf("history after a new turn = %+v", got)
	}
}

// shared is a group's single conversation with the bot.
func shared(chatID int64) Scope {
	return Scope{ChatID: chatID}
}

const otherGroup = int64(-1009999999999)

func saveBy(t *testing.T, store *Store, scope Scope, updateID, authorID int64, name string, msgs []ai.Message) {
	t.Helper()
	_, err := store.SaveTurn(context.Background(), Turn{
		Scope: scope, UpdateID: updateID, AuthorID: authorID, AuthorName: name, Messages: msgs,
	})
	if err != nil {
		t.Fatalf("SaveTurn: %v", err)
	}
}

func TestSharedGroupConversationCollectsAllMembers(t *testing.T) {
	store := newTestStore(t)
	saveBy(t, store, shared(group), 1, alice, "Alice", turn("Alice: roast me", "no material needed"))
	saveBy(t, store, shared(group), 2, bob, "Bob", turn("Bob: me too", "you first"))

	got := historyIn(t, store, shared(group))
	want := []string{"Alice: roast me", "no material needed", "Bob: me too", "you first"}
	if len(got) != len(want) {
		t.Fatalf("shared history = %+v", got)
	}
	for i, content := range want {
		if got[i].Content != content {
			t.Errorf("history[%d] = %q, want %q", i, got[i].Content, content)
		}
	}
	if rows := countRows(t, store, "conversations"); rows != 1 {
		t.Errorf("conversations rows = %d, want one shared conversation", rows)
	}
}

func TestSharedGroupScopeIsSeparateFromEverythingElse(t *testing.T) {
	store := newTestStore(t)
	saveBy(t, store, shared(group), 1, alice, "Alice", turn("in the group", "group answer"))
	saveBy(t, store, shared(otherGroup), 2, alice, "Alice", turn("in another group", "other answer"))
	saveIn(t, store, private(alice), 3, turn("in private", "private answer"))

	for name, tc := range map[string]struct {
		scope Scope
		want  string
	}{
		"group":         {shared(group), "in the group"},
		"another group": {shared(otherGroup), "in another group"},
		"private":       {private(alice), "in private"},
	} {
		got := historyIn(t, store, tc.scope)
		if len(got) != 2 || got[0].Content != tc.want {
			t.Errorf("%s history = %+v", name, got)
		}
	}
}

func TestResetOfSharedScopeLeavesOtherScopes(t *testing.T) {
	store := newTestStore(t)
	saveBy(t, store, shared(group), 1, alice, "Alice", turn("q", "a"))
	saveBy(t, store, shared(otherGroup), 2, alice, "Alice", turn("q", "a"))
	saveIn(t, store, private(alice), 3, turn("q", "a"))

	if err := store.Reset(context.Background(), shared(group)); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got := historyIn(t, store, shared(group)); len(got) != 0 {
		t.Errorf("group history after reset = %+v", got)
	}
	if len(historyIn(t, store, shared(otherGroup))) != 2 || len(historyIn(t, store, private(alice))) != 2 {
		t.Error("resetting one group removed another scope's history")
	}
}

func TestAuthorIsStoredOnTheUserMessage(t *testing.T) {
	store := newTestStore(t)
	saveBy(t, store, shared(group), 1, bob, "Bob", turn("Bob: hi", "hello"))

	rows, err := store.pool.Query(context.Background(), `SELECT role, author_id, author_name FROM messages ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type authored struct {
		role string
		id   *int64
		name *string
	}
	var got []authored
	for rows.Next() {
		var row authored
		if err := rows.Scan(&row.role, &row.id, &row.name); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if len(got) != 2 || got[0].id == nil || *got[0].id != bob || got[0].name == nil || *got[0].name != "Bob" {
		t.Fatalf("user row author = %+v", got)
	}
	if got[1].id != nil || got[1].name != nil {
		t.Errorf("the assistant row carries an author: %+v", got[1])
	}
}

func TestMuteStateIsPersisted(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	muted := func() []int64 {
		t.Helper()
		chats, err := store.MutedChats(ctx)
		if err != nil {
			t.Fatalf("MutedChats: %v", err)
		}
		return chats
	}

	if got := muted(); len(got) != 0 {
		t.Fatalf("muted chats in a new database = %v", got)
	}
	for range 2 { // muting twice is not an error
		if err := store.SetMuted(ctx, group, true, alice); err != nil {
			t.Fatalf("SetMuted: %v", err)
		}
	}
	if err := store.SetMuted(ctx, otherGroup, false, alice); err != nil {
		t.Fatalf("SetMuted: %v", err)
	}
	if got := muted(); len(got) != 1 || got[0] != group {
		t.Errorf("muted chats = %v, want [%d]", got, group)
	}

	if err := store.SetMuted(ctx, group, false, bob); err != nil {
		t.Fatalf("SetMuted: %v", err)
	}
	if got := muted(); len(got) != 0 {
		t.Errorf("muted chats after unmuting = %v", got)
	}
}

// A database at version 2 has private conversations and per-member group
// conversations. Migration 0003 must keep the private ones readable and
// start each group with an empty shared conversation.
func TestMigrationToSharedGroups(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `DROP TABLE messages, conversations, users, chats, schema_migrations CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.migrateTo(ctx, 2); err != nil {
		t.Fatalf("migrate to version 2: %v", err)
	}
	// Seeded with SQL: the current code writes columns version 2 lacks.
	_, err := store.pool.Exec(ctx, `
		INSERT INTO users (telegram_id) VALUES (1001);
		INSERT INTO conversations (chat_id, user_id) VALUES (1001, 1001), (-1001234567890, 1001);
		INSERT INTO messages (conversation_id, role, content)
			SELECT id, 'user', CASE WHEN chat_id = user_id THEN 'private before' ELSE 'per-member before' END FROM conversations;
		INSERT INTO messages (conversation_id, role, content)
			SELECT id, 'assistant', 'ok' FROM conversations;`)
	if err != nil {
		t.Fatalf("seed version 2 data: %v", err)
	}

	if _, err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := historyIn(t, store, private(alice)); len(got) != 2 || got[0].Content != "private before" {
		t.Errorf("private history after migration = %+v", got)
	}
	if got := historyIn(t, store, shared(group)); len(got) != 0 {
		t.Errorf("shared group history after migration = %+v, want empty", got)
	}
	saveBy(t, store, shared(group), 3, alice, "Alice", turn("Alice: after", "ok"))
	if got := historyIn(t, store, shared(group)); len(got) != 2 {
		t.Errorf("shared group history after a new turn = %+v", got)
	}
}

func TestOpenErrorDoesNotLeakPassword(t *testing.T) {
	_, err := Open(context.Background(), "postgres://bot:hunter2secret@127.0.0.1:1/db?connect_timeout=1")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); containsSecret(got) {
		t.Errorf("error leaks password: %v", got)
	}

	_, err = Open(context.Background(), "postgres://bot:hunter2secret@%zz/db")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); containsSecret(got) {
		t.Errorf("error leaks password: %v", got)
	}
}

func containsSecret(text string) bool {
	return strings.Contains(text, "hunter2secret")
}
