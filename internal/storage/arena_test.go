package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// arenaDay is the start of 18 November 2026 in Tashkent, 19:00 UTC the day
// before, so a date read in UTC would be the wrong one.
var arenaDay = time.Date(2026, 11, 18, 0, 0, 0, 0, time.FixedZone("Asia/Tashkent", 5*60*60))

// arenaNow is 10:00 that day; tickets are counted at it.
var arenaNow = arenaDay.Add(10 * time.Hour)

var arenaRules = ArenaRules{Tickets: 8, TicketEvery: time.Hour, Tokens: 3, K: 32, Day: arenaDay, Now: arenaNow}

var (
	aliceFront = [2]int64{1, 2}
	aliceBack  = [3]int64{3, 4, 5}
	bobFront   = [2]int64{11, 12}
	bobBack    = [3]int64{13, 14, 15}
)

// seedArena puts twenty cards in the catalog, 1 to 10 held by Alice (1) and
// 11 to 20 by Bob (2), one copy each, Bob holding two of 20.
func seedArena(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) SELECT g, 'Card ' || g, 3, 1000, 3000, 1000 FROM generate_series(1, 20) g`,
		`INSERT INTO card_players (user_id, name, points, cards, draws, tokens) VALUES (1, 'Alice', 10000, 10, 10, 0), (2, 'Bob', 11000, 10, 11, 0)`,
		`INSERT INTO card_owned (user_id, card_id) SELECT 1, g FROM generate_series(1, 10) g`,
		`INSERT INTO card_owned (user_id, card_id, count) SELECT 2, g, CASE WHEN g = 20 THEN 2 ELSE 1 END FROM generate_series(11, 20) g`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
}

func mustSaveTeam(t *testing.T, store *Store, userID int64, front [2]int64, back [3]int64, power int) {
	t.Helper()
	if err := store.SaveArenaTeam(context.Background(), userID, front, back, power); err != nil {
		t.Fatalf("SaveArenaTeam(%d): %v", userID, err)
	}
}

func cardTokens(t *testing.T, store *Store, userID int64) int {
	t.Helper()
	var tokens int
	if err := store.pool.QueryRow(context.Background(), `SELECT tokens FROM card_players WHERE user_id = $1`, userID).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	return tokens
}

// A team is saved and read back as it was, and saving again replaces it.
func TestArenaTeamSavedAndRead(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)

	if _, _, ok, err := store.ArenaTeam(ctx, 1); err != nil || ok {
		t.Fatalf("a team never saved = ok %v, %v", ok, err)
	}
	mustSaveTeam(t, store, 1, aliceFront, aliceBack, 6000)
	front, back, ok, err := store.ArenaTeam(ctx, 1)
	if err != nil || !ok || front != aliceFront || back != aliceBack {
		t.Fatalf("ArenaTeam = %v %v %v, %v", front, back, ok, err)
	}
	mustSaveTeam(t, store, 1, [2]int64{6, 7}, [3]int64{8, 9, 10}, 7000)
	front, back, _, _ = store.ArenaTeam(ctx, 1)
	if front != [2]int64{6, 7} || back != [3]int64{8, 9, 10} {
		t.Errorf("team saved again = %v %v", front, back)
	}
	player, err := store.ArenaPlayerOf(ctx, 1, arenaRules)
	if err != nil {
		t.Fatal(err)
	}
	want := ArenaPlayer{UserID: 1, Name: "Alice", Rating: 1000, Tickets: 8, TokensLeft: 3, Power: 7000}
	if player != want {
		t.Errorf("ArenaPlayerOf = %+v, want %+v", player, want)
	}
	if player, err := store.ArenaPlayerOf(ctx, 99, arenaRules); err != nil || player != (ArenaPlayer{UserID: 99, Rating: 1000, Tickets: 8, TokensLeft: 3}) {
		t.Errorf("a stranger = %+v, %v", player, err)
	}
}

// A team of a card twice, or of a card not held, is refused and the team
// saved before stays.
func TestArenaTeamRefusals(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	mustSaveTeam(t, store, 1, aliceFront, aliceBack, 6000)

	if err := store.SaveArenaTeam(ctx, 1, [2]int64{1, 2}, [3]int64{3, 4, 1}, 6000); !errors.Is(err, ErrArenaTeamRepeat) {
		t.Errorf("a card twice = %v", err)
	}
	if err := store.SaveArenaTeam(ctx, 1, [2]int64{1, 2}, [3]int64{3, 4, 11}, 6000); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("Bob's card in Alice's team = %v", err)
	}
	if err := store.SaveArenaTeam(ctx, 1, [2]int64{1, 2}, [3]int64{3, 4, 999}, 6000); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("a card not in the catalog = %v", err)
	}
	if front, back, _, _ := store.ArenaTeam(ctx, 1); front != aliceFront || back != aliceBack {
		t.Errorf("team after refusals = %v %v", front, back)
	}
}

// Elo at K 32 between equals moves 16 each way; a rating never goes
// below 0.
func TestArenaElo(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)

	out, err := store.RecordArenaBattle(ctx, 1, 2, 42, true, arenaRules, CardQuest{})
	if err != nil {
		t.Fatal(err)
	}
	if out != (ArenaOutcome{RatingChange: 16, Token: true, Tickets: 7, TokensLeft: 2, NextTicket: time.Hour}) {
		t.Errorf("a win between equals = %+v", out)
	}
	alice, _ := store.ArenaPlayerOf(ctx, 1, arenaRules)
	bob, _ := store.ArenaPlayerOf(ctx, 2, arenaRules)
	if alice.Rating != 1016 || alice.Wins != 1 || bob.Rating != 984 || bob.Losses != 1 {
		t.Errorf("after the win: Alice %+v, Bob %+v", alice, bob)
	}
	if bob.Tickets != 8 || bob.TokensLeft != 3 || cardTokens(t, store, 2) != 0 {
		t.Errorf("defending changed Bob's day: %+v", bob)
	}
	if cardTokens(t, store, 1) != 1 {
		t.Errorf("Alice's tokens = %d, want 1", cardTokens(t, store, 1))
	}
	var attacker, defender, seed int64
	var won bool
	var change, defenderChange int
	if err := store.pool.QueryRow(ctx, `SELECT attacker, defender, seed, attacker_won, rating_change, defender_change FROM arena_battles`).Scan(&attacker, &defender, &seed, &won, &change, &defenderChange); err != nil || attacker != 1 || defender != 2 || seed != 42 || !won || change != 16 || defenderChange != -16 {
		t.Errorf("battle = %d %d %d %v %d %d, %v", attacker, defender, seed, won, change, defenderChange, err)
	}

	// Carol (3), new at 1000, loses to Dave (4) at 1000: −16; then, both
	// at 5, she loses the 5 she has and Dave gains 16.
	if out, err := store.RecordArenaBattle(ctx, 3, 4, 7, false, arenaRules, CardQuest{}); err != nil || out.RatingChange != -16 || out.Token {
		t.Errorf("a loss between equals = %+v, %v", out, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE arena_players SET rating = 5 WHERE user_id IN (3, 4)`); err != nil {
		t.Fatal(err)
	}
	if out, err := store.RecordArenaBattle(ctx, 3, 4, 8, false, arenaRules, CardQuest{}); err != nil || out.RatingChange != -5 {
		t.Errorf("a loss at 5 = %+v, %v", out, err)
	}
	if carol, _ := store.ArenaPlayerOf(ctx, 3, arenaRules); carol.Rating != 0 {
		t.Errorf("Carol's rating = %d, want 0", carol.Rating)
	}

	top, err := store.ArenaTop(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 3 || top[0].UserID != 1 || top[0].Name != "Alice" || top[0].Wins != 1 || top[1].UserID != 2 || top[2].UserID != 4 || top[2].Rating != 21 {
		t.Errorf("ArenaTop = %+v", top)
	}
}

