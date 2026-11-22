package cards

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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
	coins   map[int64]int
	bought  map[int64]int // arena shop tokens bought, on any day
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
	return storage.ArenaPlayer{UserID: userID, Name: "Bek", Rating: 1000, Tickets: rules.Tickets - f.arena.attacks[userID], TokensLeft: rules.Tokens,
		Power: f.arena.power[userID], Coins: f.arena.coins[userID], ShopTokens: f.arena.bought[userID]}, nil
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

func (f *fakeStore) RecordArenaBattle(_ context.Context, attacker, defender, _ int64, won bool, rules storage.ArenaRules, quest storage.CardQuest) (storage.ArenaOutcome, error) {
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
	if f.arena.coins == nil {
		f.arena.coins = map[int64]int{}
	}
	change, coins := -16, rules.CoinsLoss
	if won {
		change, coins = 16, rules.CoinsWin
		f.count(attacker, quest, 1)
	}
	f.arena.coins[attacker] += coins
	f.arena.battles[attacker] = append(f.arena.battles[attacker], storage.ArenaBattle{At: rules.Now, Attacked: true, OpponentID: defender, Opponent: "Ali", Won: won, RatingChange: change})
	f.arena.battles[defender] = append(f.arena.battles[defender], storage.ArenaBattle{At: rules.Now, OpponentID: attacker, Opponent: "Bek", Won: !won, RatingChange: -change})
	return storage.ArenaOutcome{RatingChange: change, Token: won, Tickets: rules.Tickets - f.arena.attacks[attacker], TokensLeft: rules.Tokens, NextTicket: rules.TicketEvery,
		CoinsWon: coins, Coins: f.arena.coins[attacker]}, nil
}

func (f *fakeStore) ArenaBattles(_ context.Context, userID int64, limit int) ([]storage.ArenaBattle, error) {
	mine := f.arena.battles[userID]
	var out []storage.ArenaBattle
	for i := len(mine) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, mine[i])
	}
	return out, nil
}

func (f *fakeStore) ArenaWeekPlace(context.Context, int64, time.Time) (int, error) { return 12, nil }

func (f *fakeStore) ArenaPlace(context.Context, int64) (int, int, error) { return 7, 40, nil }

func (f *fakeStore) ArenaTop(context.Context, int) ([]storage.ArenaPlayer, error) {
	return []storage.ArenaPlayer{{UserID: 7, Name: "Bek", Rating: 1016, Wins: 1, Power: 100}}, nil
}

