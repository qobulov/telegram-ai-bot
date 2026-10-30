package raid

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeStore answers as told and keeps what it was asked.
type fakeStore struct {
	hero     storage.RaidHero
	hit      storage.RaidHit
	upgrade  error
	hitChats []int64
	taps     []int
}

func (f *fakeStore) RaidState(_ context.Context, chatID, userID int64, _ storage.RaidRules, seed uint32, _ int) (storage.RaidBoss, storage.RaidHero, storage.RaidMember, []storage.RaidMember, error) {
	f.hero.UserID, f.hero.Seed = userID, seed
	return storage.RaidBoss{ChatID: chatID, Level: 3, HP: 50, MaxHP: 100}, f.hero,
		storage.RaidMember{UserID: userID}, []storage.RaidMember{{UserID: userID, Name: "Bek", Damage: 40}}, nil
}

func (f *fakeStore) HitRaid(_ context.Context, chatID, _ int64, _ string, taps int, _ time.Time, _ storage.RaidRules, _ uint32) (storage.RaidHit, error) {
	f.hitChats = append(f.hitChats, chatID)
	f.taps = append(f.taps, taps)
	return f.hit, nil
}

func (f *fakeStore) RaidRanks(context.Context, int64, int64, int, int) (int, int, []storage.RaidMember, error) {
	return 2, 3, []storage.RaidMember{{UserID: 9, Name: "Ali", Total: 2_000_000}, {UserID: 7, Name: "Bek", Total: 50_000}}, nil
}

func (f *fakeStore) ChangeRaidHero(_ context.Context, userID int64, seed uint32, change func(*storage.RaidHero) error) (storage.RaidHero, error) {
	h := f.hero
	h.UserID, h.Seed = userID, seed
	if err := change(&h); err != nil {
		return storage.RaidHero{}, err
	}
	f.hero = h
	return h, nil
}

func (f *fakeStore) UpgradeRaidHero(context.Context, int64, string, func(int) int64, int, uint32) (storage.RaidHero, error) {
	return storage.RaidHero{Sword: 2}, f.upgrade
}

// fakeTelegram keeps the scores set and the messages sent.
type fakeTelegram struct {
	scores []int
	sent   []telegram.Outgoing
}

func (f *fakeTelegram) SetGameScore(_ context.Context, _ telegram.GameMessage, score int) (bool, error) {
	f.scores = append(f.scores, score)
	return true, nil
}

func (f *fakeTelegram) SendMessage(_ context.Context, msg telegram.Outgoing) (int64, error) {
	f.sent = append(f.sent, msg)
	return 1, nil
}

type fixture struct {
	games    *game.Game
	store    *fakeStore
	telegram *fakeTelegram
	handler  http.Handler
}

func newFixture() *fixture {
	f := &fixture{games: game.New("1:test", "https://example.com/game/", nil), store: &fakeStore{hero: storage.RaidHero{Sword: 1}}, telegram: &fakeTelegram{}}
	f.handler = NewHandler(Options{
		Sessions: f.games, Store: f.store, Telegram: f.telegram,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Seed: func() uint32 { return 42 },
	})
	return f
}

// session is the session a press of shortName's Play gives player.
func (f *fixture) session(t *testing.T, shortName string, player game.Player) string {
	t.Helper()
	link, _ := f.games.PlayURL(shortName, player)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("s")
}

func (f *fixture) post(t *testing.T, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp := httptest.NewRecorder()
	f.handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/raid", strings.NewReader(string(raw))))
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	return resp.Code, out
}

var member = game.Player{UserID: 7, Name: "Bek", ChatID: -100200, MessageID: 5}

// Only Dev jangi's sessions open the raid, and the state is the chat's
// boss with the player's hero.
func TestRaidWantsItsOwnSession(t *testing.T) {
	f := newFixture()
	if code, out := f.post(t, map[string]any{"session": f.session(t, game.ShortName, member), "action": "state"}); code != http.StatusUnauthorized || out["error"] != "session" {
		t.Fatalf("Tarozi's session got %d %v", code, out)
	}
	code, out := f.post(t, map[string]any{"session": f.session(t, game.BossShortName, member), "action": "state"})
	boss, _ := out["boss"].(map[string]any)
	hero, _ := out["hero"].(map[string]any)
	if code != http.StatusOK || boss["level"] != 3.0 || boss["kind"] != 2.0 || hero["seed"] != 42.0 || hero["tap_damage"] != 5.0 {
		t.Fatalf("state = %d %v", code, out)
	}
	rank, _ := out["rank"].(map[string]any)
	total, _ := out["top_total"].([]any)
	first, _ := total[0].(map[string]any)
	if rank["level"] != 2.0 || rank["total"] != 3.0 || len(total) != 2 || first["title"] != 3.0 {
		t.Errorf("ranks = %v, top in all = %v", rank, total)
	}
}

