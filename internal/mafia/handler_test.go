package mafia

import (
	"context"
	"encoding/json"
	"fmt"
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

// fakeTelegram records what the group is told and whom the bot restricts:
// "mute 3" or "unmute 3". Members are "member" unless statuses says
// otherwise; with refuse, the bot may restrict no one.
type fakeTelegram struct {
	sent       []string
	restricted []string
	statuses   map[int64]string
	refuse     bool
}

func (f *fakeTelegram) SendMessage(_ context.Context, m telegram.Outgoing) (int64, error) {
	f.sent = append(f.sent, m.Text)
	return 1, nil
}

func (f *fakeTelegram) GetChatMember(_ context.Context, _, userID int64) (telegram.ChatMember, error) {
	if status, ok := f.statuses[userID]; ok {
		return telegram.ChatMember{Status: status}, nil
	}
	return telegram.ChatMember{Status: "member"}, nil
}

func (f *fakeTelegram) RestrictChatMember(_ context.Context, r telegram.Restriction) error {
	if f.refuse {
		return &telegram.APIError{Code: 400, Description: "Bad Request: not enough rights to restrict/unrestrict chat member"}
	}
	what := "unmute"
	if r.Permissions == telegram.NoPermissions() {
		what = "mute"
	}
	f.restricted = append(f.restricted, fmt.Sprintf("%s %d", what, r.UserID))
	return nil
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

// The night's victim leaves a last word: the group is told it, every page
// shows it, and it cannot be said twice.
func TestHandlerTellsTheLastWord(t *testing.T) {
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
	victim := byRole[Commissar]
	f.post(t, map[string]any{"session": sessions[byRole[Don]], "action": "act", "act": actKill, "target": victim})
	f.now = f.now.Add(nightFor)
	_, view := f.post(t, map[string]any{"session": sessions[victim], "action": "state"})
	if me := view["me"].(map[string]any); me["can_speak"] != true || view["max_words"] != float64(MaxWords) {
		t.Fatalf("the victim's page: %v", view)
	}
	code, view := f.post(t, map[string]any{"session": sessions[victim], "action": "speak", "words": "Don — mafiya!"})
	if code != http.StatusOK || !strings.Contains(f.tg.sent[len(f.tg.sent)-1], "«Don — mafiya!»") {
		t.Fatalf("speak: %d, told %q", code, f.tg.sent)
	}
	for _, row := range view["players"].([]any) {
		if p := row.(map[string]any); int64(p["id"].(float64)) == victim && p["words"] != "Don — mafiya!" {
			t.Errorf("the word on the page: %v", p)
		}
	}
	if code, got := f.post(t, map[string]any{"session": sessions[victim], "action": "speak", "words": "yana"}); code != http.StatusConflict || got["error"] != "no_words" {
		t.Errorf("spoke twice: %d %v", code, got)
	}
}

// The fallen are kept from writing in the group till the game ends, but
// not admins; the end lets those it muted write again. A bot that may not
// restrict says so once a game.
func TestHandlerMutesTheFallenTillTheEnd(t *testing.T) {
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
	admin := byRole[Doctor]
	f.tg.statuses = map[int64]string{admin: "administrator"}
	f.post(t, map[string]any{"session": sessions[byRole[Don]], "action": "act", "act": actKill, "target": byRole[Commissar]})
	f.now = f.now.Add(nightFor)
	f.post(t, map[string]any{"session": sessions[1], "action": "state"})
	if want := fmt.Sprintf("mute %d", byRole[Commissar]); len(f.tg.restricted) != 1 || f.tg.restricted[0] != want {
		t.Fatalf("after the night restricted %v, want %s", f.tg.restricted, want)
	}
	if told := strings.Join(f.tg.sent, "\n"); !strings.Contains(told, "o'yin tugaguncha guruhda yoza olmaydi") {
		t.Errorf("the group was told %q", told)
	}
	_ = json.Unmarshal(f.store.games[group], &g)
	if !g.player(byRole[Commissar]).Muted {
		t.Error("the mute is not noted in the game")
	}

	// The doctor, an admin, is voted out; then the Don, which ends it.
	f.now = f.now.Add(dayFor)
	for id, s := range sessions {
		if id != byRole[Commissar] && id != admin {
			f.post(t, map[string]any{"session": s, "action": "vote", "target": admin})
		}
	}
	f.now = f.now.Add(voteFor)
	f.post(t, map[string]any{"session": sessions[1], "action": "state"})
	if len(f.tg.restricted) != 1 {
		t.Fatalf("an admin was restricted: %v", f.tg.restricted)
	}
	f.now = f.now.Add(nightFor)
	f.post(t, map[string]any{"session": sessions[1], "action": "state"})
	f.now = f.now.Add(dayFor)
	f.post(t, map[string]any{"session": sessions[1], "action": "state"})
	for id, s := range sessions {
		if id != byRole[Commissar] && id != admin && id != byRole[Don] {
			f.post(t, map[string]any{"session": s, "action": "vote", "target": byRole[Don]})
		}
	}
	f.post(t, map[string]any{"session": sessions[byRole[Don]], "action": "vote", "target": byRole[Citizen]})
	_ = json.Unmarshal(f.store.games[group], &g)
	if g.Phase != Over {
		t.Fatalf("the game goes on: %s, %v", g.Phase, f.tg.sent[len(f.tg.sent)-1])
	}
	if want := fmt.Sprintf("unmute %d", byRole[Commissar]); f.tg.restricted[len(f.tg.restricted)-1] != want || len(f.tg.restricted) != 2 {
		t.Errorf("at the end restricted %v, want the commissar let write again only", f.tg.restricted)
	}

	refused := newFixture()
	refused.tg.refuse = true
	for id := int64(1); id <= 5; id++ {
		sessions[id] = refused.session(t, id, group)
		refused.post(t, map[string]any{"session": sessions[id], "action": "join"})
	}
	refused.post(t, map[string]any{"session": sessions[1], "action": "start"})
	refused.post(t, map[string]any{"session": sessions[byRole[Don]], "action": "act", "act": actKill, "target": byRole[Commissar]})
	refused.now = refused.now.Add(nightFor)
	refused.post(t, map[string]any{"session": sessions[1], "action": "state"})
	refused.now = refused.now.Add(dayFor)
	refused.post(t, map[string]any{"session": sessions[1], "action": "state"})
	for id, s := range sessions {
		if id != byRole[Commissar] {
			refused.post(t, map[string]any{"session": s, "action": "vote", "target": byRole[Doctor]})
		}
	}
	if warnings := strings.Count(strings.Join(refused.tg.sent, "\n"), "huquqini bering"); warnings != 1 {
		t.Errorf("after two deaths warned %d times, want once: %q", warnings, refused.tg.sent)
	}
}

// A page writes in the chat open to its player, and every page that may
// read it gets the line; a closed chat is refused.
func TestHandlerChat(t *testing.T) {
	f := newFixture()
	sessions := map[int64]string{}
	for id := int64(1); id <= 5; id++ {
		sessions[id] = f.session(t, id, group)
		f.post(t, map[string]any{"session": sessions[id], "action": "join"})
	}
	if code, got := f.post(t, map[string]any{"session": sessions[2], "action": "chat", "room": RoomTown, "text": "tez boshlaylik"}); code != http.StatusOK || got["me"].(map[string]any)["room"] != RoomTown {
		t.Fatalf("lobby chat: %d %v", code, got)
	}
	_, view := f.post(t, map[string]any{"session": sessions[4], "action": "state"})
	if chat := view["chat"].([]any); len(chat) != 1 || chat[0].(map[string]any)["text"] != "tez boshlaylik" {
		t.Errorf("the lobby heard %v", view["chat"])
	}
	f.post(t, map[string]any{"session": sessions[1], "action": "start"})
	var g Game
	_ = json.Unmarshal(f.store.games[group], &g)
	for _, p := range g.Players {
		if TeamOf(p.Role) != Mafia {
			if code, got := f.post(t, map[string]any{"session": sessions[p.ID], "action": "chat", "room": RoomTown, "text": "salom"}); code != http.StatusConflict || got["error"] != "chat_closed" {
				t.Errorf("a townsman wrote at night: %d %v", code, got)
			}
			break
		}
	}
}
