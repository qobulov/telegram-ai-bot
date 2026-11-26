package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

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
	if err != nil || len(offers) != 2 || offers[0].ID != gojo || !offers[0].Can || offers[0].Mine || offers[0].FromName != "Alice" || offers[1].Want.ID == 0 {
		t.Fatalf("Bob's market = %+v, %v", offers, err)
	}
	if offers, _ = store.CardMarket(ctx, 1, market, 10); !offers[0].Mine || offers[0].Can {
		t.Errorf("Alice's market = %+v", offers[0])
	}
	if _, err := store.TakeCardOffer(ctx, levi, 1, rules, market.Keep, CardQuest{}); !errors.Is(err, ErrCardOfferOwn) {
		t.Errorf("taking one's own offer = %v", err)
	}
	trade, err := store.TakeCardOffer(ctx, levi, 2, rules, market.Keep, CardQuest{})
	if err != nil || trade.Give.Name != "Levi" || trade.Take.Name != "Hinata Hyuuga" || trade.From != 1 || trade.To != 2 {
		t.Fatalf("trade = %+v, %v", trade, err)
	}
	if _, err := store.TakeCardOffer(ctx, levi, 2, rules, market.Keep, CardQuest{}); !errors.Is(err, ErrCardTradeOver) {
		t.Errorf("taken twice = %v", err)
	}
	// Bob holds no Hinata now: he cannot take the Gojo offer.
	if _, err := store.TakeCardOffer(ctx, gojo, 2, rules, market.Keep, CardQuest{}); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("taking without the card wanted = %v", err)
	}
	// Alice gives her Gojo away elsewhere: her offer of it is called off.
	if _, err := store.pool.Exec(ctx, `DELETE FROM card_owned WHERE user_id = 1 AND card_id = 10`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO card_owned (user_id, card_id) VALUES (2, 30)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TakeCardOffer(ctx, gojo, 2, rules, market.Keep, CardQuest{}); !errors.Is(err, ErrCardOfferGone) {
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

// A trade of cards two rarities apart or more is refused, as an offer and
// as a take; one rarity apart, up or down, is listed and done; two players
// trade with each other as often as they like.
func TestTradesAreEvenAndUnlimited(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (10, 'Goku', 4, 1, 1, 4000), (20, 'Vegeta', 3, 1, 1, 2000), (30, 'Extra', 0, 1, 1, 100), (40, 'Kakashi', 4, 1, 1, 2000), (50, 'Gohan', 2, 1, 1, 500)`,
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 6600, 4, 4), (2, 'Bob', 4000, 2, 2)`,
		`INSERT INTO card_owned (user_id, card_id) VALUES (1, 10), (1, 20), (1, 30), (1, 50), (2, 40)`,
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
	if _, err := store.OfferCard(ctx, 1, 50, 40, market); !errors.Is(err, ErrCardTradeUneven) {
		t.Errorf("an Epic put up for a Mythic = %v", err)
	}
	// An offer placed before, Gohan for Kakashi, is not listed, and taking
	// it is refused with the cards left where they were.
	var old int64
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO card_trades (chat_id, from_id, give_card, take_card, created_at)
		VALUES (0, 1, 50, 40, $1) RETURNING id`, now).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if offers, err := store.CardMarket(ctx, 2, market, 10); err != nil || len(offers) != 0 {
		t.Errorf("the market with an uneven offer = %+v, %v", offers, err)
	}
	if _, err := store.TakeCardOffer(ctx, old, 2, rules, market.Keep, CardQuest{}); !errors.Is(err, ErrCardTradeUneven) {
		t.Errorf("taking an uneven offer = %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE card_trades SET status = 'off' WHERE id = $1`, old); err != nil {
		t.Fatal(err)
	}
	var gohan int
	_ = store.pool.QueryRow(ctx, `SELECT count(*) FROM card_owned WHERE user_id = 1 AND card_id = 50`).Scan(&gohan)
	if gohan != 1 {
		t.Errorf("Alice's Gohan after the refused take = %d", gohan)
	}
	// Alice's Goku and Bob's Kakashi, both Mythic, go back and forth
	// eight times the same day: whoever holds Goku puts it up, the other takes it.
	holder, other := int64(1), int64(2)
	for i := 1; i <= 8; i++ {
		id, err := store.OfferCard(ctx, holder, 10, 40, market)
		if err != nil {
			t.Fatalf("offer %d = %v", i, err)
		}
		if _, err := store.TakeCardOffer(ctx, id, other, rules, market.Keep, CardQuest{}); err != nil {
			t.Fatalf("trade %d = %v", i, err)
		}
		holder, other = other, holder
	}
	// One rarity up: Alice's Legendary Vegeta for Bob's Mythic Kakashi is
	// listed for Bob and done.
	up, err := store.OfferCard(ctx, 1, 20, 40, market)
	if err != nil {
		t.Fatalf("a Legendary put up for a Mythic = %v", err)
	}
	if offers, err := store.CardMarket(ctx, 2, market, 10); err != nil || len(offers) != 1 || offers[0].ID != up || !offers[0].Can {
		t.Fatalf("Bob's market with an offer one rarity apart = %+v, %v", offers, err)
	}
	trade, err := store.TakeCardOffer(ctx, up, 2, rules, market.Keep, CardQuest{})
	if err != nil || trade.Give.Name != "Vegeta" || trade.Take.Name != "Kakashi" {
		t.Fatalf("a trade one rarity apart = %+v, %v", trade, err)
	}
	// One rarity down: Bob's Vegeta for Alice's Epic Gohan.
	if _, err := store.OfferCard(ctx, 2, 20, 50, market); err != nil {
		t.Errorf("a Legendary put up for an Epic = %v", err)
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
	if _, err := store.TakeCardOffer(ctx, old, 2, rules, market.Keep, CardQuest{}); !errors.Is(err, ErrCardLimited) {
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
	// Migrations seed some Mythics (the Four Knights' Lancelot and Tristan);
	// the pages here are of this test's cards alone.
	if _, err := store.pool.Exec(ctx, `DELETE FROM card_catalog WHERE rarity = 4`); err != nil {
		t.Fatal(err)
	}
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

// With Open 0 a player may have as many offers open as they like.
func TestTheCardMarketWithoutALimit(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) SELECT g, 'Card ' || g, 1, 1, 1, 100 FROM generate_series(1, 8) g`,
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 700, 7, 7)`,
		`INSERT INTO card_owned (user_id, card_id) SELECT 1, g FROM generate_series(1, 7) g`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	market := CardMarketRules{Open: 0, Keep: 7 * 24 * time.Hour, Now: time.Now()}
	for card := int64(1); card <= 7; card++ {
		if _, err := store.OfferCard(ctx, 1, card, 8, market); err != nil {
			t.Fatalf("offer %d: %v", card, err)
		}
	}
}

// A player's own offers are listed apart from the others' limit: on a
// market of hundreds they came last, never takeable by their player, and
// fell off it.
func TestOwnOffersOutlastTheMarketLimit(t *testing.T) {
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
	market := CardMarketRules{Keep: 7 * 24 * time.Hour, Now: now}
	for _, o := range [][3]int64{{1, 10, 30}, {1, 20, 30}, {2, 30, 10}} {
		if _, err := store.OfferCard(ctx, o[0], o[1], o[2], market); err != nil {
			t.Fatal(err)
		}
	}
	offers, err := store.CardMarket(ctx, 2, market, 1)
	if err != nil || len(offers) != 2 || !offers[0].Mine || offers[1].Mine || !offers[1].Can {
		t.Fatalf("Bob's market, the others' limit 1 = %+v, %v", offers, err)
	}
}
