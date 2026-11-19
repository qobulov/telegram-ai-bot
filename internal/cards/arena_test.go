package cards

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// fakeArena keeps the arena of fakeStore: the cards each player holds, their
// saved teams, the attacks made and the battles fought.
type fakeArena struct {
	held    map[int64][]storage.OwnedCard
	teams   map[int64][5]int64
	power   map[int64]int
	attacks map[int64]int
	battles map[int64][]storage.ArenaBattle // newest last
	rules   []storage.ArenaRules            // the rules each battle was recorded by
}

func (f *fakeStore) SaveArenaTeam(_ context.Context, userID int64, front [2]int64, back [3]int64, power int) error {
	ids := [5]int64{front[0], front[1], back[0], back[1], back[2]}
	held := map[int64]bool{}
	for _, o := range f.arena.held[userID] {
		held[o.ID] = o.Count > 0
	}
	for _, id := range ids {
		if !held[id] {
			return storage.ErrCardNotHeld
		}
	}
	if f.arena.teams == nil {
		f.arena.teams, f.arena.power = map[int64][5]int64{}, map[int64]int{}
	}
	f.arena.teams[userID], f.arena.power[userID] = ids, power
	return nil
}

func (f *fakeStore) ArenaTeam(_ context.Context, userID int64) ([2]int64, [3]int64, bool, error) {
	ids, ok := f.arena.teams[userID]
	return [2]int64{ids[0], ids[1]}, [3]int64{ids[2], ids[3], ids[4]}, ok, nil
}

func (f *fakeStore) ArenaPlayerOf(_ context.Context, userID int64, rules storage.ArenaRules) (storage.ArenaPlayer, error) {
	return storage.ArenaPlayer{UserID: userID, Name: "Bek", Rating: 1000, Tickets: rules.Tickets - f.arena.attacks[userID], TokensLeft: rules.Tokens, Power: f.arena.power[userID]}, nil
}

func (f *fakeStore) ArenaOpponents(_ context.Context, userID int64, n int) ([]storage.ArenaPlayer, error) {
	var out []storage.ArenaPlayer
	for id, power := range f.arena.power {
		if id != userID && len(out) < n {
			out = append(out, storage.ArenaPlayer{UserID: id, Name: "Ali", Rating: 1000, Power: power})
		}
	}
	return out, nil
}

func (f *fakeStore) RecordArenaBattle(_ context.Context, attacker, defender, _ int64, won bool, rules storage.ArenaRules) (storage.ArenaOutcome, error) {
	if attacker == defender {
		return storage.ArenaOutcome{}, errors.New("attacking oneself")
	}
	if f.arena.attacks[attacker] >= rules.Tickets {
		return storage.ArenaOutcome{}, storage.ErrArenaNoTickets
	}
	if f.arena.attacks == nil {
		f.arena.attacks, f.arena.battles = map[int64]int{}, map[int64][]storage.ArenaBattle{}
	}
	f.arena.attacks[attacker]++
	f.arena.rules = append(f.arena.rules, rules)
	change := -16
	if won {
		change = 16
	}
	f.arena.battles[attacker] = append(f.arena.battles[attacker], storage.ArenaBattle{At: rules.Now, Attacked: true, OpponentID: defender, Opponent: "Ali", Won: won, RatingChange: change})
	f.arena.battles[defender] = append(f.arena.battles[defender], storage.ArenaBattle{At: rules.Now, OpponentID: attacker, Opponent: "Bek", Won: !won, RatingChange: -change})
	return storage.ArenaOutcome{RatingChange: change, Token: won, Tickets: rules.Tickets - f.arena.attacks[attacker], TokensLeft: rules.Tokens, NextTicket: rules.TicketEvery}, nil
}

func (f *fakeStore) ArenaBattles(_ context.Context, userID int64, limit int) ([]storage.ArenaBattle, error) {
	mine := f.arena.battles[userID]
	var out []storage.ArenaBattle
	for i := len(mine) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, mine[i])
	}
	return out, nil
}

