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

type fakeBook struct {
	id, user int64
	name     string
	entries  []storage.KassaEntry
}

// fakeStore keeps books and their entries in memory.
type fakeStore struct {
	next  int64
	books []*fakeBook
}

func (f *fakeStore) book(userID, bookID int64) *fakeBook {
	for _, b := range f.books {
		if b.id == bookID && b.user == userID {
			return b
		}
	}
	return nil
}

func totals(b *fakeBook) storage.KassaTotals {
	var t storage.KassaTotals
	for _, e := range b.entries {
		if e.Kind == storage.KassaIn {
			t.In += e.Amount
		} else {
			t.Out += e.Amount
		}
	}
	t.Balance = t.In - t.Out
	return t
}

func (f *fakeStore) KassaBooks(_ context.Context, userID int64) ([]storage.KassaBook, error) {
	var list []storage.KassaBook
	for _, b := range f.books {
		if b.user == userID {
			list = append(list, storage.KassaBook{ID: b.id, Name: b.name, Totals: totals(b)})
		}
	}
	return list, nil
}

func (f *fakeStore) KassaBook(_ context.Context, userID, bookID int64) (storage.KassaBook, error) {
	b := f.book(userID, bookID)
	if b == nil {
		return storage.KassaBook{}, storage.ErrNoKassaBook
	}
	return storage.KassaBook{ID: b.id, Name: b.name, Totals: totals(b)}, nil
}

func (f *fakeStore) CreateKassaBook(_ context.Context, userID int64, name string) (int64, error) {
	if books, _ := f.KassaBooks(context.Background(), userID); len(books) >= storage.MaxKassaBooks {
		return 0, storage.ErrKassaBooksFull
	}
	f.next++
	f.books = append(f.books, &fakeBook{id: f.next, user: userID, name: name})
	return f.next, nil
}

func (f *fakeStore) RenameKassaBook(_ context.Context, userID, bookID int64, name string) error {
	b := f.book(userID, bookID)
	if b == nil {
		return storage.ErrNoKassaBook
	}
	b.name = name
	return nil
}

func (f *fakeStore) DeleteKassaBook(_ context.Context, userID, bookID int64) error {
	for i, b := range f.books {
		if b.id == bookID && b.user == userID {
			f.books = append(f.books[:i], f.books[i+1:]...)
			return nil
		}
	}
	return storage.ErrNoKassaBook
}

func (f *fakeStore) AddKassaEntry(_ context.Context, userID, bookID int64, e storage.KassaEntry) (int64, error) {
	b := f.book(userID, bookID)
	if b == nil {
		return 0, storage.ErrNoKassaBook
	}
	f.next++
	e.ID = f.next
	b.entries = append([]storage.KassaEntry{e}, b.entries...)
	return e.ID, nil
}

func (f *fakeStore) KassaEntries(_ context.Context, userID, bookID int64, kind string, _ int) ([]storage.KassaEntry, error) {
	var list []storage.KassaEntry
	if b := f.book(userID, bookID); b != nil {
		for _, e := range b.entries {
			if e.Kind == kind {
				list = append(list, e)
			}
		}
	}
	return list, nil
}

