package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
)

const (
	e2eUser    = int64(777001)
	e2eMember  = int64(777002)
	e2eGroup   = int64(-100777000)
	e2eBotID   = int64(777)
	e2eBotName = "MyBot"
)

// fakeTelegram answers getMe, delivers one update, and records what the bot
// sends back.
type fakeTelegram struct {
	update string // JSON of the updates to deliver, comma-separated

	mu        sync.Mutex
	delivered bool
	sent      chan map[string]any
}

func newFakeTelegram(update string) *fakeTelegram {
	return &fakeTelegram{update: update, sent: make(chan map[string]any, 4)}
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/getMe"):
		_, _ = io.WriteString(w, `{"ok":true,"result":{"id":777,"is_bot":true,"username":"MyBot"}}`)
	case strings.HasSuffix(r.URL.Path, "/getChatMember"):
		_, _ = io.WriteString(w, `{"ok":true,"result":{"status":"member"}}`)
	case strings.HasSuffix(r.URL.Path, "/sendMessage"):
		var params map[string]any
		_ = json.NewDecoder(r.Body).Decode(&params)
		f.sent <- params
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":501}}`)
	default:
		f.serveUpdates(w)
	}
}

func (f *fakeTelegram) serveUpdates(w http.ResponseWriter) {
	f.mu.Lock()
	first := !f.delivered
	f.delivered = true
	f.mu.Unlock()
	if !first {
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
		return
	}
	_, _ = io.WriteString(w, `{"ok":true,"result":[`+f.update+`]}`)
}

type modelMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// fakeOpenRouter records the messages of each request and replies through
// answer, which sees the request's messages.
type fakeOpenRouter struct {
	mu       sync.Mutex
	requests [][]modelMessage
	answer   func(messages []modelMessage) string
}

