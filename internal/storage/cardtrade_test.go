package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// A gift moves one copy and its points; giving the last copy not melted
// takes the card out of the collection; a player gives as many cards a
// day as they like; a trade is done once, by the player it was offered to.
func TestCardsPassBetweenPlayers(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (10, 'Gojo Satoru', 4, 1, 1, 5000), (20, 'Levi', 3, 1, 1, 1000), (30, 'Naruto Uzumaki', 2, 1, 1, 300)`,
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 6300, 3, 4), (2, 'Bob', 1000, 1, 1), (3, 'Carol', 0, 0, 0)`,
		`INSERT INTO card_owned (user_id, card_id, count, melted) VALUES (1, 10, 1, 0), (1, 20, 3, 1), (1, 30, 1, 0), (2, 20, 1, 0)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	rules := CardTradeRules{Now: time.Now()}
	held := func(user, card int64) (count, melted int) {
		_ = store.pool.QueryRow(ctx, `SELECT count, melted FROM card_owned WHERE user_id = $1 AND card_id = $2`, user, card).Scan(&count, &melted)
		return
	}
	player := func(user int64) (points int64, cards int) {
		_ = store.pool.QueryRow(ctx, `SELECT points, cards FROM card_players WHERE user_id = $1`, user).Scan(&points, &cards)
		return
	}

	players, err := store.CardPlayersNamed(ctx, "", 1, 10)
	if err != nil || len(players) != 1 || players[0].Name != "Bob" {
		t.Fatalf("players but Alice, who have drawn = %+v, %v", players, err)
	}
	if players, _ = store.CardPlayersNamed(ctx, "ali", 2, 10); len(players) != 1 || players[0].UserID != 1 {
		t.Errorf("players named ali = %+v", players)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO group_members (chat_id, user_id, first_name, username) VALUES (-100, 2, 'Bob', 'bobby_k')`); err != nil {
		t.Fatal(err)
	}
	if players, _ = store.CardPlayersNamed(ctx, "@BOBBY", 1, 10); len(players) != 1 || players[0].Username != "bobby_k" {
		t.Errorf("players by @username = %+v", players)
	}

	// A spare Levi: Alice keeps two, one melted; Bob has two now.
	if err := store.GiveCard(ctx, -100, 1, 2, 20, rules); err != nil {
		t.Fatal(err)
	}
	if c, m := held(1, 20); c != 2 || m != 1 {
		t.Errorf("Alice's Levi = %d, %d melted", c, m)
	}
	if c, _ := held(2, 20); c != 2 {
		t.Errorf("Bob's Levi = %d", c)
	}
	// A copy adds no points, and its giver keeps the card.
	if p, n := player(1); p != 6300 || n != 3 {
		t.Errorf("Alice = %d points, %d cards", p, n)
	}
	if p, n := player(2); p != 1000 || n != 1 {
		t.Errorf("Bob = %d points, %d cards", p, n)
	}
	// Her last Levi not melted is not a spare: a gift is of spares alone.
	if err := store.GiveCard(ctx, -100, 1, 2, 20, rules); !errors.Is(err, ErrCardLastCopy) {
		t.Fatalf("a gift of the last copy = %v", err)
	}
	if c, _ := held(1, 20); c != 2 {
		t.Errorf("Alice's Levi after the refused gift = %d", c)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE card_owned SET count = 2 WHERE user_id = 1 AND card_id = 10`); err != nil {
		t.Fatal(err)
	}
	if err := store.GiveCard(ctx, -100, 1, 2, 10, rules); err != nil {
		t.Fatal(err)
	}
	// There is no daily limit: a third gift the same day goes too.
	if _, err := store.pool.Exec(ctx, `UPDATE card_owned SET count = 2 WHERE user_id = 1 AND card_id = 30`); err != nil {
		t.Fatal(err)
	}
	if err := store.GiveCard(ctx, -100, 1, 2, 30, rules); err != nil {
		t.Errorf("a third gift the same day = %v", err)
	}
	if err := store.GiveCard(ctx, -100, 2, 3, 20, rules); !errors.Is(err, ErrCardNoPlayer) {
		t.Errorf("a gift to one who never drew = %v", err)
	}
	if err := store.GiveCard(ctx, -100, 2, 1, 99, rules); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("a card not held = %v", err)
	}
}

// A player puts a card up for one they want; anyone holding that card takes
// the offer, each giving the other theirs; an offer whose card is gone is
// called off; a player has at most so many offers open.
func TestTheCardMarket(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (10, 'Gojo Satoru', 3, 1, 1, 5000), (20, 'Levi', 3, 1, 1, 1000), (30, 'Hinata Hyuuga', 3, 1, 1, 100)`,
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 6000, 2, 2), (2, 'Bob', 100, 1, 1)`,
		`INSERT INTO card_owned (user_id, card_id) VALUES (1, 10), (1, 20), (2, 30)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	market := CardMarketRules{Open: 2, Keep: 7 * 24 * time.Hour, Now: now}
	rules := CardTradeRules{Now: now}

	if _, err := store.OfferCard(ctx, 1, 30, 10, market); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("an offer of a card not held = %v", err)
	}
	levi, err := store.OfferCard(ctx, 1, 20, 30, market)
	if err != nil {
		t.Fatal(err)
	}
	gojo, err := store.OfferCard(ctx, 1, 10, 30, market)
	if err != nil {
		t.Fatal(err)
	}
	var many *CardOffersError
	if _, err := store.OfferCard(ctx, 1, 10, 20, market); !errors.As(err, &many) || many.Limit != 2 {
		t.Errorf("a third offer = %v", err)
	}
	offers, err := store.CardMarket(ctx, 2, market, 10)
	if err != nil || len(offers) != 2 || offers[0].ID != gojo || !offers[0].Can || offers[0].Mine || offers[0].FromName != "Alice" || offers[1].Want.Name != "Hinata Hyuuga" {
		t.Fatalf("Bob's market = %+v, %v", offers, err)
	}
	if offers, _ = store.CardMarket(ctx, 1, market, 10); !offers[0].Mine || offers[0].Can {
		t.Errorf("Alice's market = %+v", offers[0])
	}
	if _, err := store.TakeCardOffer(ctx, levi, 1, rules, market.Keep); !errors.Is(err, ErrCardOfferOwn) {
		t.Errorf("taking one's own offer = %v", err)
	}
	trade, err := store.TakeCardOffer(ctx, levi, 2, rules, market.Keep)
	if err != nil || trade.Give.Name != "Levi" || trade.Take.Name != "Hinata Hyuuga" || trade.From != 1 || trade.To != 2 {
		t.Fatalf("trade = %+v, %v", trade, err)
	}
	if _, err := store.TakeCardOffer(ctx, levi, 2, rules, market.Keep); !errors.Is(err, ErrCardTradeOver) {
		t.Errorf("taken twice = %v", err)
	}
	// Bob holds no Hinata now: he cannot take the Gojo offer.
	if _, err := store.TakeCardOffer(ctx, gojo, 2, rules, market.Keep); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("taking without the card wanted = %v", err)
	}
	// Alice gives her Gojo away elsewhere: her offer of it is called off.
	if _, err := store.pool.Exec(ctx, `DELETE FROM card_owned WHERE user_id = 1 AND card_id = 10`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO card_owned (user_id, card_id) VALUES (2, 30)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TakeCardOffer(ctx, gojo, 2, rules, market.Keep); !errors.Is(err, ErrCardOfferGone) {
		t.Errorf("an offer whose card is gone = %v", err)
	}
	if offers, _ = store.CardMarket(ctx, 2, market, 10); len(offers) != 0 {
		t.Errorf("the market after = %+v", offers)
	}
	found, err := store.CatalogCardsNamed(ctx, "hyu", 5)
	if err != nil || len(found) != 1 || found[0].ID != 30 {
		t.Errorf("catalog search = %+v, %v", found, err)
	}
}

