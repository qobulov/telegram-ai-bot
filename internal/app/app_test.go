package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/webhook"
)

const (
	hookSecret = "hook-secret_1"
	hookUser   = int64(778001)
	hookGroup  = int64(-100778000)
)

// fakeAPIs stands in for Telegram and OpenRouter and records what is sent.
type fakeAPIs struct {
	mu          sync.Mutex
	sent        []map[string]any
	modelInputs []string
}

func (f *fakeAPIs) telegram(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/getMe"):
		_, _ = io.WriteString(w, `{"ok":true,"result":{"id":777,"is_bot":true,"username":"MyBot"}}`)
	case strings.HasSuffix(r.URL.Path, "/sendMessage"):
		var params map[string]any
		_ = json.NewDecoder(r.Body).Decode(&params)
		f.mu.Lock()
		f.sent = append(f.sent, params)
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":601}}`)
	default:
		_, _ = io.WriteString(w, `{"ok":true,"result":{"status":"member"}}`)
	}
}

func (f *fakeAPIs) model(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.modelInputs = append(f.modelInputs, string(body))
	f.mu.Unlock()
	_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"webhook answer"}}]}`)
}

func (f *fakeAPIs) messages() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.sent...)
}

type hookEnv struct {
	apis    *fakeAPIs
	hook    *Webhook
	store   *storage.Store
	baseEnv map[string]string
}

func startWebhook(t *testing.T) *hookEnv {
	t.Helper()
	databaseURL := os.Getenv("TEST_DB_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_DB_POSTGRES_URL is not set; skipping webhook end-to-end test")
	}
	env := &hookEnv{apis: &fakeAPIs{}}
	telegramServer := httptest.NewServer(http.HandlerFunc(env.apis.telegram))
	t.Cleanup(telegramServer.Close)
	modelServer := httptest.NewServer(http.HandlerFunc(env.apis.model))
	t.Cleanup(modelServer.Close)
	env.baseEnv = map[string]string{
		"TELEGRAM_BOT_TOKEN":      "1:test",
		"TELEGRAM_API_URL":        telegramServer.URL,
		"TELEGRAM_WEBHOOK_SECRET": hookSecret,
		"OPENROUTER_API_KEY":      "test",
		"OPENROUTER_BASE_URL":     modelServer.URL,
		"DB_POSTGRES_URL":         databaseURL,
		"BOT_DISPLAY_NAME":        "Mira",
	}

	ctx := context.Background()
	store, err := storage.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	env.store = store
	if _, err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []storage.Scope{{ChatID: hookUser, UserID: hookUser}, {ChatID: hookGroup}} {
		if err := store.Reset(ctx, scope); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ClearGroupMessages(ctx, hookGroup); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMuted(ctx, hookGroup, false, hookUser); err != nil {
		t.Fatal(err)
	}
	forgetBotRights(t, databaseURL)

	env.hook, err = NewWebhook(ctx, env.config(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}
	t.Cleanup(env.hook.Close)
	return env
}

// forgetBotRights clears the test group's stored rights. Rights stored by an
// earlier run would be used as they are, and the fallback to Telegram would
// then go untested.
func forgetBotRights(t *testing.T, databaseURL string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `UPDATE groups SET bot_status = NULL, bot_rights = NULL WHERE chat_id = $1`, hookGroup); err != nil {
		t.Fatal(err)
	}
}

func (env *hookEnv) config(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(func(key string) string { return env.baseEnv[key] })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func (env *hookEnv) deliver(update, secret string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/webhook", strings.NewReader(update))
	req.Header.Set(webhook.SecretHeader, secret)
	resp := httptest.NewRecorder()
	env.hook.Handler.ServeHTTP(resp, req)
	return resp.Code
}

const privateUpdate = `{"update_id":910001,"message":{"message_id":7,
	"from":{"id":778001,"is_bot":false,"first_name":"Bek"},"chat":{"id":778001,"type":"private"},"text":"salom"}}`

// groupUpdate is a group message whose message ID is its update ID: like
// Telegram's, every message has its own, which the group log relies on.
func groupUpdate(updateID, text string) string {
	return `{"update_id":` + updateID + `,"message":{"message_id":` + updateID + `,
		"from":{"id":778001,"is_bot":false,"first_name":"Bek"},"chat":{"id":-100778000,"type":"supergroup"},
		"text":"` + text + `"}}`
}

// On serverless hosting nothing runs after the response, so by the time the
// webhook answers Telegram the reply must already be sent and saved.
func TestWebhookAnswersBeforeResponding(t *testing.T) {
	env := startWebhook(t)

	status := env.deliver(privateUpdate, hookSecret)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	sent := env.apis.messages()
	if len(sent) != 1 || sent[0]["text"] != "webhook answer" || sent[0]["chat_id"] != float64(hookUser) {
		t.Fatalf("messages sent before the response = %v", sent)
	}
	history, err := env.store.History(context.Background(), storage.Scope{ChatID: hookUser, UserID: hookUser}, 10)
	if err != nil || len(history) != 2 {
		t.Errorf("saved history = %+v, err = %v", history, err)
	}
}

func TestWebhookRejectsForgedUpdates(t *testing.T) {
	env := startWebhook(t)

	if status := env.deliver(privateUpdate, "wrong"); status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if sent := env.apis.messages(); len(sent) != 0 {
		t.Errorf("a forged update was answered: %v", sent)
	}
}

func TestWebhookRedeliveryIsAnsweredOnce(t *testing.T) {
	env := startWebhook(t)

	env.deliver(privateUpdate, hookSecret)
	env.deliver(privateUpdate, hookSecret) // Telegram retries after a slow response

	if sent := env.apis.messages(); len(sent) != 1 {
		t.Errorf("messages sent = %d, want 1 for a redelivered update", len(sent))
	}
}

func TestWebhookGroupFlow(t *testing.T) {
	env := startWebhook(t)

	env.deliver(groupUpdate("910010", "just chatting with friends"), hookSecret)
	if sent := env.apis.messages(); len(sent) != 0 {
		t.Fatalf("an unaddressed group message was answered: %v", sent)
	}

	env.deliver(groupUpdate("910011", "Mira, salom"), hookSecret)
	sent := env.apis.messages()
	if len(sent) != 1 || sent[0]["chat_id"] != float64(hookGroup) {
		t.Fatalf("sent = %v", sent)
	}
	if reply, _ := sent[0]["reply_parameters"].(map[string]any); reply["message_id"] != float64(910011) {
		t.Errorf("reply_parameters = %v", sent[0]["reply_parameters"])
	}
	env.apis.mu.Lock()
	input := env.apis.modelInputs[0]
	env.apis.mu.Unlock()
	// Each request may run in a different instance, so the day's log has to
	// come from the database for the earlier message to be seen.
	if !strings.Contains(input, "just chatting with friends") {
		t.Error("the earlier group message did not reach the model")
	}
	// The fake Telegram says the bot is a plain member, and that answer is
	// kept, so later turns need not ask again.
	if !strings.Contains(input, "You are not an admin here.") {
		t.Error("the turn header with the bot's rights did not reach the model")
	}
	if _, known, err := env.store.BotRights(context.Background(), hookGroup); err != nil || !known {
		t.Errorf("bot rights known = %v, err = %v; want them stored", known, err)
	}
	logged, err := env.store.GroupMessagesSince(context.Background(), hookGroup, time.Now().Add(-time.Hour), 10)
	if err != nil || len(logged) != 3 {
		t.Fatalf("group log = %+v, err = %v; want the chatter, the question and the answer", logged, err)
	}
	if logged[2].AuthorName != "You" || logged[2].Text != "webhook answer" {
		t.Errorf("the bot's answer was not logged: %+v", logged[2])
	}
}

// Mute state must come from the database on every request: the next request
// may run in a different instance that never saw the /mute.
func TestWebhookHonoursMuteSetElsewhere(t *testing.T) {
	env := startWebhook(t)
	ctx := context.Background()

	if err := env.store.SetMuted(ctx, hookGroup, true, hookUser); err != nil {
		t.Fatal(err)
	}
	env.deliver(groupUpdate("910020", "Mira, salom"), hookSecret)
	if sent := env.apis.messages(); len(sent) != 0 {
		t.Fatalf("the bot answered in a group muted in the database: %v", sent)
	}

	if err := env.store.SetMuted(ctx, hookGroup, false, hookUser); err != nil {
		t.Fatal(err)
	}
	env.deliver(groupUpdate("910021", "Mira, salom"), hookSecret)
	if sent := env.apis.messages(); len(sent) != 1 {
		t.Errorf("messages after unmuting = %d, want 1", len(sent))
	}
}

// If BOT_OWNER_IDS stopped reaching the service, owners would quietly be
// recognised by username again, and whoever took over an owner's old
// username would be one. The service's own tests cannot see that wiring.
func TestWebhookOwnersAreKnownByID(t *testing.T) {
	env := startWebhook(t)
	env.baseEnv["BOT_OWNER_USERNAME"] = "bek_owner"
	env.baseEnv["BOT_OWNER_IDS"] = "778009"
	hook, err := NewWebhook(context.Background(), env.config(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}
	t.Cleanup(hook.Close)
	env.hook = hook

	env.deliver(`{"update_id":910030,"message":{"message_id":910030,
		"from":{"id":778002,"is_bot":false,"first_name":"Bek","username":"bek_owner"},
		"chat":{"id":-100778000,"type":"supergroup"},"text":"Mira, salom"}}`, hookSecret)

	env.apis.mu.Lock()
	inputs := env.apis.modelInputs
	env.apis.mu.Unlock()
	if len(inputs) != 1 || !strings.Contains(inputs[0], "Bek @bek_owner") {
		t.Fatalf("model inputs = %q, want one naming Bek @bek_owner", inputs)
	}
	if strings.Contains(inputs[0], "@bek_owner [owner]") {
		t.Error("an account not in BOT_OWNER_IDS was marked as an owner for having an owner's username")
	}
}

func TestWebhookNeedsASecret(t *testing.T) {
	env := startWebhook(t)
	delete(env.baseEnv, "TELEGRAM_WEBHOOK_SECRET")

	_, err := NewWebhook(context.Background(), env.config(t), slog.Default())
	if err == nil || !strings.Contains(err.Error(), "TELEGRAM_WEBHOOK_SECRET") {
		t.Fatalf("err = %v, want it to name TELEGRAM_WEBHOOK_SECRET", err)
	}
}

const cronSecret = "a-long-random-cron-secret"

func TestCleanupEndpointExpiresOldHistory(t *testing.T) {
	env := startWebhook(t)
	env.baseEnv["CRON_SECRET"] = cronSecret
	ctx := context.Background()
	env.deliver(privateUpdate, hookSecret) // one turn: 2 messages
	conn, err := pgx.Connect(ctx, os.Getenv("TEST_DB_POSTGRES_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `UPDATE messages SET created_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}

	cleanup, err := NewCleanup(ctx, env.config(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCleanup: %v", err)
	}
	defer cleanup.Close()

	unauthorized := httptest.NewRecorder()
	cleanup.Handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/cleanup", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Errorf("without the secret: status = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/cleanup", nil)
	req.Header.Set("Authorization", "Bearer "+cronSecret)
	resp := httptest.NewRecorder()
	cleanup.Handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"messages":2`) {
		t.Errorf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	history, _ := env.store.History(ctx, storage.Scope{ChatID: hookUser, UserID: hookUser}, 10)
	if len(history) != 0 {
		t.Errorf("history after cleanup = %+v", history)
	}
}

func TestCleanupNeedsACronSecret(t *testing.T) {
	env := startWebhook(t)

	_, err := NewCleanup(context.Background(), env.config(t), slog.Default())
	if err == nil || !strings.Contains(err.Error(), "CRON_SECRET") {
		t.Fatalf("err = %v, want it to name CRON_SECRET", err)
	}
}

// fakeRightsStore keeps one group's rights in memory.
type fakeRightsStore struct {
	rights  []string
	known   bool
	readErr error
	saveErr error
	saved   []savedRights
}

type savedRights struct {
	chatID int64
	status string
	rights []string
}

func (f *fakeRightsStore) BotRights(context.Context, int64) ([]string, bool, error) {
	return f.rights, f.known, f.readErr
}

func (f *fakeRightsStore) SetBotRights(_ context.Context, chatID int64, status string, rights []string) error {
	f.saved = append(f.saved, savedRights{chatID, status, rights})
	return f.saveErr
}

// fakeMemberLookup answers getChatMember for the bot.
type fakeMemberLookup struct {
	member telegram.ChatMember
	err    error
	asked  [][2]int64
}

func (f *fakeMemberLookup) GetChatMember(_ context.Context, chatID, userID int64) (telegram.ChatMember, error) {
	f.asked = append(f.asked, [2]int64{chatID, userID})
	return f.member, f.err
}

func TestBotRightsFallsBackToTelegramAndStores(t *testing.T) {
	const botID = int64(777)
	admin := telegram.ChatMember{Status: "administrator", CanDeleteMessages: true, CanRestrictMembers: true}
	adminRights := []string{telegram.RightDeleteMessages, telegram.RightRestrictMembers}
	cases := map[string]struct {
		store      *fakeRightsStore
		telegram   *fakeMemberLookup
		want       []string
		wantErr    bool
		wantAsked  bool
		wantStored []savedRights
	}{
		"stored rights are used as they are": {
			store:    &fakeRightsStore{known: true, rights: []string{telegram.RightPinMessages}},
			telegram: &fakeMemberLookup{member: admin},
			want:     []string{telegram.RightPinMessages},
		},
		"stored as none is still known": {
			store:    &fakeRightsStore{known: true},
			telegram: &fakeMemberLookup{member: admin},
		},
		"unknown rights are asked of Telegram and stored": {
			store:      &fakeRightsStore{},
			telegram:   &fakeMemberLookup{member: admin},
			want:       adminRights,
			wantAsked:  true,
			wantStored: []savedRights{{hookGroup, "administrator", adminRights}},
		},
		"a member holds none": {
			store:      &fakeRightsStore{},
			telegram:   &fakeMemberLookup{member: telegram.ChatMember{Status: "member"}},
			wantAsked:  true,
			wantStored: []savedRights{{hookGroup, "member", nil}},
		},
		"Telegram cannot be asked": {
			store:     &fakeRightsStore{},
			telegram:  &fakeMemberLookup{err: errors.New("telegram: 502")},
			wantErr:   true,
			wantAsked: true,
		},
		"the store cannot be read": {
			store:    &fakeRightsStore{readErr: errors.New("connection refused")},
			telegram: &fakeMemberLookup{member: admin},
			wantErr:  true,
		},
		// Storing is only a cache: Telegram's answer is still the truth.
		"the answer cannot be stored": {
			store:      &fakeRightsStore{saveErr: errors.New("connection refused")},
			telegram:   &fakeMemberLookup{member: admin},
			want:       adminRights,
			wantAsked:  true,
			wantStored: []savedRights{{hookGroup, "administrator", adminRights}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			source := botRights{store: c.store, members: c.telegram, botID: botID, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

			rights, err := source.BotRights(context.Background(), hookGroup)

			if (err != nil) != c.wantErr || !reflect.DeepEqual(rights, c.want) {
				t.Errorf("rights = %v, err = %v; want %v, error %v", rights, err, c.want, c.wantErr)
			}
			if asked := len(c.telegram.asked) > 0; asked != c.wantAsked {
				t.Errorf("Telegram asked = %v, want %v", c.telegram.asked, c.wantAsked)
			} else if asked && c.telegram.asked[0] != [2]int64{hookGroup, botID} {
				t.Errorf("Telegram asked about %v, want the bot in the group", c.telegram.asked[0])
			}
			if !reflect.DeepEqual(c.store.saved, c.wantStored) {
				t.Errorf("stored = %+v, want %+v", c.store.saved, c.wantStored)
			}
		})
	}
}