// Review Focus 1: a saved team holding a card traded away on the market
// still attacks and is attacked, but is not offered as an opponent, and
// saving it again is refused.
func TestTeamWithACardTradedAway(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	mustSaveTeam(t, store, 1, aliceFront, aliceBack, 6000)
	mustSaveTeam(t, store, 2, bobFront, bobBack, 6000)

	now := time.Now()
	market := CardMarketRules{Open: 5, Keep: 7 * 24 * time.Hour, Now: now}
	offer, err := store.OfferCard(ctx, 1, 3, 20, market)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TakeCardOffer(ctx, offer, 2, CardTradeRules{Now: now}, market.Keep, CardQuest{}); err != nil {
		t.Fatal(err)
	}

	if opponents, err := store.ArenaOpponents(ctx, 2, 3); err != nil || len(opponents) != 0 {
		t.Errorf("Bob is offered %+v, %v", opponents, err)
	}
	if err := store.SaveArenaTeam(ctx, 1, aliceFront, aliceBack, 6000); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("saving the team again = %v", err)
	}
	if front, back, ok, err := store.ArenaTeam(ctx, 1); err != nil || !ok || front != aliceFront || back != aliceBack {
		t.Errorf("Alice's team = %v %v %v, %v", front, back, ok, err)
	}
	if _, err := store.RecordArenaBattle(ctx, 1, 2, 1, true, arenaRules, CardQuest{}); err != nil {
		t.Errorf("Alice attacking = %v", err)
	}
	if _, err := store.RecordArenaBattle(ctx, 2, 1, 2, true, arenaRules, CardQuest{}); err != nil {
		t.Errorf("Alice attacked = %v", err)
	}
	if opponents, err := store.ArenaOpponents(ctx, 1, 3); err != nil || len(opponents) != 1 || opponents[0].UserID != 2 {
		t.Errorf("Alice is offered %+v, %v", opponents, err)
	}
}