// The market lists first the offers its viewer can take, older ones among
// them, and leaves out an offer whose player no longer holds its card: the
// market had stalled with such offers listed as takeable among the rest.
func TestTheMarketListsWhatTheViewerCanTakeFirst(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (20, 'Levi', 3, 1, 1, 1000), (30, 'Hinata Hyuuga', 3, 1, 1, 1000), (40, 'Kakashi', 3, 1, 1, 1000)`,
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 2000, 2, 2), (2, 'Bob', 1000, 1, 1)`,
		`INSERT INTO card_owned (user_id, card_id) VALUES (1, 20), (1, 40), (2, 30)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	market := CardMarketRules{Open: 5, Keep: 7 * 24 * time.Hour, Now: now}
	older, err := store.OfferCard(ctx, 1, 20, 30, market)
	if err != nil {
		t.Fatal(err)
	}
	market.Now = now.Add(time.Minute)
	newer, err := store.OfferCard(ctx, 1, 40, 20, market)
	if err != nil {
		t.Fatal(err)
	}
	offers, err := store.CardMarket(ctx, 2, market, 10)
	if err != nil || len(offers) != 2 || offers[0].ID != older || !offers[0].Can || offers[1].ID != newer || offers[1].Can {
		t.Fatalf("Bob's market = %+v, %v", offers, err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM card_owned WHERE user_id = 1 AND card_id = 40`); err != nil {
		t.Fatal(err)
	}
	if offers, err = store.CardMarket(ctx, 2, market, 10); err != nil || len(offers) != 1 || offers[0].ID != older {
		t.Errorf("Bob's market once Alice's Kakashi is gone = %+v, %v", offers, err)
	}
}

