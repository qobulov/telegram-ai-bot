package kassa

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/storage"
)

const testToken = "123:test-token"

func signedInit(user int64, at time.Time) string {
	values := url.Values{"user": {fmt.Sprintf(`{"id":%d,"first_name":"Bek"}`, user)}, "auth_date": {fmt.Sprint(at.Unix())}}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + values.Get(k)
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(testToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}

// fakeStore keeps entries in memory, by user.
type fakeStore struct {
	next    int64
	entries map[int64][]storage.KassaEntry
}

func (f *fakeStore) AddKassaEntry(_ context.Context, userID int64, e storage.KassaEntry) (int64, error) {
	f.next++
	e.ID = f.next
	f.entries[userID] = append([]storage.KassaEntry{e}, f.entries[userID]...)
	return e.ID, nil
}

func (f *fakeStore) KassaEntries(_ context.Context, userID int64, kind string, _ int) ([]storage.KassaEntry, error) {
	var list []storage.KassaEntry
	for _, e := range f.entries[userID] {
		if e.Kind == kind {
			list = append(list, e)
		}
	}
	return list, nil
}

func (f *fakeStore) KassaTotals(_ context.Context, userID int64) (storage.KassaTotals, error) {
	var t storage.KassaTotals
	for _, e := range f.entries[userID] {
		if e.Kind == storage.KassaIn {
			t.In += e.Amount
		} else {
			t.Out += e.Amount
		}
	}
	t.Balance = t.In - t.Out
	return t, nil
}

func (f *fakeStore) DeleteKassaEntry(_ context.Context, userID, id int64) error {
	for i, e := range f.entries[userID] {
		if e.ID == id {
			f.entries[userID] = append(f.entries[userID][:i], f.entries[userID][i+1:]...)
			return nil
		}
	}
	return storage.ErrNoKassaEntry
}

func post(t *testing.T, h http.Handler, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/kassa", strings.NewReader(string(raw))))
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, got
}

func TestKassaHandler(t *testing.T) {
	now := time.Date(2026, 11, 24, 4, 0, 0, 0, time.UTC)
	store := &fakeStore{entries: map[int64][]storage.KassaEntry{}}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	me, them := signedInit(1, now), signedInit(2, now)

	code, got := post(t, h, map[string]any{"init_data": me, "action": "state"})
	if code != http.StatusOK || got["today"] != "2026-11-24" {
		t.Fatalf("state = %d %v", code, got)
	}
	post(t, h, map[string]any{"init_data": me, "action": "add", "kind": "in", "amount": 500000, "note": " oylik ", "day": "2026-11-20"})
	code, got = post(t, h, map[string]any{"init_data": me, "action": "add", "kind": "out", "amount": 120000, "note": "bozor", "day": "2026-11-24"})
	totals := got["totals"].(map[string]any)
	if code != http.StatusOK || totals["balance"] != float64(380000) {
		t.Fatalf("after add = %d %v", code, got)
	}
	in := got["in"].([]any)[0].(map[string]any)
	if in["note"] != "oylik" || in["day"] != "2026-11-20" {
		t.Fatalf("in entry = %v", in)
	}

	for name, tc := range map[string]struct {
		body map[string]any
		want string
	}{
		"forged":       {map[string]any{"init_data": strings.Replace(me, "Bek", "Ali", 1), "action": "state"}, "forged"},
		"stale":        {map[string]any{"init_data": signedInit(1, now.Add(-48*time.Hour)), "action": "state"}, "stale"},
		"zero amount":  {map[string]any{"init_data": me, "action": "add", "kind": "in", "amount": 0, "day": "2026-11-24"}, "amount"},
		"future day":   {map[string]any{"init_data": me, "action": "add", "kind": "in", "amount": 5, "day": "2026-11-27"}, "day"},
		"bad day":      {map[string]any{"init_data": me, "action": "add", "kind": "in", "amount": 5, "day": "24.11.2026"}, "day"},
		"bad kind":     {map[string]any{"init_data": me, "action": "add", "kind": "loan", "amount": 5, "day": "2026-11-24"}, "bad_request"},
		"long note":    {map[string]any{"init_data": me, "action": "add", "kind": "in", "amount": 5, "day": "2026-11-24", "note": strings.Repeat("a", 301)}, "note"},
		"their delete": {map[string]any{"init_data": them, "action": "delete", "id": 1}, "not_found"},
	} {
		if _, got := post(t, h, tc.body); got["error"] != tc.want {
			t.Errorf("%s: error = %v, want %s", name, got["error"], tc.want)
		}
	}

	code, got = post(t, h, map[string]any{"init_data": me, "action": "delete", "id": 1})
	if code != http.StatusOK || got["totals"].(map[string]any)["balance"] != float64(-120000) {
		t.Fatalf("after delete = %d %v", code, got)
	}
	if _, got := post(t, h, map[string]any{"init_data": them, "action": "state"}); len(got["out"].([]any)) != 0 {
		t.Fatalf("another user sees %v", got["out"])
	}
}

// A press of Play on the "kassa" game opens the page without init data:
// its session says whose book it is, and another game's session does not.
func TestKassaGameSession(t *testing.T) {
	now := time.Date(2026, 11, 24, 4, 0, 0, 0, time.UTC)
	games := game.New(testToken, "https://example.com/game/", func() time.Time { return now })
	store := &fakeStore{entries: map[int64][]storage.KassaEntry{7: {{ID: 1, Kind: storage.KassaIn, Amount: 900}}}}
	h := NewHandler(Options{BotToken: testToken, Store: store, Sessions: games, Now: func() time.Time { return now }})
	session := func(name string) string {
		link, ok := games.PlayURL(name, game.Player{UserID: 7, ChatID: 7, MessageID: 1})
		if !ok {
			t.Fatalf("no page for %s", name)
		}
		u, _ := url.Parse(link)
		return u.Query().Get("s")
	}

	code, got := post(t, h, map[string]any{"session": session(game.KassaShortName), "action": "state"})
	if code != http.StatusOK || got["totals"].(map[string]any)["balance"] != float64(900) {
		t.Fatalf("kassa session = %d %v", code, got)
	}
	if _, got := post(t, h, map[string]any{"session": session(game.MafiaShortName), "action": "state"}); got["error"] != "forged" {
		t.Errorf("mafia's session: %v, want forged", got["error"])
	}
	if _, got := post(t, h, map[string]any{"session": "made.up", "action": "state"}); got["error"] != "stale" {
		t.Errorf("a made-up session: %v, want stale", got["error"])
	}
}