// Review Focus 2: attacks at once are counted one after the other: never a
// ninth attack, a fourth token or a rating counted twice, and two players
// attacking each other at once do not deadlock.
func TestAttacksAreCountedUnderALock(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Alice attacks and wins; Bob attacks and loses.
			attacker, defender, won := int64(1), int64(2), true
			if i%2 == 1 {
				attacker, defender, won = 2, 1, false
			}
			_, err := store.RecordArenaBattle(ctx, attacker, defender, int64(i), won, arenaRules, CardQuest{})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	done, refused := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			done++
		case errors.Is(err, ErrArenaNoTickets):
			refused++
		default:
			t.Errorf("RecordArenaBattle: %v", err)
		}
	}
	if done != 16 || refused != 4 {
		t.Errorf("%d attacks done, %d refused; want 16 and 4", done, refused)
	}
	alice, _ := store.ArenaPlayerOf(ctx, 1, arenaRules)
	bob, _ := store.ArenaPlayerOf(ctx, 2, arenaRules)
	if alice.Wins != 16 || alice.Losses != 0 || bob.Wins != 0 || bob.Losses != 16 {
		t.Errorf("Alice %+v, Bob %+v", alice, bob)
	}
	if alice.Tickets != 0 || alice.TokensLeft != 0 || bob.Tickets != 0 || bob.TokensLeft != 3 {
		t.Errorf("days: Alice %+v, Bob %+v", alice, bob)
	}
	if alice.Rating+bob.Rating != 2000 || alice.Rating <= 1000 {
		t.Errorf("ratings Alice %d, Bob %d", alice.Rating, bob.Rating)
	}
	if got := cardTokens(t, store, 1); got != 3 {
		t.Errorf("Alice's tokens = %d, want 3", got)
	}
	if got := cardTokens(t, store, 2); got != 0 {
		t.Errorf("Bob's tokens = %d, want 0", got)
	}
	var mine, his int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE attacker = 1), count(*) FILTER (WHERE attacker = 2) FROM arena_battles`).Scan(&mine, &his); err != nil || mine != 8 || his != 8 {
		t.Errorf("battles recorded: Alice's %d, Bob's %d, %v", mine, his, err)
	}
}

// Tickets: eight at most, each attack takes one and the ninth is refused;
// one comes back each hour after the stock stopped being full, never more
// than eight; the day's tokens start again on the Tashkent day after.
func TestArenaTicketsComeBackHourly(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)

	for i := range 8 {
		if _, err := store.RecordArenaBattle(ctx, 1, 2, int64(i), true, arenaRules, CardQuest{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.RecordArenaBattle(ctx, 1, 2, 8, true, arenaRules, CardQuest{}); !errors.Is(err, ErrArenaNoTickets) {
		t.Fatalf("a ninth attack = %v", err)
	}
	if alice, _ := store.ArenaPlayerOf(ctx, 1, arenaRules); alice.Tickets != 0 || alice.NextTicket != time.Hour || alice.TokensLeft != 0 {
		t.Errorf("with the tickets spent = %+v", alice)
	}

	at := func(d time.Duration) ArenaRules {
		r := arenaRules
		r.Now = arenaNow.Add(d)
		return r
	}
	for _, c := range []struct {
		after   time.Duration
		tickets int
		next    time.Duration
	}{
		{30 * time.Minute, 0, 30 * time.Minute},
		{time.Hour, 1, time.Hour},
		{3*time.Hour + 30*time.Minute, 3, 30 * time.Minute},
		{20 * time.Hour, 8, 0},
	} {
		if alice, _ := store.ArenaPlayerOf(ctx, 1, at(c.after)); alice.Tickets != c.tickets || alice.NextTicket != c.next {
			t.Errorf("after %v: %d tickets, next in %v; want %d, %v", c.after, alice.Tickets, alice.NextTicket, c.tickets, c.next)
		}
	}

	// At 13:30 three are back; one spent leaves two, the half hour toward the
	// next one kept.
	out, err := store.RecordArenaBattle(ctx, 1, 2, 9, false, at(3*time.Hour+30*time.Minute), CardQuest{})
	if err != nil || out.Tickets != 2 || out.NextTicket != 30*time.Minute {
		t.Errorf("an attack at 13:30 = %+v, %v", out, err)
	}
	if alice, _ := store.ArenaPlayerOf(ctx, 1, at(4*time.Hour)); alice.Tickets != 3 || alice.NextTicket != time.Hour {
		t.Errorf("at 14:00 = %+v", alice)
	}

	next := at(24 * time.Hour)
	next.Day = arenaDay.AddDate(0, 0, 1)
	if alice, _ := store.ArenaPlayerOf(ctx, 1, next); alice.Tickets != 8 || alice.TokensLeft != 3 {
		t.Errorf("the next day = %+v", alice)
	}
	if out, err := store.RecordArenaBattle(ctx, 1, 2, 10, true, next, CardQuest{}); err != nil || out.Tickets != 7 || out.TokensLeft != 2 || !out.Token {
		t.Errorf("the next day's attack = %+v, %v", out, err)
	}
	if got := cardTokens(t, store, 1); got != 4 {
		t.Errorf("Alice's tokens = %d, want 4", got)
	}
}

// The battle history shows a player's battles, attacking and attacked, the
// newest first, each with the other player, who won as they see it, and
// their own rating change; at most limit.
func TestArenaBattleHistory(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)

	if battles, err := store.ArenaBattles(ctx, 1, 10); err != nil || len(battles) != 0 {
		t.Fatalf("no battles = %+v, %v", battles, err)
	}
	// Alice beats Bob (+16, Bob −16), then Bob attacks Alice and loses.
	if _, err := store.RecordArenaBattle(ctx, 1, 2, 1, true, arenaRules, CardQuest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordArenaBattle(ctx, 2, 1, 2, false, arenaRules, CardQuest{}); err != nil {
		t.Fatal(err)
	}
	alice, err := store.ArenaBattles(ctx, 1, 10)
	if err != nil || len(alice) != 2 {
		t.Fatalf("Alice's battles = %+v, %v", alice, err)
	}
	if b := alice[0]; b.Attacked || b.OpponentID != 2 || b.Opponent != "Bob" || !b.Won || b.RatingChange <= 0 {
		t.Errorf("Alice defending = %+v", b)
	}
	if b := alice[1]; !b.Attacked || b.OpponentID != 2 || !b.Won || b.RatingChange != 16 {
		t.Errorf("Alice attacking = %+v", b)
	}
	bob, _ := store.ArenaBattles(ctx, 2, 10)
	if len(bob) != 2 || !bob[0].Attacked || bob[0].Won || bob[0].RatingChange != -alice[0].RatingChange || bob[1].Attacked || bob[1].RatingChange != -16 {
		t.Errorf("Bob's battles = %+v", bob)
	}
	if bobRow, _ := store.ArenaPlayerOf(ctx, 2, arenaRules); bobRow.Rating != 1000-16+bob[0].RatingChange {
		t.Errorf("Bob's rating %d does not match his history %+v", bobRow.Rating, bob)
	}
	if last, _ := store.ArenaBattles(ctx, 1, 1); len(last) != 1 || last[0].Attacked {
		t.Errorf("the latest battle = %+v", last)
	}
}

// Review Focus 5: with no one, only oneself, or one other with a team, the
// opponents never include oneself and never fail; with more, the nearest
// ratings come first, at most n.
func TestOpponentsWhenFewHaveTeams(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)

	if opponents, err := store.ArenaOpponents(ctx, 1, 3); err != nil || len(opponents) != 0 {
		t.Errorf("no teams: %+v, %v", opponents, err)
	}
	mustSaveTeam(t, store, 1, aliceFront, aliceBack, 6000)
	if opponents, err := store.ArenaOpponents(ctx, 1, 3); err != nil || len(opponents) != 0 {
		t.Errorf("only one's own team: %+v, %v", opponents, err)
	}
	mustSaveTeam(t, store, 2, bobFront, bobBack, 6500)
	opponents, err := store.ArenaOpponents(ctx, 1, 3)
	if err != nil || len(opponents) != 1 {
		t.Fatalf("two teams: %+v, %v", opponents, err)
	}
	if o := opponents[0]; o.UserID != 2 || o.Name != "Bob" || o.Rating != 1000 || o.Power != 6500 {
		t.Errorf("Bob offered as %+v", o)
	}

	// Carol (3), Dave (4) and Erin (5), each holding their own five cards.
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) SELECT g, 'Card ' || g, 3, 1000, 3000, 1000 FROM generate_series(21, 35) g`,
		`INSERT INTO card_players (user_id, name) VALUES (3, 'Carol'), (4, 'Dave'), (5, 'Erin')`,
		`INSERT INTO card_owned (user_id, card_id) SELECT 3 + (g - 21) / 5, g FROM generate_series(21, 35) g`,
		`INSERT INTO arena_players (user_id, rating) VALUES (1, 1100), (2, 1500), (3, 1050), (4, 900), (5, 1300)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	mustSaveTeam(t, store, 3, [2]int64{21, 22}, [3]int64{23, 24, 25}, 1)
	mustSaveTeam(t, store, 4, [2]int64{26, 27}, [3]int64{28, 29, 30}, 1)
	mustSaveTeam(t, store, 5, [2]int64{31, 32}, [3]int64{33, 34, 35}, 1)
	opponents, err = store.ArenaOpponents(ctx, 1, 3)
	if err != nil || len(opponents) != 3 {
		t.Fatalf("five teams: %+v, %v", opponents, err)
	}
	// Alice at 1100: Carol 50 away, Dave 200, Erin 200; Bob 400 is left out.
	if opponents[0].UserID != 3 || opponents[1].UserID != 4 || opponents[2].UserID != 5 {
		t.Errorf("opponents = %+v", opponents)
	}
	// A player with no team and no rating yet is matched from 1000.
	opponents, err = store.ArenaOpponents(ctx, 6, 1)
	if err != nil || len(opponents) != 1 || opponents[0].UserID != 3 {
		t.Errorf("a newcomer's opponent = %+v, %v", opponents, err)
	}
}
