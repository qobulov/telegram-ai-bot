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

// newTestStore connects to the database named by TEST_DATABASE_URL and
// rebuilds the schema from scratch. It DROPS the bot's tables, so point it
// only at a disposable database. Without the variable the test is skipped.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(store.Close)

	if _, err := store.pool.Exec(ctx, `DROP TABLE IF EXISTS messages, conversations, users, schema_migrations CASCADE`); err != nil {
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
	if err := store.SaveTurn(context.Background(), userID, updateID, msgs); err != nil {
		t.Fatalf("SaveTurn: %v", err)
	}
}

func mustHistory(t *testing.T, store *Store, userID int64, limit int) []ai.Message {
	t.Helper()
	history, err := store.History(context.Background(), userID, limit)
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
	if _, err := store.pool.Exec(ctx, `DROP TABLE messages, conversations, users, schema_migrations CASCADE`); err != nil {
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

	if err := store.Reset(context.Background(), alice); err != nil {
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

	if err := store.Reset(context.Background(), 999); err != nil {
		t.Errorf("Reset: %v", err)
	}
}

func TestSaveTurnRejectsDuplicateUpdate(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, alice, 7, turn("q", "a"))

	err := store.SaveTurn(context.Background(), alice, 7, turn("q", "a"))
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

	if err := store.SaveTurn(context.Background(), alice, 1, nil); err == nil {
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