// A hit that kills a group's boss is told in the group, with who hit it
// most and their rewards; the member's total is set as their score when
// due.
func TestKillIsAnnouncedInTheGroup(t *testing.T) {
	f := newFixture()
	f.store.hit = storage.RaidHit{
		Boss: storage.RaidBoss{Level: 4, HP: 100, MaxHP: 100}, Member: storage.RaidMember{Total: 1234}, Damage: 30, Counted: 3, ScoreDue: true,
		Killed: &storage.RaidKill{Level: 3, MaxHP: 1000, Fighters: []storage.RaidFighter{
			{RaidMember: storage.RaidMember{UserID: 9, Name: "Ali*", Damage: 52300}, Reward: 700},
			{RaidMember: storage.RaidMember{UserID: 7, Name: "Bek", Damage: 900}, Reward: 80},
		}},
	}
	code, out := f.post(t, map[string]any{"session": f.session(t, game.BossShortName, member), "action": "hit", "taps": 3})
	killed, _ := out["killed"].(map[string]any)
	if code != http.StatusOK || killed["reward"] != 80.0 || f.store.taps[0] != 3 || f.store.hitChats[0] != member.ChatID {
		t.Fatalf("hit = %d %v", code, out)
	}
	if len(f.telegram.scores) != 1 || f.telegram.scores[0] != 1234 {
		t.Errorf("scores set = %v", f.telegram.scores)
	}
	if len(f.telegram.sent) != 1 || f.telegram.sent[0].ChatID != member.ChatID {
		t.Fatalf("sent = %+v", f.telegram.sent)
	}
	text := f.telegram.sent[0].Text
	for _, want := range []string{"Bir ko'zli dev (3-daraja) yengildi", "🥇 Ali — 52k zarba (+700 💰)", "🥈 Bek", "Ajdar (4-daraja)", "/boss"} {
		if !strings.Contains(text, want) {
			t.Errorf("the announcement lacks %q:\n%s", want, text)
		}
	}
}

// A boss fought alone, in a private chat or from an inline game, is no one
// else's to be told of.
func TestKillIsNotAnnouncedAlone(t *testing.T) {
	f := newFixture()
	f.store.hit = storage.RaidHit{Killed: &storage.RaidKill{Level: 1}}
	for _, p := range []game.Player{{UserID: 7, ChatID: 7, MessageID: 5}, {UserID: 7, InlineMessageID: "inl"}} {
		if code, _ := f.post(t, map[string]any{"session": f.session(t, game.BossShortName, p), "action": "hit", "taps": 1}); code != http.StatusOK {
			t.Fatalf("hit = %d", code)
		}
	}
	if len(f.telegram.sent) != 0 || f.store.hitChats[1] != 7 {
		t.Errorf("sent %+v; the inline player's chat was %d", f.telegram.sent, f.store.hitChats[1])
	}
}

// An upgrade without the coins, or of something that is not one, is
// refused.
func TestUpgradeRefusals(t *testing.T) {
	f := newFixture()
	session := f.session(t, game.BossShortName, member)
	f.store.upgrade = storage.ErrRaidCoins
	if code, out := f.post(t, map[string]any{"session": session, "action": "upgrade", "item": "sword"}); code != http.StatusConflict || out["error"] != "coins" {
		t.Errorf("without coins: %d %v", code, out)
	}
	if code, out := f.post(t, map[string]any{"session": session, "action": "upgrade", "item": "coins"}); code != http.StatusBadRequest {
		t.Errorf("an unknown upgrade: %d %v", code, out)
	}
	f.store.upgrade = nil
	if code, out := f.post(t, map[string]any{"session": session, "action": "upgrade", "item": "sword"}); code != http.StatusOK || out["hero"] == nil {
		t.Errorf("an upgrade: %d %v", code, out)
	}
}

// A skill used and a quest claimed change the hero; one not ready, or not
// done, is refused; the day's first visit brings the daily gift.
func TestSkillsQuestsAndTheDailyGift(t *testing.T) {
	f := newFixture()
	session := f.session(t, game.BossShortName, member)
	code, out := f.post(t, map[string]any{"session": session, "action": "state"})
	if code != http.StatusOK || out["gift"] != 50.0 {
		t.Fatalf("the first visit: %d gift %v", code, out["gift"])
	}
	if _, out := f.post(t, map[string]any{"session": session, "action": "state"}); out["gift"] != 0.0 {
		t.Errorf("the second visit brought a gift of %v", out["gift"])
	}
	code, out = f.post(t, map[string]any{"session": session, "action": "skill", "skill": "storm"})
	skills, _ := out["hero"].(map[string]any)["skills"].(map[string]any)
	storm, _ := skills["storm"].(map[string]any)
	if code != http.StatusOK || storm["active_for"].(float64) <= 0 || storm["ready_in"].(float64) <= 0 {
		t.Fatalf("the storm: %d %v", code, out)
	}
	if code, out := f.post(t, map[string]any{"session": session, "action": "skill", "skill": "storm"}); code != http.StatusConflict || out["error"] != "cooldown" {
		t.Errorf("the storm again: %d %v", code, out)
	}
	if code, out := f.post(t, map[string]any{"session": session, "action": "skill", "skill": "rain"}); out["error"] != "no_helper" {
		t.Errorf("rain without Barchinoy: %d %v", code, out)
	}
	if code, out := f.post(t, map[string]any{"session": session, "action": "claim", "quest": 2}); out["error"] != "not_done" {
		t.Errorf("an undone quest: %d %v", code, out)
	}
}