func (f *fakeStore) ArenaTop(context.Context, int) ([]storage.ArenaPlayer, error) {
	return []storage.ArenaPlayer{{UserID: 7, Name: "Bek", Rating: 1016, Wins: 1, Power: 100}}, nil
}

// arenaCards are five cards, a Mythic among them, each held at ★2 by the
// players 7 and 8.
func arenaCards() []storage.OwnedCard {
	cards := make([]storage.OwnedCard, 5)
	for i := range cards {
		c := storage.Card{ID: int64(10 + i), Name: "Hero", ImageURL: "https://img/h.png", Rarity: i % 5, Attack: 1000, Health: 3000 + 1000*i}
		cards[i] = storage.OwnedCard{Card: c, Count: 1, Stars: 2}
	}
	return cards
}

func arenaHandler(t *testing.T, store *fakeStore, now time.Time) func(user int64, fields map[string]any) (int, map[string]any) {
	t.Helper()
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	return func(user int64, fields map[string]any) (int, map[string]any) {
		fields["init_data"] = signedInit(user, now)
		body, _ := json.Marshal(fields)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
}

var (
	arenaFront = []int64{10, 11}
	arenaBack  = []int64{12, 13, 14}
)

// A team of five cards held is saved with its power, worked out from the
// cards and their stars; a team of the wrong size, a card twice or a card
// not held is refused with the reason.
func TestArenaTeamIsSavedWithItsPower(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{arena: fakeArena{held: map[int64][]storage.OwnedCard{7: arenaCards()}}}
	call := arenaHandler(t, store, now)
	_, out := call(7, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack, "power": 1})
	team, _ := out["team"].([]any)
	if len(team) != 5 || store.arena.power[7] <= 1 {
		t.Fatalf("saved = %v, power %d", out, store.arena.power[7])
	}
	// ★2 is ×1.2: 1000 attack is 1200, 3000 health 3600.
	if f := team[0].(map[string]any); f["id"] != 10.0 || f["stars"] != 2.0 || f["attack"] != 1200.0 || f["health"] != 3600.0 || f["class"] != "mage" || f["image"] != "https://img/h.png" {
		t.Errorf("first fighter = %v", f)
	}
	if f := team[4].(map[string]any); f["rarity"] != 4.0 || f["skill"] == "" || f["class"] != "tank" {
		t.Errorf("the Mythic = %v", f)
	}
	if p := out["player"].(map[string]any); p["power"] != float64(store.arena.power[7]) {
		t.Errorf("player = %v", p)
	}
	for name, fields := range map[string]map[string]any{
		"size":     {"front": []int64{10}, "back": arenaBack},
		"repeat":   {"front": []int64{10, 10}, "back": arenaBack},
		"not_held": {"front": []int64{10, 99}, "back": arenaBack},
	} {
		fields["action"] = "arena_team"
		if _, out := call(7, fields); out["refused"] != name {
			t.Errorf("%s = %v", name, out)
		}
	}
}