// arenaCards are five cards, a Mythic among them, each held at ★2 by the
// players 7 and 8.
func arenaCards() []storage.OwnedCard {
	cards := make([]storage.OwnedCard, 5)
	for i := range cards {
		c := storage.Card{ID: int64(10 + i), Name: "Hero", ImageURL: "https://img/h.png", Rarity: i % 5, Attack: 1000, Health: 3000 + 1000*i}
		if i == 1 {
			c.Health = 3500 // a Dealer, between the Support and the Tanks
		}
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
	if f := team[0].(map[string]any); f["id"] != 10.0 || f["stars"] != 2.0 || f["attack"] != 1200.0 || f["health"] != 3600.0 || f["class"] != "support" || f["image"] != "https://img/h.png" {
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
	if me := out["me"].(map[string]any); me["place"] != 7.0 || me["total"] != 40.0 {
		t.Errorf("my arena place = %v", me)
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
	// The player can look at the opponent's team before attacking.
	if len(opponents) == 1 {
		if team, _ := opponents[0].(map[string]any)["team"].([]any); len(team) != 5 {
			t.Errorf("opponent team = %v, want 5 fighters", opponents[0].(map[string]any)["team"])
		}
	}
	if row := out["top"].([]any)[0].(map[string]any); row["me"] != true || row["rating"] != 1016.0 {
		t.Errorf("top = %v", row)
	}
	// The fake places the player 12th: 11 to 30 get 15 tokens.
	if week := out["week"].(map[string]any); week["place"] != 12.0 || week["prize"] != 15.0 || len(week["prizes"].([]any)) != len(ArenaWeekPrizes) || week["ends"] != float64(arenaWeekStart(now).AddDate(0, 0, 7).UnixMilli()) {
		t.Errorf("week = %v", week)
	}
}

// A week runs from Monday midnight in Tashkent; the one paid is the last
// one ended.
func TestArenaWeek(t *testing.T) {
	// Sunday 22 November 2026, 23:30 in Tashkent, then Monday 23rd 00:10.
	sunday := time.Date(2026, 11, 22, 23, 30, 0, 0, Location)
	monday := time.Date(2026, 11, 23, 0, 10, 0, 0, Location)
	if got, want := arenaWeekStart(sunday), time.Date(2026, 11, 16, 0, 0, 0, 0, Location); !got.Equal(want) {
		t.Errorf("week of Sunday starts %v, want %v", got, want)
	}
	if got, want := arenaWeekStart(monday), time.Date(2026, 11, 23, 0, 0, 0, 0, Location); !got.Equal(want) {
		t.Errorf("week of Monday starts %v, want %v", got, want)
	}
	payer := &fakeWeekPayer{}
	if _, err := PayArenaWeek(context.Background(), payer, monday.Add(8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !payer.since.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, Location)) || !payer.until.Equal(time.Date(2026, 11, 23, 0, 0, 0, 0, Location)) {
		t.Errorf("paid %v to %v", payer.since, payer.until)
	}
	if payer.note != "🏆 Arena haftalik mukofoti: 3-o'rin" {
		t.Errorf("note = %q", payer.note)
	}
	// The tokens are every week's alike; every week's top 3 get diamond
	// boxes, 3, 2 and 1, and the event week's top 5 get 4, 3, 2, 1 and 1.
	event, next := arenaWeekPrizes(ArenaEventWeek), arenaWeekPrizes(ArenaEventWeek.AddDate(0, 0, 7))
	for place, want := range map[int]int{1: 100, 2: 75, 3: 50, 4: 25, 5: 25, 6: 20, 10: 20, 11: 15, 50: 10, 100: 5, 101: 0, 0: 0} {
		if got := arenaWeekPrize(next, place).Tokens; got != want {
			t.Errorf("prize for %d = %d, want %d", place, got, want)
		}
		if got := arenaWeekPrize(event, place).Tokens; got != want {
			t.Errorf("event prize for %d = %d tokens, want %d", place, got, want)
		}
	}
	for place, want := range map[int][2]int{1: {4, 3}, 2: {3, 2}, 3: {2, 1}, 4: {1, 0}, 5: {1, 0}, 6: {0, 0}, 0: {0, 0}} {
		if got := arenaWeekPrize(event, place).Boxes; got != want[0] {
			t.Errorf("event boxes for %d = %d, want %d", place, got, want[0])
		}
		if got := arenaWeekPrize(next, place).Boxes; got != want[1] {
			t.Errorf("the week after the event gives %d boxes to %d, want %d", got, place, want[1])
		}
	}
	if !ArenaEventWeek.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, Location)) || ArenaEventWeek.Weekday() != time.Monday {
		t.Errorf("event week = %v", ArenaEventWeek)
	}
	// Paid on Monday 23 November, the week ended is the event's.
	if len(payer.prizes) == 0 || payer.prizes[0].Boxes != 4 {
		t.Errorf("the event week was paid with %+v, want 4 boxes to the first", payer.prizes)
	}
	if _, ok := BoxByID(arenaWeekBox); !ok {
		t.Errorf("the week's box %q is no box the inventory holds", arenaWeekBox)
	}
}

type fakeWeekPayer struct {
	since, until time.Time
	note         string
	prizes       []storage.ArenaPrize
}

func (f *fakeWeekPayer) PayArenaWeek(_ context.Context, since, until time.Time, prizes []storage.ArenaPrize, note func(int) string) (int, bool, error) {
	f.since, f.until, f.note, f.prizes = since, until, note(3), prizes
	return 0, true, nil
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

// A refresh offers opponents other than those shown while the pool has
// them, listed nearest in rating first, and falls back on the shown ones.
func TestPickOpponentsRefreshes(t *testing.T) {
	pool := make([]storage.ArenaPlayer, 6)
	for i := range pool {
		pool[i] = storage.ArenaPlayer{UserID: int64(i + 1)}
	}
	keep := func(int, func(int, int)) {} // no shuffle: the pool's order
	ids := func(ps []storage.ArenaPlayer) []int64 {
		out := make([]int64, len(ps))
		for i, p := range ps {
			out[i] = p.UserID
		}
		return out
	}
	if got := ids(pickOpponents(pool, nil, 3, keep)); !slices.Equal(got, []int64{1, 2, 3}) {
		t.Errorf("first = %v", got)
	}
	if got := ids(pickOpponents(pool, []int64{1, 2, 3}, 3, keep)); !slices.Equal(got, []int64{4, 5, 6}) {
		t.Errorf("refresh = %v, want the others", got)
	}
	if got := ids(pickOpponents(pool, []int64{1, 2, 3, 4, 5}, 3, keep)); !slices.Equal(got, []int64{1, 2, 6}) {
		t.Errorf("few others = %v, want the other one and the nearest shown", got)
	}
	if got := ids(pickOpponents(pool[:2], nil, 3, keep)); !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("a small pool = %v", got)
	}
	reversed := func(n int, swap func(int, int)) {
		for i := 0; i < n/2; i++ {
			swap(i, n-1-i)
		}
	}
	if got := ids(pickOpponents(pool, nil, 3, reversed)); !slices.Equal(got, []int64{4, 5, 6}) {
		t.Errorf("shuffled = %v, want the drawn ones listed nearest first", got)
	}
}

// Cards of one anime in a team, seasons and sequels as one, raise each
// other: a pair by SynergyTwo, three or more by SynergyThree, each pair on
// its own; a card without an anime has none.
func TestSynergies(t *testing.T) {
	cases := []struct {
		animes [5]string
		want   [5]int
	}{
		{[5]string{"A", "B", "C", "D", "E"}, [5]int{}},
		{[5]string{"A", "A", "C", "D", "E"}, [5]int{SynergyTwo, SynergyTwo, 0, 0, 0}},
		{[5]string{"A", "B", "A", "D", "A"}, [5]int{SynergyThree, 0, SynergyThree, 0, SynergyThree}},
		{[5]string{"A", "B", "A", "B", "E"}, [5]int{SynergyTwo, SynergyTwo, SynergyTwo, SynergyTwo, 0}},
		{[5]string{"A", "A", "A", "A", "A"}, [5]int{SynergyThree, SynergyThree, SynergyThree, SynergyThree, SynergyThree}},
		{[5]string{"", "", "C", "D", "E"}, [5]int{}},
	}
	for _, c := range cases {
		if got := synergies(c.animes); got != c.want {
			t.Errorf("synergies(%v) = %v, want %v", c.animes, got, c.want)
		}
	}
	if raised(1200, 10) != 1320 || raised(3600, 20) != 4320 || raised(5, 10) != 6 || raised(777, 0) != 777 {
		t.Error("raised rounds wrong")
	}
}

// A team's synergy is by franchise, so two seasons count as one anime, and
// it is in the fighters, their views and the team's power saved.
func TestArenaTeamSynergy(t *testing.T) {
	held := arenaCards()
	held[0].Anime, held[1].Anime = "Attack on Titan", "Attack on Titan Season 3"
	held[2].Anime, held[3].Anime, held[4].Anime = "Naruto", "Naruto: Shippuden", "Naruto"
	team, ok := fightersOf(held, [5]int64{10, 11, 12, 13, 14})
	if !ok {
		t.Fatal("team not made")
	}
	plain, _ := fightersOf(arenaCards(), [5]int64{10, 11, 12, 13, 14})
	for i, v := range team.views {
		want := []int{SynergyTwo, SynergyTwo, SynergyThree, SynergyThree, SynergyThree}[i]
		base := plain.views[i]
		if v.Synergy != want || v.Attack != raised(base.Attack, want) || v.Health != raised(base.Health, want) || plain.views[i].Synergy != 0 {
			t.Errorf("slot %d = %+v, plain %+v", i, v, base)
		}
	}
	if team.views[0].Anime != "Attack on Titan" || team.views[3].Anime != "Naruto" {
		t.Errorf("animes %q %q", team.views[0].Anime, team.views[3].Anime)
	}
	if team.team.Power() <= plain.team.Power() || team.team.Front[0].Attack != team.views[0].Attack {
		t.Errorf("power %d, plain %d", team.team.Power(), plain.team.Power())
	}

	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{arena: fakeArena{held: map[int64][]storage.OwnedCard{7: held, 8: held}}}
	call := arenaHandler(t, store, now)
	_, out := call(7, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	if store.arena.power[7] != team.team.Power() {
		t.Errorf("power saved %d, want %d", store.arena.power[7], team.team.Power())
	}
	if f := out["team"].([]any)[2].(map[string]any); f["synergy"] != float64(SynergyThree) || f["anime"] != "Naruto" {
		t.Errorf("a fighter's view = %v", f)
	}
	call(8, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	_, out = call(7, map[string]any{"action": "arena_attack", "opponent": 8})
	edges := 0
	for _, e := range out["events"].([]any) {
		edge, ok := e.(map[string]any)["edge"].(bool)
		if !ok {
			t.Fatalf("an event without edge: %v", e)
		}
		if edge {
			edges++
		}
	}
	// The team has a Support, a Dealer and Tanks: Tanks beat Dealers, Dealers Supports.
	if edges == 0 {
		t.Error("no edge in the battle")
	}
	if d := out["defender"].([]any)[0].(map[string]any); d["synergy"] != float64(SynergyTwo) {
		t.Errorf("the defender's view = %v", d)
	}
}
