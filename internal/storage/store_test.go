package storage

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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

// newTestStore connects to the database named by TEST_DB_POSTGRES_URL and
// rebuilds the schema from scratch. It DROPS the bot's tables, so point it
// only at a disposable database. Without the variable the test is skipped.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DB_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_DB_POSTGRES_URL is not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(store.Close)

	if _, err := store.pool.Exec(ctx, `DROP TABLE IF EXISTS messages, conversations, users, chats, group_messages, image_generations, bot_replies, groups, broadcasts, group_replies, group_rules, moderation_actions, pending_bans, pending_moderation, private_messages, group_members, owner_notices, user_profiles, bot_stickers, sticker_sends, hero_games, hero_scores, presentations, schema_migrations CASCADE`); err != nil {
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
	if _, err := store.pool.Exec(ctx, `DROP TABLE messages, conversations, users, chats, group_messages, image_generations, bot_replies, groups, broadcasts, group_replies, group_rules, moderation_actions, private_messages, group_members, owner_notices, user_profiles, bot_stickers, sticker_sends, hero_games, hero_scores, presentations, schema_migrations CASCADE`); err != nil {
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

// age makes every message of a user's private conversation look older.
func age(t *testing.T, store *Store, userID int64, by string) {
	t.Helper()
	_, err := store.pool.Exec(context.Background(), `
		UPDATE messages SET created_at = now() - $2::interval
		WHERE conversation_id IN (SELECT id FROM conversations WHERE user_id = $1)`, userID, by)
	if err != nil {
		t.Fatal(err)
	}
}

func TestExpireRemovesMessagesOlderThanTheRetention(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mustSave(t, store, alice, 1, turn("yesterday's question", "yesterday's answer"))
	age(t, store, alice, "25 hours")
	mustSave(t, store, alice, 2, turn("today's question", "today's answer"))
	mustSave(t, store, bob, 3, turn("bob today", "ok"))

	expired, err := store.Expire(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if expired.Messages != 2 || expired.Conversations != 0 {
		t.Errorf("expired = %+v, want 2 messages and no conversations", expired)
	}
	got := mustHistory(t, store, alice, 20)
	if len(got) != 2 || got[0].Content != "today's question" {
		t.Errorf("alice history = %+v, want only today's turn", got)
	}
	if got := mustHistory(t, store, bob, 20); len(got) != 2 {
		t.Errorf("bob history = %+v", got)
	}
}

func TestExpireRemovesConversationsLeftEmpty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mustSave(t, store, alice, 1, turn("old", "old"))
	age(t, store, alice, "2 days")
	if err := store.SetMuted(ctx, group, true, alice); err != nil {
		t.Fatal(err)
	}

	expired, err := store.Expire(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if expired.Messages != 2 || expired.Conversations != 1 {
		t.Errorf("expired = %+v, want 2 messages and 1 conversation", expired)
	}
	if rows := countRows(t, store, "conversations"); rows != 0 {
		t.Errorf("conversations left = %d", rows)
	}
	if muted, _ := store.MutedChats(ctx); len(muted) != 1 {
		t.Errorf("muted chats = %v; settings are not chat history and must survive", muted)
	}
}

func TestExpireNeverSplitsATurn(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	saveIn(t, store, private(alice), 1, []ai.Message{
		{Role: ai.RoleUser, Content: "2+2?"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c", Type: "function", Function: ai.FunctionCall{Name: "calculator"}}}},
		{Role: ai.RoleTool, ToolCallID: "c", Content: "4"},
		{Role: ai.RoleAssistant, Content: "4"},
	})

	// All rows of a turn are written in one transaction and share its
	// timestamp, so a cutoff can never fall between them.
	var distinct int
	if err := store.pool.QueryRow(ctx, `SELECT count(DISTINCT created_at) FROM messages`).Scan(&distinct); err != nil {
		t.Fatal(err)
	}
	if distinct != 1 {
		t.Errorf("one turn has %d different timestamps", distinct)
	}
}

func TestExpireOnAnEmptyDatabase(t *testing.T) {
	store := newTestStore(t)

	expired, err := store.Expire(context.Background(), 24*time.Hour)
	if err != nil || expired.Messages != 0 || expired.Conversations != 0 {
		t.Errorf("expired = %+v, err = %v", expired, err)
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

func TestAnsweredAuthorIsWhomTheBotsMessageAnswered(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	answer := func(scope Scope, updateID, messageID, authorID int64, name string, sentID int64) {
		t.Helper()
		answerID, err := store.SaveTurn(ctx, Turn{Scope: scope, UpdateID: updateID, MessageID: messageID, AuthorID: authorID, AuthorName: name, Messages: turn(name+": hi", "hello")})
		if err != nil {
			t.Fatalf("SaveTurn: %v", err)
		}
		if err := store.SetSentMessageID(ctx, answerID, sentID); err != nil {
			t.Fatalf("SetSentMessageID: %v", err)
		}
	}
	answer(shared(group), 1, 30, bob, "Bob @bob", 31)
	answer(shared(group), 2, 32, alice, "Alice [owner]", 33)
	// Telegram numbers messages per chat: another chat's 31 is another message.
	answer(shared(otherGroup), 3, 30, alice, "Alice", 31)
	answer(private(bob), 4, 34, 0, "", 35)

	got, err := store.AnsweredAuthor(ctx, group, 31)
	if want := (Author{ID: bob, Name: "Bob @bob", MessageID: 30}); err != nil || got != want {
		t.Errorf("AnsweredAuthor(group, 31) = %+v, %v; want %+v", got, err, want)
	}
	if got, err := store.AnsweredAuthor(ctx, group, 33); err != nil || got.ID != alice || got.Name != "Alice [owner]" {
		t.Errorf("AnsweredAuthor(group, 33) = %+v, %v; want Alice as stored", got, err)
	}
	for name, id := range map[string]int64{"a member's message": 30, "an unknown message": 99, "a private chat's": 35} {
		if _, err := store.AnsweredAuthor(ctx, group, id); !errors.Is(err, ErrUnknownAuthor) {
			t.Errorf("%s: err = %v, want ErrUnknownAuthor", name, err)
		}
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

func TestIsMutedReadsTheStoredSetting(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	isMuted := func(chatID int64) bool {
		t.Helper()
		muted, err := store.IsMuted(ctx, chatID)
		if err != nil {
			t.Fatalf("IsMuted: %v", err)
		}
		return muted
	}

	if isMuted(group) {
		t.Error("a chat with no setting is reported as muted")
	}
	if err := store.SetMuted(ctx, group, true, alice); err != nil {
		t.Fatal(err)
	}
	if !isMuted(group) || isMuted(otherGroup) {
		t.Error("muting one chat must affect that chat only")
	}
	if err := store.SetMuted(ctx, group, false, alice); err != nil {
		t.Fatal(err)
	}
	if isMuted(group) {
		t.Error("the chat is still reported as muted after unmuting")
	}
}

func logMessage(t *testing.T, store *Store, chatID int64, name, text string) {
	t.Helper()
	err := store.AppendGroupMessage(context.Background(), GroupMessage{ChatID: chatID, AuthorID: alice, AuthorName: name, Text: text})
	if err != nil {
		t.Fatalf("AppendGroupMessage: %v", err)
	}
}

func TestGroupMessagesAreReadBackOldestFirst(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, text := range []string{"one", "two", "three"} {
		logMessage(t, store, group, "Ann", text)
	}
	logMessage(t, store, otherGroup, "Bek", "elsewhere")

	got, err := store.GroupMessagesSince(ctx, group, time.Now().Add(-time.Hour), 2)
	if err != nil {
		t.Fatalf("GroupMessagesSince: %v", err)
	}
	if len(got) != 2 || got[0].Text != "two" || got[1].Text != "three" || got[0].AuthorName != "Ann" {
		t.Errorf("messages = %+v, want the newest two, oldest first", got)
	}
	if got[0].SentAt.IsZero() {
		t.Error("SentAt is not filled in")
	}
}

// A tagged @username is the member last seen writing under it in that
// group; a name that merely contains it, another group, or a username that
// is not one are nobody.
func TestAuthorByUsername(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, m := range []GroupMessage{
		{ChatID: group, MessageID: 1, AuthorID: alice, AuthorName: "Ann @said_05", Text: "salom"},
		{ChatID: group, MessageID: 2, AuthorID: bob, AuthorName: "Bek @said_05x [owner]", Text: "hi"},
		{ChatID: group, MessageID: 3, AuthorID: 4242, AuthorName: "New @Said_05", Text: "username changed hands"},
		{ChatID: otherGroup, MessageID: 4, AuthorID: 5151, AuthorName: "Elsewhere @only_there", Text: "x"},
	} {
		if err := store.AppendGroupMessage(ctx, m); err != nil {
			t.Fatalf("AppendGroupMessage: %v", err)
		}
	}
	// The member list outlasts the day's log: a member seen there is found
	// by the username they have now.
	if err := store.SeeMember(ctx, GroupMember{ChatID: group, UserID: 6006, FirstName: "Listed", Username: "Listed_Only"}); err != nil {
		t.Fatal(err)
	}
	cases := map[string]int64{"said_05": 4242, "SAID_05": 4242, "said_05x": bob, "listed_only": 6006}
	for username, want := range cases {
		if got, err := store.AuthorByUsername(ctx, group, username); err != nil || got != want {
			t.Errorf("AuthorByUsername(%q) = %d, %v; want %d", username, got, err, want)
		}
	}
	for _, username := range []string{"said", "only_there", "", "x.*", "a' OR '1'='1"} {
		if got, err := store.AuthorByUsername(ctx, group, username); !errors.Is(err, ErrUnknownAuthor) {
			t.Errorf("AuthorByUsername(%q) = %d, %v; want ErrUnknownAuthor", username, got, err)
		}
	}
}

// A redelivered update must not log its message twice; the bot's own lines,
// which have no message ID, are all kept.
func TestGroupMessageIsLoggedOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, m := range []GroupMessage{
		{ChatID: group, MessageID: 30, AuthorID: alice, AuthorName: "Ann", Text: "xaxa"},
		{ChatID: group, MessageID: 30, AuthorID: alice, AuthorName: "Ann", Text: "xaxa"},
		{ChatID: otherGroup, MessageID: 30, AuthorID: bob, AuthorName: "Bek", Text: "same ID, another chat"},
		{ChatID: group, AuthorID: 777, AuthorName: "You", Text: "ha"},
		{ChatID: group, AuthorID: 777, AuthorName: "You", Text: "ha"},
	} {
		if err := store.AppendGroupMessage(ctx, m); err != nil {
			t.Fatalf("AppendGroupMessage: %v", err)
		}
	}

	got, err := store.GroupMessagesSince(ctx, group, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("GroupMessagesSince: %v", err)
	}
	var texts []string
	for _, m := range got {
		texts = append(texts, m.AuthorName+": "+m.Text)
	}
	if want := "Ann: xaxa|You: ha|You: ha"; strings.Join(texts, "|") != want {
		t.Errorf("log = %q, want %q", texts, want)
	}
	if other, _ := store.GroupMessagesSince(ctx, otherGroup, time.Now().Add(-time.Hour), 10); len(other) != 1 {
		t.Errorf("another chat's message with the same ID = %+v, want it logged", other)
	}
}

func TestGroupMessagesBeforeTheCutoffAreLeftOut(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	logMessage(t, store, group, "Ann", "yesterday")
	if _, err := store.pool.Exec(ctx, `UPDATE group_messages SET created_at = now() - interval '20 hours'`); err != nil {
		t.Fatal(err)
	}
	logMessage(t, store, group, "Ann", "today")

	got, err := store.GroupMessagesSince(ctx, group, time.Now().Add(-time.Hour), 100)
	if err != nil || len(got) != 1 || got[0].Text != "today" {
		t.Errorf("messages = %+v, err = %v", got, err)
	}
}

func TestClearGroupMessagesClearsOneChat(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	logMessage(t, store, group, "Ann", "a")
	logMessage(t, store, otherGroup, "Bek", "b")

	if err := store.ClearGroupMessages(ctx, group); err != nil {
		t.Fatalf("ClearGroupMessages: %v", err)
	}
	if got, _ := store.GroupMessagesSince(ctx, group, time.Now().Add(-time.Hour), 10); len(got) != 0 {
		t.Errorf("cleared chat still has %+v", got)
	}
	if got, _ := store.GroupMessagesSince(ctx, otherGroup, time.Now().Add(-time.Hour), 10); len(got) != 1 {
		t.Errorf("another chat lost its messages: %+v", got)
	}
}

func TestExpireAlsoRemovesOldGroupMessages(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	logMessage(t, store, group, "Ann", "old")
	if _, err := store.pool.Exec(ctx, `UPDATE group_messages SET created_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	logMessage(t, store, group, "Ann", "new")

	expired, err := store.Expire(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if expired.GroupMessages != 1 {
		t.Errorf("expired = %+v, want 1 group message", expired)
	}
	if rows := countRows(t, store, "group_messages"); rows != 1 {
		t.Errorf("group_messages rows = %d, want 1", rows)
	}
}

func TestImageReservationsAreCountedPerUserPerDay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	today := time.Now().Add(-time.Hour)

	for i := range 2 {
		id, ok, err := store.ReserveImage(ctx, alice, group, today, 2)
		if err != nil || !ok || id == 0 {
			t.Fatalf("reservation %d: id = %d, ok = %v, err = %v", i+1, id, ok, err)
		}
	}
	if _, ok, err := store.ReserveImage(ctx, alice, group, today, 2); err != nil || ok {
		t.Errorf("third reservation: ok = %v, err = %v; the daily limit is 2", ok, err)
	}
	if _, ok, _ := store.ReserveImage(ctx, bob, group, today, 2); !ok {
		t.Error("another user was refused because of alice's count")
	}
	if _, ok, _ := store.ReserveImage(ctx, alice, group, time.Now().Add(time.Hour), 2); !ok {
		t.Error("yesterday's images counted against a new day")
	}
}

func TestReleasedReservationDoesNotCount(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)
	id, _, _ := store.ReserveImage(ctx, alice, group, since, 1)

	if err := store.ReleaseImage(ctx, id); err != nil {
		t.Fatalf("ReleaseImage: %v", err)
	}
	if _, ok, _ := store.ReserveImage(ctx, alice, group, since, 1); !ok {
		t.Error("a failed generation still used up the day's only image")
	}
}

func TestConcurrentReservationsRespectTheLimit(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)

	var granted atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, ok, err := store.ReserveImage(ctx, alice, group, since, 2); err == nil && ok {
				granted.Add(1)
			}
		})
	}
	wg.Wait()
	if n := granted.Load(); n != 2 {
		t.Errorf("granted = %d of 8 simultaneous requests, want exactly 2", n)
	}
}

func TestBotRepliesAreLimitedPerChatInAWindow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	window := time.Now().Add(-10 * time.Minute)

	for i := range 3 {
		if ok, err := store.ReserveBotReply(ctx, group, 555, window, 3); err != nil || !ok {
			t.Fatalf("reply %d: ok = %v, err = %v", i+1, ok, err)
		}
	}
	if ok, _ := store.ReserveBotReply(ctx, group, 666, window, 3); ok {
		t.Error("a fourth reply to bots in the window was allowed, from another bot")
	}
	if ok, _ := store.ReserveBotReply(ctx, otherGroup, 555, window, 3); !ok {
		t.Error("another chat was refused because of this chat's count")
	}
	if ok, _ := store.ReserveBotReply(ctx, group, 555, time.Now().Add(time.Minute), 3); !ok {
		t.Error("replies before the window still counted")
	}
}

// ageGroupReplies moves every recorded group answer minutes into the past.
func ageGroupReplies(t *testing.T, store *Store, minutes int) {
	t.Helper()
	_, err := store.pool.Exec(context.Background(), `UPDATE group_replies SET created_at = created_at - make_interval(mins => $1)`, minutes)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReserveGroupReplyLimitsChatAndMember(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const carol = int64(1003)
	reserve := func(chatID, userID int64) bool {
		t.Helper()
		id, ok, err := store.ReserveGroupReply(ctx, ReplyQuota{ChatID: chatID, UserID: userID, PerMinute: 3, PerMember: 2})
		if err != nil {
			t.Fatalf("ReserveGroupReply: %v", err)
		}
		if ok != (id != 0) {
			t.Fatalf("ReserveGroupReply = %d, %t; want an ID exactly when granted", id, ok)
		}
		return ok
	}

	got := []bool{reserve(group, alice), reserve(group, alice), reserve(group, alice), reserve(group, bob), reserve(group, carol)}
	if want := []bool{true, true, false, true, false}; !reflect.DeepEqual(got, want) {
		t.Errorf("granted = %v, want %v: alice may have 2 answers, the chat 3 a minute", got, want)
	}
	if !reserve(otherGroup, carol) {
		t.Error("another chat was refused because of this chat's count")
	}

	ageGroupReplies(t, store, 2)
	if !reserve(group, carol) {
		t.Error("the chat was still full a minute later")
	}
	if reserve(group, alice) {
		t.Error("alice was answered again within her 10 minutes")
	}
	ageGroupReplies(t, store, 9)
	if !reserve(group, alice) {
		t.Error("alice was refused after her 10 minutes were over")
	}
}

// A turn that answers nothing, such as one Telegram delivered again, gives
// its answer back.
func TestReleasedGroupReplyDoesNotCount(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	quota := ReplyQuota{ChatID: group, UserID: alice, PerMinute: 3, PerMember: 1}

	id, ok, err := store.ReserveGroupReply(ctx, quota)
	if err != nil || !ok {
		t.Fatalf("ReserveGroupReply = %t, %v", ok, err)
	}
	if _, ok, _ := store.ReserveGroupReply(ctx, quota); ok {
		t.Fatal("alice got a second answer")
	}
	if err := store.ReleaseGroupReply(ctx, id); err != nil {
		t.Fatalf("ReleaseGroupReply: %v", err)
	}
	if _, ok, _ := store.ReserveGroupReply(ctx, quota); !ok {
		t.Error("the answer given back was still counted")
	}
}

func TestExpireAlsoRemovesOldGroupReplies(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	quota := ReplyQuota{ChatID: group, UserID: alice, PerMinute: 5, PerMember: 5}
	if _, _, err := store.ReserveGroupReply(ctx, quota); err != nil {
		t.Fatal(err)
	}
	ageGroupReplies(t, store, 2*24*60)
	if _, _, err := store.ReserveGroupReply(ctx, quota); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Expire(ctx, 24*time.Hour); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if rows := countRows(t, store, "group_replies"); rows != 1 {
		t.Errorf("group_replies rows = %d, want only today's", rows)
	}
}

// privateReservation is what ReservePrivateMessage answered.
type privateReservation struct{ allowed, notify bool }

func reservePrivate(t *testing.T, store *Store, q DailyQuota) (int64, privateReservation) {
	t.Helper()
	id, allowed, notify, err := store.ReservePrivateMessage(context.Background(), q)
	if err != nil {
		t.Fatalf("ReservePrivateMessage: %v", err)
	}
	if allowed != (id != 0) {
		t.Errorf("id = %d with allowed = %v; only an allowed answer has a reservation", id, allowed)
	}
	return id, privateReservation{allowed: allowed, notify: notify}
}

func TestReservePrivateMessageCountsPerDayAndNotifiesOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	today := DailyQuota{UserID: alice, Since: time.Now().Add(-time.Hour), Limit: 2}
	reserveTimes := func(n int) []privateReservation {
		t.Helper()
		var got []privateReservation
		for range n {
			_, r := reservePrivate(t, store, today)
			got = append(got, r)
		}
		return got
	}
	allowed, notice, silence := privateReservation{allowed: true}, privateReservation{notify: true}, privateReservation{}

	if got, want := reserveTimes(4), []privateReservation{allowed, allowed, notice, silence}; !reflect.DeepEqual(got, want) {
		t.Errorf("reservations = %+v, want %+v: two answers, then one notice, then silence", got, want)
	}
	if _, r := reservePrivate(t, store, DailyQuota{UserID: bob, Since: today.Since, Limit: 2}); r != allowed {
		t.Errorf("bob = %+v; another user was refused because of alice's count", r)
	}

	if _, err := store.pool.Exec(ctx, `UPDATE private_messages SET created_at = created_at - interval '1 day' WHERE user_id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if got, want := reserveTimes(3), []privateReservation{allowed, allowed, notice}; !reflect.DeepEqual(got, want) {
		t.Errorf("next day = %+v, want %+v: yesterday's answers and notice do not count", got, want)
	}
}

func TestReleasedPrivateMessageDoesNotCount(t *testing.T) {
	store := newTestStore(t)
	today := DailyQuota{UserID: alice, Since: time.Now().Add(-time.Hour), Limit: 1}
	id, _ := reservePrivate(t, store, today)

	if err := store.ReleasePrivateMessage(context.Background(), id); err != nil {
		t.Fatalf("ReleasePrivateMessage: %v", err)
	}
	if _, r := reservePrivate(t, store, today); !r.allowed {
		t.Error("a failed turn still used up the day's only answer")
	}
	if _, r := reservePrivate(t, store, today); r.allowed || !r.notify {
		t.Errorf("after the limit = %+v, want the notice", r)
	}
}

func TestConcurrentPrivateReservationsNotifyOnce(t *testing.T) {
	store := newTestStore(t)
	today := DailyQuota{UserID: alice, Since: time.Now().Add(-time.Hour), Limit: 2}

	var granted, notified atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, allowed, notify, err := store.ReservePrivateMessage(context.Background(), today)
			if err == nil && allowed {
				granted.Add(1)
			}
			if err == nil && notify {
				notified.Add(1)
			}
		})
	}
	wg.Wait()
	if granted.Load() != 2 || notified.Load() != 1 {
		t.Errorf("granted = %d, notified = %d of 8 simultaneous messages, want 2 and 1", granted.Load(), notified.Load())
	}
}

func TestExpireAlsoRemovesOldPrivateMessages(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	today := DailyQuota{UserID: alice, Since: time.Now().Add(-time.Hour), Limit: 1}
	reservePrivate(t, store, today)
	reservePrivate(t, store, today) // the notice's row
	if _, err := store.pool.Exec(ctx, `UPDATE private_messages SET created_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	reservePrivate(t, store, today)

	if _, err := store.Expire(ctx, 24*time.Hour); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if rows := countRows(t, store, "private_messages"); rows != 1 {
		t.Errorf("private_messages rows = %d, want only today's", rows)
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