// A trade of cards of different rarities is refused, as an offer and as a
// take, even one rarity apart; two players trade with each other as often
// as they like.
func TestTradesAreEvenAndUnlimited(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (10, 'Goku', 4, 1, 1, 4000), (20, 'Vegeta', 3, 1, 1, 2000), (30, 'Extra', 0, 1, 1, 100), (40, 'Kakashi', 4, 1, 1, 2000)`,
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 6100, 3, 3), (2, 'Bob', 4000, 2, 2)`,
		`INSERT INTO card_owned (user_id, card_id) VALUES (1, 10), (1, 20), (1, 30), (2, 40)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	rules := CardTradeRules{Now: now}
	market := CardMarketRules{Open: 5, Keep: 7 * 24 * time.Hour, Now: now}
	if _, err := store.OfferCard(ctx, 1, 30, 10, market); !errors.Is(err, ErrCardTradeUneven) {
		t.Errorf("a Common put up for a Mythic = %v", err)
	}
	if _, err := store.OfferCard(ctx, 1, 20, 40, market); !errors.Is(err, ErrCardTradeUneven) {
		t.Errorf("a Legendary put up for a Mythic = %v", err)
	}
	// An offer placed before, Vegeta for Kakashi, is not listed, and taking
	// it is refused with the cards left where they were.
	var old int64
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO card_trades (chat_id, from_id, give_card, take_card, created_at)
		VALUES (0, 1, 20, 40, $1) RETURNING id`, now).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if offers, err := store.CardMarket(ctx, 2, market, 10); err != nil || len(offers) != 0 {
		t.Errorf("the market with an uneven offer = %+v, %v", offers, err)
	}
	if _, err := store.TakeCardOffer(ctx, old, 2, rules, market.Keep); !errors.Is(err, ErrCardTradeUneven) {
		t.Errorf("taking an uneven offer = %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE card_trades SET status = 'off' WHERE id = $1`, old); err != nil {
		t.Fatal(err)
	}
	var vegeta int
	_ = store.pool.QueryRow(ctx, `SELECT count(*) FROM card_owned WHERE user_id = 1 AND card_id = 20`).Scan(&vegeta)
	if vegeta != 1 {
		t.Errorf("Alice's Vegeta after the refused take = %d", vegeta)
	}
	// Alice's Goku and Bob's Kakashi, both Mythic, go back and forth
	// eight times the same day: whoever holds Goku puts it up, the other takes it.
	holder, other := int64(1), int64(2)
	for i := 1; i <= 8; i++ {
		id, err := store.OfferCard(ctx, holder, 10, 40, market)
		if err != nil {
			t.Fatalf("offer %d = %v", i, err)
		}
		if _, err := store.TakeCardOffer(ctx, id, other, rules, market.Keep); err != nil {
			t.Fatalf("trade %d = %v", i, err)
		}
		holder, other = other, holder
	}
}

