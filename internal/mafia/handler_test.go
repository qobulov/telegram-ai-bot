package mafia

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
	mu    sync.Mutex
	games map[int64][]byte
	paid  map[int64]int64
}

func (f *fakeStore) ChangeMafiaGame(_ context.Context, chatID int64, change func([]byte) ([]byte, error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.games[chatID]
	if state == nil {
		state = []byte("{}")
	}
	next, err := change(state)
	if err != nil {
		return err
	}
	f.games[chatID] = next
	return nil
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

const group = int64(-100500)

type fixture struct {
	h     http.Handler
	games *game.Game
	store *fakeStore
	tg    *fakeTelegram
	now   time.Time
}

func newFixture() *fixture {
	f := &fixture{games: game.New("1:test", "https://example.com/game/", nil), store: &fakeStore{games: map[int64][]byte{}, paid: map[int64]int64{}}, tg: &fakeTelegram{}, now: t0}
	f.h = NewHandler(Options{Sessions: f.games, Store: f.store, Telegram: f.tg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return f.now }, Intn: func(int) int { return 0 }})
	return f
}

func (f *fixture) session(t *testing.T, user, chat int64) string {
	t.Helper()
	link, ok := f.games.PlayURL(game.MafiaShortName, game.Player{UserID: user, Name: "P" + string(rune('0'+user)), ChatID: chat, MessageID: 9})
	if !ok || !strings.HasPrefix(link, "https://example.com/game/mafiya/?s=") {
		t.Fatalf("link %q", link)
	}
	u, _ := url.Parse(link)
	return u.Query().Get("s")
}

func (f *fixture) post(t *testing.T, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/mafia", strings.NewReader(string(raw))))
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%q: %v", rec.Body.String(), err)
	}
	return rec.Code, got
}

// A group's members join and start a game; each page is told its own role
// and no one else's, the mafia their partners; a private chat cannot play.
func TestHandlerPlaysAGame(t *testing.T) {
	f := newFixture()
	if code, got := f.post(t, map[string]any{"session": f.session(t, 1, 7), "action": "join"}); code != http.StatusConflict || got["error"] != "group" {
		t.Errorf("a private chat: %d %v", code, got)
	}
	sessions := map[int64]string{}
	for id := int64(1); id <= 6; id++ {
		sessions[id] = f.session(t, id, group)
		if code, got := f.post(t, map[string]any{"session": sessions[id], "action": "join"}); code != http.StatusOK || got["phase"] != "lobby" {
			t.Fatalf("join %d: %d %v", id, code, got)
		}
	}
	if len(f.tg.sent) != 1 || !strings.Contains(f.tg.sent[0], "xonasini ochdi") {
		t.Errorf("the group was told %q", f.tg.sent)
	}
	code, got := f.post(t, map[string]any{"session": sessions[3], "action": "start"})
	if code != http.StatusOK || got["phase"] != "night" {
		t.Fatalf("start: %d %v", code, got)
	}
	var g Game
	_ = json.Unmarshal(f.store.games[group], &g)
	roles := map[int64]string{}
	for _, p := range g.Players {
		roles[p.ID] = p.Role
	}
	for id, s := range sessions {
		_, view := f.post(t, map[string]any{"session": s, "action": "state"})
		me := view["me"].(map[string]any)
		if me["role"] != roles[id] {
			t.Errorf("player %d sees their role as %v, is %s", id, me["role"], roles[id])
		}
		for _, row := range view["players"].([]any) {
			p := row.(map[string]any)
			other := int64(p["id"].(float64))
			seen, shown := p["role"]
			partners := TeamOf(roles[id]) == Mafia && TeamOf(roles[other]) == Mafia
			if other != id && shown && !partners {
				t.Errorf("player %d sees %d's role %v", id, other, seen)
			}
			if partners && !shown {
				t.Errorf("mafia %d does not see partner %d", id, other)
			}
			if !strings.HasPrefix(p["avatar"].(string), "/api/game?avatar=") {
				t.Errorf("no photo for %d", other)
			}
		}
	}
	if code, got := f.post(t, map[string]any{"session": f.session(t, 9, group), "action": "join"}); code != http.StatusConflict || got["error"] != "started" {
		t.Errorf("joined a started game: %d %v", code, got)
	}
}