func (f *fakeStore) DeleteKassaEntry(_ context.Context, userID, id int64) error {
	for _, b := range f.books {
		if b.user != userID {
			continue
		}
		for i, e := range b.entries {
			if e.ID == id {
				b.entries = append(b.entries[:i], b.entries[i+1:]...)
				return nil
			}
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

func balanceOf(v any) float64 {
	return v.(map[string]any)["totals"].(map[string]any)["balance"].(float64)
}

func TestKassaBooks(t *testing.T) {
	now := time.Date(2026, 11, 24, 4, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	me, them := signedInit(1, now), signedInit(2, now)

	// A user without a book is given the first one.
	code, got := post(t, h, map[string]any{"init_data": me, "action": "books"})
	books := got["books"].([]any)
	if code != http.StatusOK || len(books) != 1 || books[0].(map[string]any)["name"] != firstBook || got["today"] != "2026-11-24" {
		t.Fatalf("first books = %d %v", code, got)
	}
	home := int64(books[0].(map[string]any)["id"].(float64))

	_, got = post(t, h, map[string]any{"init_data": me, "action": "create_book", "name": "  Do'kon   kassasi "})
	books = got["books"].([]any)
	if len(books) != 2 || books[1].(map[string]any)["name"] != "Do'kon kassasi" {
		t.Fatalf("after create = %v", got)
	}
	shop := int64(books[1].(map[string]any)["id"].(float64))

	post(t, h, map[string]any{"init_data": me, "action": "add", "book": home, "kind": "in", "amount": 500000, "note": " oylik ", "day": "2026-11-20"})
	code, got = post(t, h, map[string]any{"init_data": me, "action": "add", "book": home, "kind": "out", "amount": 120000, "note": "bozor", "day": "2026-11-24"})
	if code != http.StatusOK || balanceOf(got["book"]) != 380000 {
		t.Fatalf("after add = %d %v", code, got)
	}
	if in := got["in"].([]any)[0].(map[string]any); in["note"] != "oylik" || in["day"] != "2026-11-20" {
		t.Fatalf("in entry = %v", in)
	}
	post(t, h, map[string]any{"init_data": me, "action": "add", "book": shop, "kind": "in", "amount": 20, "day": "2026-11-24"})
	if _, got := post(t, h, map[string]any{"init_data": me, "action": "books"}); balanceOf(got) != 380020 {
		t.Fatalf("all books' balance = %v", got["totals"])
	}

	for name, tc := range map[string]struct {
		body map[string]any
		want string
	}{
		"forged":         {map[string]any{"init_data": strings.Replace(me, "Bek", "Ali", 1), "action": "books"}, "forged"},
		"stale":          {map[string]any{"init_data": signedInit(1, now.Add(-48*time.Hour)), "action": "books"}, "stale"},
		"zero amount":    {map[string]any{"init_data": me, "action": "add", "book": home, "kind": "in", "amount": 0, "day": "2026-11-24"}, "amount"},
		"future day":     {map[string]any{"init_data": me, "action": "add", "book": home, "kind": "in", "amount": 5, "day": "2026-11-27"}, "day"},
		"bad day":        {map[string]any{"init_data": me, "action": "add", "book": home, "kind": "in", "amount": 5, "day": "24.11.2026"}, "day"},
		"bad kind":       {map[string]any{"init_data": me, "action": "add", "book": home, "kind": "loan", "amount": 5, "day": "2026-11-24"}, "bad_request"},
		"long note":      {map[string]any{"init_data": me, "action": "add", "book": home, "kind": "in", "amount": 5, "day": "2026-11-24", "note": strings.Repeat("a", 301)}, "note"},
		"empty name":     {map[string]any{"init_data": me, "action": "create_book", "name": "   "}, "name"},
		"long name":      {map[string]any{"init_data": me, "action": "create_book", "name": strings.Repeat("a", 41)}, "name"},
		"their book":     {map[string]any{"init_data": them, "action": "state", "book": home}, "not_found"},
		"add to theirs":  {map[string]any{"init_data": them, "action": "add", "book": home, "kind": "in", "amount": 5, "day": "2026-11-24"}, "not_found"},
		"rename theirs":  {map[string]any{"init_data": them, "action": "rename_book", "book": home, "name": "x"}, "not_found"},
		"delete theirs":  {map[string]any{"init_data": them, "action": "delete_book", "book": home}, "not_found"},
		"their entry":    {map[string]any{"init_data": them, "action": "delete", "book": home, "id": 3}, "not_found"},
		"unknown action": {map[string]any{"init_data": me, "action": "drop"}, "bad_request"},
	} {
		if _, got := post(t, h, tc.body); got["error"] != tc.want {
			t.Errorf("%s: error = %v, want %s", name, got["error"], tc.want)
		}
	}

	_, got = post(t, h, map[string]any{"init_data": me, "action": "rename_book", "book": shop, "name": "Magazin"})
	if got["book"].(map[string]any)["name"] != "Magazin" {
		t.Fatalf("after rename = %v", got)
	}
	_, got = post(t, h, map[string]any{"init_data": me, "action": "delete_book", "book": shop})
	if books := got["books"].([]any); len(books) != 1 || balanceOf(got) != 380000 {
		t.Fatalf("after deleting a book = %v", got)
	}

	for i := 1; i < storage.MaxKassaBooks; i++ {
		post(t, h, map[string]any{"init_data": me, "action": "create_book", "name": fmt.Sprint("k", i)})
	}
	if _, got := post(t, h, map[string]any{"init_data": me, "action": "create_book", "name": "one too many"}); got["error"] != "books_full" {
		t.Errorf("book past the limit: %v, want books_full", got["error"])
	}
}

// A press of Play on the "kassa" game opens the page without init data:
// its session says whose books they are, and another game's session does not.
func TestKassaGameSession(t *testing.T) {
	now := time.Date(2026, 11, 24, 4, 0, 0, 0, time.UTC)
	games := game.New(testToken, "https://example.com/game/", func() time.Time { return now })
	store := &fakeStore{}
	store.books = []*fakeBook{{id: 9, user: 7, name: "Uy", entries: []storage.KassaEntry{{ID: 1, Kind: storage.KassaIn, Amount: 900}}}}
	h := NewHandler(Options{BotToken: testToken, Store: store, Sessions: games, Now: func() time.Time { return now }})
	session := func(name string) string {
		link, ok := games.PlayURL(name, game.Player{UserID: 7, ChatID: 7, MessageID: 1})
		if !ok {
			t.Fatalf("no page for %s", name)
		}
		u, _ := url.Parse(link)
		return u.Query().Get("s")
	}

	code, got := post(t, h, map[string]any{"session": session(game.KassaShortName), "action": "state", "book": 9})
	if code != http.StatusOK || balanceOf(got["book"]) != 900 {
		t.Fatalf("kassa session = %d %v", code, got)
	}
	if _, got := post(t, h, map[string]any{"session": session(game.MafiaShortName), "action": "books"}); got["error"] != "forged" {
		t.Errorf("mafia's session: %v, want forged", got["error"])
	}
	if _, got := post(t, h, map[string]any{"session": "made.up", "action": "books"}); got["error"] != "stale" {
		t.Errorf("a made-up session: %v, want stale", got["error"])
	}
}