// A limited card never changes hands: not given, not offered, not wanted,
// and an offer of one placed before is not listed nor taken.
func TestALimitedCardStaysPut(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (298761, 'Satoko Kirigaya', 4, 1, 1, 5000), (10, 'Goku', 4, 1, 1, 4000)`,
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 5000, 1, 3), (2, 'Bob', 4000, 1, 1)`,
		`INSERT INTO card_owned (user_id, card_id, count) VALUES (1, 298761, 3), (2, 10, 2)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	rules := CardTradeRules{Now: now}
	market := CardMarketRules{Open: 5, Keep: 7 * 24 * time.Hour, Now: now}
	if !CardLimited(298761) || CardLimited(10) {
		t.Fatal("limited cards are not as listed")
	}
	if err := store.GiveCard(ctx, 0, 1, 2, 298761, rules); !errors.Is(err, ErrCardLimited) {
		t.Errorf("a gift of a limited spare = %v", err)
	}
	if _, err := store.OfferCard(ctx, 1, 298761, 10, market); !errors.Is(err, ErrCardLimited) {
		t.Errorf("an offer of a limited card = %v", err)
	}
	if _, err := store.OfferCard(ctx, 2, 10, 298761, market); !errors.Is(err, ErrCardLimited) {
		t.Errorf("an offer for a limited card = %v", err)
	}
	var old int64
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO card_trades (chat_id, from_id, give_card, take_card, created_at)
		VALUES (0, 1, 298761, 10, $1) RETURNING id`, now).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if offers, err := store.CardMarket(ctx, 2, market, 10); err != nil || len(offers) != 0 {
		t.Errorf("the market with a limited offer = %+v, %v", offers, err)
	}
	if _, err := store.TakeCardOffer(ctx, old, 2, rules, market.Keep); !errors.Is(err, ErrCardLimited) {
		t.Errorf("taking a limited offer = %v", err)
	}
	var count int
	_ = store.pool.QueryRow(ctx, `SELECT count FROM card_owned WHERE user_id = 1 AND card_id = 298761`).Scan(&count)
	if count != 3 {
		t.Errorf("Alice's Satoko = %d", count)
	}
}

// The catalog pages through one rarity, the most loved first, without the
// card given and without limited cards, a name narrowing it.
func TestTheCatalogPagesThroughARarity(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO card_catalog (id, name, rarity, attack, health, value, favourites) VALUES
		(298761, 'Satoko Kirigaya', 4, 1, 1, 1, 9999), (1, 'Gojo', 4, 1, 1, 1, 500), (2, 'Goku', 4, 1, 1, 1, 400),
		(3, 'Luffy', 4, 1, 1, 1, 300), (4, 'Levi', 4, 1, 1, 1, 300), (5, 'Itachi', 4, 1, 1, 1, 100), (6, 'Naruto', 3, 1, 1, 1, 9000)`); err != nil {
		t.Fatal(err)
	}
	names := func(cards []Card) (out []string) {
		for _, c := range cards {
			out = append(out, c.Name)
		}
		return out
	}
	page := CatalogPage{Rarity: 4, Except: 2, Limit: 2}
	cards, more, err := store.CatalogCards(ctx, page)
	if err != nil || !more || fmt.Sprint(names(cards)) != "[Gojo Levi]" {
		t.Fatalf("first page = %v, %v, %v", names(cards), more, err)
	}
	page.Offset = 2
	if cards, more, _ = store.CatalogCards(ctx, page); more || fmt.Sprint(names(cards)) != "[Luffy Itachi]" {
		t.Errorf("second page = %v, %v", names(cards), more)
	}
	page = CatalogPage{Rarity: 4, Name: "GO", Limit: 10}
	if cards, more, _ = store.CatalogCards(ctx, page); more || fmt.Sprint(names(cards)) != "[Gojo Goku]" {
		t.Errorf("named go = %v, %v", names(cards), more)
	}
	page = CatalogPage{Rarity: 4, Name: "satoko", Limit: 10}
	if cards, _, _ = store.CatalogCards(ctx, page); len(cards) != 0 {
		t.Errorf("a limited card = %v", names(cards))
	}
}