// The town wins by voting out its killers; the group is told, the roles
// shown, and the winners paid.
func TestHandlerEndsAndPays(t *testing.T) {
	f := newFixture()
	sessions := map[int64]string{}
	for id := int64(1); id <= 5; id++ {
		sessions[id] = f.session(t, id, group)
		f.post(t, map[string]any{"session": sessions[id], "action": "join"})
	}
	f.post(t, map[string]any{"session": sessions[1], "action": "start"})
	var g Game
	_ = json.Unmarshal(f.store.games[group], &g)
	var don int64
	for _, p := range g.Players {
		if p.Role == Don {
			don = p.ID
		}
	}
	// A phase begins when a request finds the last one's time up.
	f.now = f.now.Add(nightFor)
	f.post(t, map[string]any{"session": sessions[1], "action": "state"})
	f.now = f.now.Add(dayFor)
	for id, s := range sessions {
		target := don
		if id == don {
			target = 1 + id%5
		}
		if code, got := f.post(t, map[string]any{"session": s, "action": "vote", "target": target}); code != http.StatusOK {
			t.Fatalf("vote %d: %d %v", id, code, got)
		}
	}
	_, view := f.post(t, map[string]any{"session": sessions[1], "action": "state"})
	if view["phase"] != "over" || view["winner"] != "town" {
		t.Fatalf("after the vote: %v %v", view["phase"], view["winner"])
	}
	for _, row := range view["players"].([]any) {
		if _, shown := row.(map[string]any)["role"]; !shown {
			t.Errorf("a role hidden at the end: %v", row)
		}
	}
	last := f.tg.sent[len(f.tg.sent)-1]
	if !strings.Contains(last, "Tinch aholi yutdi") || !strings.Contains(last, "+150") {
		t.Errorf("the group was told %q", last)
	}
	for id := int64(1); id <= 5; id++ {
		if want := map[bool]int64{true: 0, false: prize}[id == don]; f.store.paid[id] != want {
			t.Errorf("player %d was paid %d, want %d", id, f.store.paid[id], want)
		}
	}
}

// A game kept as JSON between requests, its empty maps dropped, still takes
// the night's acts, the checks and the votes.
func TestHandlerKeepsTheGameBetweenRequests(t *testing.T) {
	f := newFixture()
	sessions := map[int64]string{}
	for id := int64(1); id <= 5; id++ {
		sessions[id] = f.session(t, id, group)
		f.post(t, map[string]any{"session": sessions[id], "action": "join"})
	}
	f.post(t, map[string]any{"session": sessions[1], "action": "start"})
	var g Game
	_ = json.Unmarshal(f.store.games[group], &g)
	byRole := map[string]int64{}
	for _, p := range g.Players {
		byRole[p.Role] = p.ID
	}
	acts := []struct {
		role, act string
		target    int64
	}{{Commissar, actCheck, byRole[Don]}, {Doctor, actHeal, byRole[Doctor]}, {Don, actKill, byRole[Citizen]}, {Don, actFind, byRole[Commissar]}}
	for _, a := range acts {
		if code, got := f.post(t, map[string]any{"session": sessions[byRole[a.role]], "action": "act", "act": a.act, "target": a.target}); code != http.StatusOK {
			t.Fatalf("%s %s: %d %v", a.role, a.act, code, got)
		}
	}
	_, view := f.post(t, map[string]any{"session": sessions[byRole[Commissar]], "action": "state"})
	if view["phase"] != "day" {
		t.Fatalf("after everyone acted: %v", view["phase"])
	}
	for _, row := range view["players"].([]any) {
		p := row.(map[string]any)
		if int64(p["id"].(float64)) == byRole[Don] && p["mark"] != "mafia" {
			t.Errorf("the commissar's check of the Don: %v", p)
		}
	}
}
