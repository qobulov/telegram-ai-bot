package maydon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/telegram"
)

type fakeStore struct {
	mu     sync.Mutex
	rooms  map[int64][]byte
	levels map[int64][2]int
	paid   map[int64]int64
}

func (f *fakeStore) ChangeMaydonRoom(_ context.Context, chatID int64, change func([]byte) ([]byte, error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.rooms[chatID]
	if state == nil {
		state = []byte("{}")
	}
	next, err := change(state)
	if err != nil {
		return err
	}
	f.rooms[chatID] = next
	return nil
}

func (f *fakeStore) RaidLevels(_ context.Context, userID int64) (int, int, error) {
	if l, ok := f.levels[userID]; ok {
		return l[0], l[1], nil
	}
	return 1, 0, nil
}

func (f *fakeStore) AddRaidCoins(_ context.Context, users []int64, coins int64) error {
	for _, u := range users {
		f.paid[u] += coins
	}
	return nil
}

type fakeTelegram struct{ sent []string }

func (f *fakeTelegram) SendMessage(_ context.Context, m telegram.Outgoing) (int64, error) {
	f.sent = append(f.sent, m.Text)
	return 1, nil
}

const group = int64(-100600)

type fixture struct {
	h     http.Handler
	games *game.Game
	store *fakeStore
	tg    *fakeTelegram
	now   time.Time
}

func newFixture() *fixture {
	f := &fixture{games: game.New("1:test", "https://example.com/game/", nil), tg: &fakeTelegram{}, now: t0,
		store: &fakeStore{rooms: map[int64][]byte{}, levels: map[int64][2]int{1: {20, 15}}, paid: map[int64]int64{}}}
	f.h = NewHandler(Options{Sessions: f.games, Store: f.store, Telegram: f.tg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return f.now }, Seed: func() uint32 { return 7 }})
	return f
}

func (f *fixture) session(t *testing.T, user, chat int64) string {
	t.Helper()
	link, ok := f.games.PlayURL(ShortName, game.Player{UserID: user, Name: "P" + string(rune('0'+user)), ChatID: chat, MessageID: 9})
	if !ok || !strings.HasPrefix(link, "https://example.com/game/maydon/?s=") {
		t.Fatalf("link %q", link)
	}
	u, _ := url.Parse(link)
	return u.Query().Get("s")
}

func (f *fixture) post(t *testing.T, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/maydon", strings.NewReader(string(raw))))
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%q: %v", rec.Body.String(), err)
	}
	return rec.Code, got
}

// Members join with their Dev jangi levels and start a battle; a blow on
// time comes back as a hit; the battle ends, the group is told and the
// winners paid; a private chat cannot fight.
func TestHandlerFightsABattle(t *testing.T) {
	f := newFixture()
	if code, got := f.post(t, map[string]any{"session": f.session(t, 1, 5), "action": "join", "class": Archer}); code != http.StatusConflict || got["error"] != "group" {
		t.Errorf("a private chat: %d %v", code, got)
	}
	s1, s2 := f.session(t, 1, group), f.session(t, 2, group)
	f.post(t, map[string]any{"session": s1, "action": "join", "class": Archer})
	if code, got := f.post(t, map[string]any{"session": s2, "action": "join", "class": "wizard"}); code != http.StatusConflict || got["error"] != "class" {
		t.Errorf("a wrong class: %d %v", code, got)
	}
	f.post(t, map[string]any{"session": s2, "action": "join", "class": Warrior})
	var r Room
	_ = json.Unmarshal(f.store.rooms[group], &r)
	if r.fighter(1).Sword != 20 || r.fighter(1).Crit != 15 || r.fighter(2).Sword != 1 {
		t.Fatalf("levels: %+v", r.Fighters)
	}
	code, got := f.post(t, map[string]any{"session": s2, "action": "start"})
	if code != http.StatusOK || got["phase"] != "battle" || len(f.tg.sent) != 2 || !strings.Contains(f.tg.sent[1], "Maydon boshlandi") {
		t.Fatalf("start: %d %v, told %q", code, got["phase"], f.tg.sent)
	}
	if code, got := f.post(t, map[string]any{"session": s1, "action": "attack"}); code != http.StatusConflict || got["error"] != "early" {
		t.Errorf("an early blow: %d %v", code, got["error"])
	}
	f.now = f.now.Add(900 * time.Millisecond)
	code, got = f.post(t, map[string]any{"session": s1, "action": "attack", "since": 0})
	hits := got["hits"].([]any)
	if code != http.StatusOK || len(hits) == 0 || hits[len(hits)-1].(map[string]any)["perfect"] != true {
		t.Fatalf("a blow on time: %d %v", code, got)
	}
	me := got["me"].(map[string]any)
	if me["id"] != 1.0 || me["joined"] != true {
		t.Errorf("me: %v", me)
	}
	f.now = f.now.Add(battleMs * time.Millisecond)
	_, got = f.post(t, map[string]any{"session": s2, "action": "state"})
	if got["phase"] != "over" {
		t.Fatalf("after the time: %v", got["phase"])
	}
	last := f.tg.sent[len(f.tg.sent)-1]
	if !strings.Contains(last, "Maydon:") || !strings.Contains(last, "Jang qahramoni") {
		t.Errorf("told %q", last)
	}
	winner := int(got["winner"].(float64))
	var r2 Room
	_ = json.Unmarshal(f.store.rooms[group], &r2)
	for _, fr := range r2.Fighters {
		want := int64(0)
		if fr.Team == winner {
			want = prize
		}
		if f.store.paid[fr.ID] != want {
			t.Errorf("fighter %d was paid %d, want %d", fr.ID, f.store.paid[fr.ID], want)
		}
	}
}