// The arena shows the player with their tickets, their team, opponents,
// the table and their battle history; a player without a team has none.
func TestArenaView(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{arena: fakeArena{held: map[int64][]storage.OwnedCard{7: arenaCards(), 8: arenaCards()}}}
	call := arenaHandler(t, store, now)
	_, out := call(7, map[string]any{"action": "arena"})
	if out["team"] != nil || len(out["opponents"].([]any)) != 0 || len(out["top"].([]any)) != 1 || len(out["history"].([]any)) != 0 {
		t.Fatalf("arena without teams = %v", out)
	}
	if p := out["player"].(map[string]any); p["rating"] != 1000.0 || p["tickets"] != 8.0 || p["tickets_max"] != 8.0 || p["tokens_left"] != float64(ArenaTokens) {
		t.Errorf("player = %v", p)
	}
	call(8, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	_, out = call(7, map[string]any{"action": "arena"})
	opponents, _ := out["opponents"].([]any)
	if len(opponents) != 1 || opponents[0].(map[string]any)["id"] != 8.0 || opponents[0].(map[string]any)["power"].(float64) <= 0 {
		t.Errorf("opponents = %v", out["opponents"])
	}
	if row := out["top"].([]any)[0].(map[string]any); row["me"] != true || row["rating"] != 1016.0 {
		t.Errorf("top = %v", row)
	}
}

// An attack fights the two saved teams and gives the events to replay, the
// fighters of both sides and what it changed; one with no ticket left is
// refused, as is an attack without a team or on a player whose team is
// gone. The defender's history shows each battle, the newest first.
func TestArenaAttack(t *testing.T) {
	now := time.Date(2026, 11, 18, 23, 30, 0, 0, Location)
	store := &fakeStore{arena: fakeArena{held: map[int64][]storage.OwnedCard{7: arenaCards(), 8: arenaCards(), 9: arenaCards()}}}
	call := arenaHandler(t, store, now)
	if _, out := call(7, map[string]any{"action": "arena_attack", "opponent": 8}); out["refused"] != "no_team" {
		t.Errorf("an attack without a team = %v", out)
	}
	call(7, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	if _, out := call(7, map[string]any{"action": "arena_attack", "opponent": 8}); out["refused"] != "opponent_gone" {
		t.Errorf("an attack on a player without a team = %v", out)
	}
	call(8, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	code, out := call(7, map[string]any{"action": "arena_attack", "opponent": 8})
	events, _ := out["events"].([]any)
	if code != http.StatusOK || len(events) == 0 || out["tickets"] != float64(ArenaTickets-1) || out["next_ticket_in"] != float64(time.Hour/time.Millisecond) || out["won"] == nil || out["rating_change"] == nil || out["token"] == nil || out["tokens_left"] == nil {
		t.Fatalf("attack = %d %v", code, out)
	}
	if e := events[0].(map[string]any); e["kind"] == nil || e["round"] == nil || e["hp"] == nil {
		t.Errorf("an event = %v", e)
	}
	if a, d := out["attacker"].([]any), out["defender"].([]any); len(a) != 5 || len(d) != 5 || d[0].(map[string]any)["attack"] != 1200.0 {
		t.Errorf("fighters = %v %v", a, d)
	}
	if r := store.arena.rules[0]; !r.Day.Equal(time.Date(2026, 11, 18, 0, 0, 0, 0, Location)) || r.Day.Location() != Location || !r.Now.Equal(now) || r.Tickets != 8 || r.TicketEvery != time.Hour {
		t.Errorf("the arena rules = %+v", r)
	}
	for range ArenaTickets - 1 {
		call(7, map[string]any{"action": "arena_attack", "opponent": 8})
	}
	if _, out := call(7, map[string]any{"action": "arena_attack", "opponent": 8}); out["refused"] != "no_tickets" || out["events"] != nil {
		t.Errorf("an attack with no ticket = %v", out)
	}
	_, out = call(8, map[string]any{"action": "arena"})
	history, _ := out["history"].([]any)
	if len(history) != ArenaTickets {
		t.Fatalf("player 8's history = %v", out["history"])
	}
	if b := history[0].(map[string]any); b["attacked"] != false || b["opponent"] != "Bek" || b["won"] == nil || b["rating_change"] == nil || b["at"] != float64(now.UnixMilli()) {
		t.Errorf("a battle defended = %v", b)
	}
	// Player 9 saves a team, then trades a card of it away.
	call(9, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	store.arena.held[9] = store.arena.held[9][1:]
	if _, out := call(8, map[string]any{"action": "arena_attack", "opponent": 9}); out["refused"] != "opponent_gone" {
		t.Errorf("an attack on a team no longer held = %v", out)
	}
	if _, out := call(9, map[string]any{"action": "arena_attack", "opponent": 8}); out["refused"] != "no_team" {
		t.Errorf("an attack with a team no longer held = %v", out)
	}
	if _, out := call(8, map[string]any{"action": "arena_attack", "opponent": 8}); out["refused"] != "self" {
		t.Errorf("an attack on oneself = %v", out)
	}
	if code, _ := call(8, map[string]any{"action": "arena_attack"}); code != http.StatusBadRequest {
		t.Errorf("an attack on no one = %d", code)
	}
}
