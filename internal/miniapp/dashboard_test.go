package miniapp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	choyxona = int64(-1001234567890)
	owner    = int64(42)
	member   = int64(77)
)

type fakeStore struct {
	statsFor []int64
	since    time.Time
}

func (f *fakeStore) GroupStats(_ context.Context, chatID, botID int64, since time.Time, timeZone string, limit int) (storage.GroupStats, error) {
	f.statsFor = append(f.statsFor, chatID)
	f.since = since
	stats := storage.GroupStats{
		Messages: 5, Members: 2,
		Top:      []storage.AuthorCount{{UserID: owner, Name: "Ali @ali", Count: 4}, {UserID: 9, Name: "Bek", Count: 1}},
		Swearers: []storage.AuthorCount{{UserID: 9, Name: "Bek", Count: 2}},
	}
	stats.Hours[13] = 5
	return stats, nil
}

func (f *fakeStore) GroupRules(_ context.Context, chatID int64) ([]storage.GroupRule, error) {
	return []storage.GroupRule{
		{ChatID: chatID, Kind: storage.RuleLinkGuard},
		{ChatID: chatID, Kind: storage.RuleInstruction, Text: "be kind"},
		{ChatID: chatID, Kind: storage.RuleSwearGuard},
		{ChatID: 0, Kind: storage.RuleAddress},
	}, nil
}

func (f *fakeStore) Groups(_ context.Context, since time.Time, limit int) ([]storage.GroupSummary, error) {
	return []storage.GroupSummary{
		{ChatID: choyxona, Title: "Choyxona", BotStatus: "administrator"},
		{ChatID: -1009, Title: "Old", BotStatus: "left"},
	}, nil
}

type fakeTelegram struct{}

func (f *fakeTelegram) GetChat(_ context.Context, chatID int64) (telegram.Chat, error) {
	return telegram.Chat{ID: chatID, Title: "Choyxona"}, nil
}

type dashboardEnv struct {
	store    *fakeStore
	telegram *fakeTelegram
	handler  http.Handler
}

func newDashboardEnv(t *testing.T) *dashboardEnv {
	t.Helper()
	env := &dashboardEnv{store: &fakeStore{}, telegram: &fakeTelegram{}}
	tashkent, err := time.LoadLocation("Asia/Tashkent")
	if err != nil {
		t.Fatal(err)
	}
	env.handler, err = NewHandler(Options{
		BotToken: testToken, BotID: 4242, Store: env.store, Telegram: env.telegram, Owners: map[int64]bool{owner: true},
		Location: tashkent, Now: func() time.Time { return testNow },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func (env *dashboardEnv) post(t *testing.T, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/stats", strings.NewReader(string(raw))))
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response %q: %v", rec.Body.String(), err)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	return rec.Code, got
}

// Opened from a group's link, an owner sees that group's numbers, its
// guards and its title.
func TestDashboardShowsTheGroupToAnOwner(t *testing.T) {
	env := newDashboardEnv(t)

	status, got := env.post(t, request{InitData: openedBy(owner, testNow, GroupParam(choyxona)), Period: "week"})

	if status != http.StatusOK {
		t.Fatalf("status %d: %v", status, got)
	}
	if got["group"] != GroupParam(choyxona) || got["title"] != "Choyxona" || got["period"] != "week" || got["messages"] != float64(5) || got["members"] != float64(2) {
		t.Errorf("got %v", got)
	}
	if want := []any{"swear", "links"}; !reflect.DeepEqual(got["guards"], want) {
		t.Errorf("guards = %v, want %v", got["guards"], want)
	}
	top := got["top"].([]any)
	if len(top) != 2 || top[0].(map[string]any)["name"] != "Ali @ali" || top[0].(map[string]any)["count"] != float64(4) {
		t.Errorf("top = %v", top)
	}
	if swearers := got["swearers"].([]any); len(swearers) != 1 {
		t.Errorf("swearers = %v", swearers)
	}
	if hours := got["hours"].([]any); len(hours) != 24 || hours[13] != float64(5) {
		t.Errorf("hours = %v", hours)
	}
	if !env.store.since.Equal(testNow.Add(-7 * 24 * time.Hour)) {
		t.Errorf("since = %v, want a week ago", env.store.since)
	}
}

// The dashboard names who writes and swears most, so only the bot's owners
// see it: a member of the group, with its link or naming it, sees nothing,
// and nothing is counted for them.
func TestDashboardRefusesWhoIsNoOwner(t *testing.T) {
	env := newDashboardEnv(t)

	for _, body := range []request{
		{InitData: openedBy(member, testNow, GroupParam(choyxona))},
		{InitData: openedBy(member, testNow, ""), Group: GroupParam(choyxona)},
		{InitData: openedBy(member, testNow, "")},
	} {
		if status, got := env.post(t, body); status != http.StatusForbidden || got["error"] != "not_owner" {
			t.Errorf("status %d, %v; want 403 not_owner", status, got)
		}
	}
	if len(env.store.statsFor) != 0 {
		t.Errorf("counted %v for a member", env.store.statsFor)
	}
}

// Opened from the private chat, without a group, the bot's groups are
// offered, not those it has left.
func TestDashboardListsTheBotsGroups(t *testing.T) {
	env := newDashboardEnv(t)

	status, got := env.post(t, request{InitData: openedBy(owner, testNow, "")})

	want := []any{map[string]any{"group": GroupParam(choyxona), "title": "Choyxona"}}
	if status != http.StatusOK || !reflect.DeepEqual(got["groups"], want) {
		t.Errorf("status %d, %v; want %v", status, got, want)
	}
}

func TestDashboardRefusesBadRequests(t *testing.T) {
	env := newDashboardEnv(t)
	cases := map[string]struct {
		body   any
		status int
		code   string
	}{
		"forged":     {request{InitData: strings.Replace(openedBy(owner, testNow, ""), "auth_date=", "auth_date=1", 1)}, http.StatusUnauthorized, "unauthorized"},
		"expired":    {request{InitData: openedBy(owner, testNow.Add(-2*time.Hour), "")}, http.StatusUnauthorized, "expired"},
		"bad group":  {request{InitData: openedBy(owner, testNow, ""), Group: "-100123"}, http.StatusBadRequest, "bad_request"},
		"bad period": {request{InitData: openedBy(owner, testNow, GroupParam(choyxona)), Period: "year"}, http.StatusBadRequest, "bad_request"},
		"not json":   {"[", http.StatusBadRequest, "bad_request"},
	}
	for name, c := range cases {
		if status, got := env.post(t, c.body); status != c.status || got["error"] != c.code {
			t.Errorf("%s: %d %v; want %d %s", name, status, got, c.status, c.code)
		}
	}
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", rec.Code)
	}
	if len(env.store.statsFor) != 0 {
		t.Errorf("counted %v for bad requests", env.store.statsFor)
	}
}