func (f *fakeOpenRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []modelMessage `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.requests = append(f.requests, req.Messages)
	f.mu.Unlock()
	_, _ = io.WriteString(w, f.answer(req.Messages))
}

func (f *fakeOpenRouter) firstRequest() []modelMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[0]
}

// calculatorThenAnswer asks for the calculator, then answers with its result.
func calculatorThenAnswer(t *testing.T) func([]modelMessage) string {
	return func(messages []modelMessage) string {
		last := messages[len(messages)-1]
		if last.Role != "tool" {
			return `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"calculator","arguments":"{\"expression\":\"2+2\"}"}}]}}]}`
		}
		if !strings.Contains(last.Content, `"result":4`) {
			t.Errorf("tool result sent to the model = %s", last.Content)
		}
		return `{"choices":[{"message":{"role":"assistant","content":"2+2 = 4"}}]}`
	}
}

// e2e is the bot wired from real packages against fake Telegram and
// OpenRouter servers and a real PostgreSQL database.
type e2e struct {
	telegram *fakeTelegram
	model    *fakeOpenRouter
	store    *storage.Store
	stop     func() error // shuts the bot down and returns serve's error
}

func startE2E(t *testing.T, update string, answer func([]modelMessage) string) *e2e {
	t.Helper()
	databaseURL := os.Getenv("TEST_db_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_db_POSTGRES_URL is not set; skipping end-to-end test")
	}
	env := &e2e{telegram: newFakeTelegram(update), model: &fakeOpenRouter{answer: answer}}
	telegramServer := httptest.NewServer(env.telegram)
	t.Cleanup(telegramServer.Close)
	modelServer := httptest.NewServer(env.model)
	t.Cleanup(modelServer.Close)

	cfg, err := config.Load(func(key string) string {
		return map[string]string{
			"TELEGRAM_BOT_TOKEN":  "1:test",
			"TELEGRAM_API_URL":    telegramServer.URL,
			"OPENROUTER_API_KEY":  "test",
			"OPENROUTER_BASE_URL": modelServer.URL,
			"db_POSTGRES_URL":     databaseURL,
		}[key]
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	env.store, err = storage.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.store.Close)
	if _, err := env.store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []storage.Scope{privateScope(e2eUser), privateScope(e2eMember), {ChatID: e2eGroup}} {
		if err := env.store.Reset(ctx, scope); err != nil {
			t.Fatal(err)
		}
	}

	served := make(chan error, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { served <- serve(ctx, cfg, env.store, logger) }()
	env.stop = func() error {
		cancel()
		select {
		case err := <-served:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("serve did not shut down")
			return nil
		}
	}
	return env
}

func privateScope(userID int64) storage.Scope {
	return storage.Scope{ChatID: userID, UserID: userID}
}

func (env *e2e) waitForSend(t *testing.T) map[string]any {
	t.Helper()
	select {
	case sent := <-env.telegram.sent:
		return sent
	case <-time.After(10 * time.Second):
		t.Fatal("the bot did not answer")
		return nil
	}
}

func (env *e2e) history(t *testing.T, scope storage.Scope) []ai.Message {
	t.Helper()
	history, err := env.store.History(context.Background(), scope, 20)
	if err != nil {
		t.Fatal(err)
	}
	return history
}

func TestServeAnswersMessageEndToEnd(t *testing.T) {
	env := startE2E(t, `{"update_id":900001,"message":{"message_id":1,
		"from":{"id":777001,"is_bot":false},"chat":{"id":777001,"type":"private"},"text":"what is 2+2?"}}`,
		calculatorThenAnswer(t))

	sent := env.waitForSend(t)
	if sent["text"] != "2+2 = 4" || sent["chat_id"] != float64(e2eUser) {
		t.Errorf("bot sent %v", sent)
	}
	if _, present := sent["reply_parameters"]; present {
		t.Error("a private answer was sent as a reply")
	}
	if err := env.stop(); err != nil {
		t.Errorf("serve returned %v after shutdown", err)
	}

	history := env.history(t, privateScope(e2eUser))
	if len(history) != 4 || history[2].Role != "tool" || history[3].Content != "2+2 = 4" {
		t.Errorf("saved history = %+v", history)
	}
}

const groupChatter = `{"update_id":900010,"message":{"message_id":28,
		"from":{"id":555,"is_bot":false,"first_name":"Ann"},"chat":{"id":-100777000,"type":"supergroup"},
		"text":"Go is interpreted, everyone knows that"}},
	{"update_id":900011,"message":{"message_id":29,
		"from":{"id":777002,"is_bot":false,"first_name":"Bek"},"chat":{"id":-100777000,"type":"supergroup"},
		"text":"no way 😀"}},
	{"update_id":900012,"message":{"message_id":30,
		"from":{"id":777002,"is_bot":false,"first_name":"Bek"},"chat":{"id":-100777000,"type":"supergroup"},
		"text":"Hey Mira, who is right?"}}`

// Two members chat, then one calls the bot by name. The bot must answer once,
// as a reply to the message that called it, in character, with the earlier
// chatter as context — and that chatter must not end up in the database.
func TestServeAnswersWakeWordWithRecentChatterEndToEnd(t *testing.T) {
	env := startE2E(t, groupChatter, func([]modelMessage) string {
		return `{"choices":[{"message":{"role":"assistant","content":"Ann, compile your facts first 😂"}}]}`
	})

	sent := env.waitForSend(t)
	if sent["text"] != "Ann, compile your facts first 😂" || sent["chat_id"] != float64(e2eGroup) {
		t.Errorf("bot sent %v", sent)
	}
	reply, _ := sent["reply_parameters"].(map[string]any)
	if reply["message_id"] != float64(30) {
		t.Errorf("reply_parameters = %v, want a reply to message 30", sent["reply_parameters"])
	}
	if err := env.stop(); err != nil {
		t.Errorf("serve returned %v after shutdown", err)
	}
	select {
	case extra := <-env.telegram.sent:
		t.Errorf("the bot sent a second message: %v", extra)
	default:
	}

	request := env.model.firstRequest()
	system := request[0].Content
	if !strings.Contains(system, "You are Mira") || !strings.Contains(system, "group chat") {
		t.Errorf("system prompt = %.120q, want the persona with the group note", system)
	}
	question := request[len(request)-1].Content
	for _, want := range []string{"Recent messages in the group", "Ann: Go is interpreted, everyone knows that", "Bek: no way 😀", "Bek: who is right?"} {
		if !strings.Contains(question, want) {
			t.Errorf("user message %q does not contain %q", question, want)
		}
	}
	if strings.Contains(question, "Hey Mira") {
		t.Errorf("the wake phrase was not removed: %q", question)
	}

	groupHistory := env.history(t, storage.Scope{ChatID: e2eGroup})
	if len(groupHistory) != 2 || groupHistory[0].Content != "Bek: who is right?" || groupHistory[1].Content != "Ann, compile your facts first 😂" {
		t.Errorf("shared group history = %+v", groupHistory)
	}
	if private := env.history(t, privateScope(e2eMember)); len(private) != 0 {
		t.Errorf("the group exchange leaked into the private history: %+v", private)
	}
	if got := sentMessageID(t, 900012); got != 501 {
		t.Errorf("stored sent message ID = %d, want 501", got)
	}
	if rows := rowsContaining(t, "interpreted") + rowsContaining(t, "no way"); rows != 0 {
		t.Errorf("%d database rows contain ordinary group messages; they must stay in memory", rows)
	}
	if author := storedAuthor(t, 900012); author != "777002 Bek" {
		t.Errorf("stored author = %q, want the sender's ID and first name", author)
	}
}

// A group muted before the bot started must stay silent after a restart.
func TestMutedGroupStaysSilentAfterRestart(t *testing.T) {
	databaseURL := os.Getenv("TEST_db_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_db_POSTGRES_URL is not set; skipping end-to-end test")
	}
	setMuted(t, databaseURL, true)
	t.Cleanup(func() { setMuted(t, databaseURL, false) })

	env := startE2E(t, groupChatter, func([]modelMessage) string {
		return `{"choices":[{"message":{"role":"assistant","content":"should never be sent"}}]}`
	})

	select {
	case sent := <-env.telegram.sent:
		t.Errorf("the bot spoke in a muted group: %v", sent)
	case <-time.After(400 * time.Millisecond):
	}
	if err := env.stop(); err != nil {
		t.Errorf("serve returned %v after shutdown", err)
	}
	if history := env.history(t, storage.Scope{ChatID: e2eGroup}); len(history) != 0 {
		t.Errorf("a muted group got history: %+v", history)
	}
}

func setMuted(t *testing.T, databaseURL string, muted bool) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMuted(ctx, e2eGroup, muted, e2eUser); err != nil {
		t.Fatal(err)
	}
}

func queryOne[T any](t *testing.T, sql string, args ...any) T {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("TEST_db_POSTGRES_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var value T
	if err := conn.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func rowsContaining(t *testing.T, text string) int {
	return queryOne[int](t, `SELECT count(*) FROM messages WHERE content LIKE '%' || $1 || '%'`, text)
}

func storedAuthor(t *testing.T, updateID int64) string {
	return queryOne[string](t, `SELECT author_id || ' ' || author_name FROM messages WHERE update_id = $1`, updateID)
}

// sentMessageID reads the Telegram message ID recorded on the answer of the
// turn that the given update produced. Zero means none was recorded.
func sentMessageID(t *testing.T, updateID int64) int64 {
	return queryOne[int64](t, `
		SELECT coalesce(answer.telegram_message_id, 0)
		FROM messages question
		JOIN messages answer ON answer.conversation_id = question.conversation_id AND answer.role = 'assistant'
		WHERE question.update_id = $1
		ORDER BY answer.id DESC LIMIT 1`, updateID)
}

func TestServeFailsFastOnBadToken(t *testing.T) {
	databaseURL := os.Getenv("TEST_db_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_db_POSTGRES_URL is not set; skipping startup test")
	}
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
	}))
	defer telegramServer.Close()
	cfg, err := config.Load(func(key string) string {
		return map[string]string{
			"TELEGRAM_BOT_TOKEN": "1:wrong-secret",
			"TELEGRAM_API_URL":   telegramServer.URL,
			"OPENROUTER_API_KEY": "test",
			"db_POSTGRES_URL":    databaseURL,
		}[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	err = serve(ctx, cfg, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "wrong-secret") {
		t.Fatalf("serve = %v, want a 401 error without the token", err)
	}
}

// Migrations are run by an operator with the owner role. They must not need
// the bot's other secrets.
func TestMigrateNeedsOnlyDatabaseURL(t *testing.T) {
	databaseURL := os.Getenv("TEST_db_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_db_POSTGRES_URL is not set; skipping migrate test")
	}
	t.Setenv("db_POSTGRES_URL", databaseURL)
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	if err := run([]string{"migrate"}); err != nil {
		t.Fatalf("run migrate: %v", err)
	}
}

func TestMigrateWithoutDatabaseURLNamesTheVariable(t *testing.T) {
	t.Setenv("db_POSTGRES_URL", "")

	err := run([]string{"migrate"})
	if err == nil || !strings.Contains(err.Error(), "db_POSTGRES_URL") {
		t.Fatalf("err = %v, want mention of db_POSTGRES_URL", err)
	}
}
