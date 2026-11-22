package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

var fuseCost = []int{0, 1, 2, 3, 4}

// Fusion uses up free copies of a card for a star: four copies reach ★3,
// eleven reach ★5 and no further; a card not held is not fused.
func TestFuseCard(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (10, 'Gojo Satoru', 4, 5000, 18000, 5000), (20, 'Levi', 3, 1, 1, 1000)`,
		`INSERT INTO card_players (user_id, name, draws) VALUES (1, 'Alice', 15)`,
		`INSERT INTO card_owned (user_id, card_id, count) VALUES (1, 10, 4), (1, 20, 11)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	held := func(id int64) OwnedCard {
		t.Helper()
		owned, err := store.OwnedCards(ctx, 1, -1)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range owned {
			if o.ID == id {
				return o
			}
		}
		t.Fatalf("card %d not held", id)
		return OwnedCard{}
	}
	if o := held(10); o.Stars != 1 {
		t.Errorf("a card drawn is ★%d", o.Stars)
	}
	for want := 2; want <= 3; want++ {
		if stars, err := store.FuseCard(ctx, 1, 10, fuseCost, CardQuest{}); err != nil || stars != want {
			t.Fatalf("fused = ★%d, %v; want ★%d", stars, err, want)
		}
	}
	if o := held(10); o.Stars != 3 || o.Melted != 3 || o.Count != 4 {
		t.Errorf("after two fusions = %+v", o)
	}
	if _, err := store.FuseCard(ctx, 1, 10, fuseCost, CardQuest{}); !errors.Is(err, ErrCardFewCopies) {
		t.Errorf("fused with too few copies = %v", err)
	}
	for want := 2; want <= 5; want++ {
		if stars, err := store.FuseCard(ctx, 1, 20, fuseCost, CardQuest{}); err != nil || stars != want {
			t.Fatalf("Levi fused = ★%d, %v; want ★%d", stars, err, want)
		}
	}
	if _, err := store.FuseCard(ctx, 1, 20, fuseCost, CardQuest{}); !errors.Is(err, ErrCardMaxStars) {
		t.Errorf("fused past ★5 = %v", err)
	}
	if o := held(20); o.Stars != 5 || o.Melted != 10 {
		t.Errorf("Levi at ★5 = %+v", o)
	}
	if _, err := store.FuseCard(ctx, 2, 10, fuseCost, CardQuest{}); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("fused a card not held = %v", err)
	}
}

// A card raised above ★1 stays with its holder, spare copies or none: an
// offer giving it, once fused, is called off on take (offer gone, not
// "fused") and left off the market, and it is not offered again.
func TestFuseThenOfferedCopyIsGone(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (10, 'Gojo Satoru', 3, 1, 1, 5000), (20, 'Levi', 3, 1, 1, 1000)`,
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 5000, 1, 2), (2, 'Bob', 1000, 1, 1)`,
		`INSERT INTO card_owned (user_id, card_id, count) VALUES (1, 10, 2), (2, 20, 1)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	market := CardMarketRules{Open: 5, Keep: 7 * 24 * time.Hour, Now: now}
	offer, err := store.OfferCard(ctx, 1, 10, 20, market)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FuseCard(ctx, 1, 10, fuseCost, CardQuest{}); err != nil {
		t.Fatal(err)
	}
	// Copies drawn again are spare, yet the card stays: it is starred.
	if _, err := store.pool.Exec(ctx, `UPDATE card_owned SET count = 4 WHERE user_id = 1 AND card_id = 10`); err != nil {
		t.Fatal(err)
	}
	if offers, err := store.CardMarket(ctx, 2, market, 10); err != nil || len(offers) != 0 {
		t.Errorf("the market shows a starred card: %+v, %v", offers, err)
	}
	if _, err := store.TakeCardOffer(ctx, offer, 2, CardTradeRules{Now: now}, market.Keep, CardQuest{}); !errors.Is(err, ErrCardOfferGone) {
		t.Errorf("taking a starred card = %v, want the offer gone", err)
	}
	var status string
	if err := store.pool.QueryRow(ctx, `SELECT status FROM card_trades WHERE id = $1`, offer).Scan(&status); err != nil || status != "off" {
		t.Errorf("the offer of a starred card = %q, %v, want off", status, err)
	}
	if _, err := store.OfferCard(ctx, 1, 10, 20, market); !errors.Is(err, ErrCardFused) {
		t.Errorf("offering a starred card with spare copies = %v", err)
	}
	var count, stars int
	if err := store.pool.QueryRow(ctx, `SELECT count, stars FROM card_owned WHERE user_id = 1 AND card_id = 10`).Scan(&count, &stars); err != nil || count != 4 || stars != 2 {
		t.Errorf("Alice's Gojo = ×%d ★%d, %v", count, stars, err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM card_owned WHERE user_id = 2 AND card_id = 10`).Scan(&count); err != nil || count != 0 {
		t.Errorf("Bob holds Gojo: %d, %v", count, err)
	}
}

// A taker whose own copy of the card wanted is starred cannot take the
// offer: the market says so (not Can, Starred), and a take is refused,
// ErrCardFused, with the offer left open and the cards where they were.
func TestAStarredCardIsNotGivenForAnOffer(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (10, 'Gojo Satoru', 3, 1, 1, 5000), (20, 'Levi', 3, 1, 1, 1000)`,
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 5000, 1, 1), (2, 'Bob', 1000, 1, 3)`,
		`INSERT INTO card_owned (user_id, card_id, count, stars) VALUES (1, 10, 1, 1), (2, 20, 3, 2)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	market := CardMarketRules{Keep: 7 * 24 * time.Hour, Now: now}
	offer, err := store.OfferCard(ctx, 1, 10, 20, market)
	if err != nil {
		t.Fatal(err)
	}
	offers, err := store.CardMarket(ctx, 2, market, 10)
	if err != nil || len(offers) != 1 || offers[0].Can || !offers[0].Starred {
		t.Fatalf("Bob's market = %+v, %v; want the offer, not his to take", offers, err)
	}
	if _, err := store.TakeCardOffer(ctx, offer, 2, CardTradeRules{Now: now}, market.Keep, CardQuest{}); !errors.Is(err, ErrCardFused) {
		t.Errorf("giving a starred card for an offer = %v", err)
	}
	var status string
	if err := store.pool.QueryRow(ctx, `SELECT status FROM card_trades WHERE id = $1`, offer).Scan(&status); err != nil || status != "open" {
		t.Errorf("the offer = %q, %v, want open", status, err)
	}
	var count int
	if err := store.pool.QueryRow(ctx, `SELECT count FROM card_owned WHERE user_id = 1 AND card_id = 10`).Scan(&count); err != nil || count != 1 {
		t.Errorf("Alice's Gojo = ×%d, %v", count, err)
	}
}

// A reset takes a card back to ★1 for free: the copies its stars used are
// free again, those melted into shards before stay gone; a card at ★1 or not
// held is refused.
func TestResetCardStars(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES (10, 'Gojo Satoru', 4, 5000, 18000, 4000), (20, 'Levi', 3, 1, 1, 2000)`,
		`INSERT INTO card_players (user_id, name, points, cards) VALUES (1, 'Alice', 6000, 2)`,
		`INSERT INTO card_owned (user_id, card_id, count, melted, stars) VALUES (1, 10, 13, 12, 5), (1, 20, 3, 0, 1)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	cost := []int{0, 1, 2, 3, 4}
	// ★5 used 1+2+3+4 = 10 copies; 2 more were melted into shards before.
	free, err := store.ResetCardStars(ctx, 1, 10, cost)
	if err != nil || free != 10 {
		t.Fatalf("reset = %d, %v; want 10 free copies", free, err)
	}
	var count, melted, stars, points int
	if err := store.pool.QueryRow(ctx, `SELECT o.count, o.melted, o.stars, p.points FROM card_owned o JOIN card_players p ON p.user_id = o.user_id WHERE o.user_id = 1 AND o.card_id = 10`).Scan(&count, &melted, &stars, &points); err != nil {
		t.Fatal(err)
	}
	if count != 13 || melted != 2 || stars != 1 || points != 6000 {
		t.Errorf("after the reset: count %d melted %d stars %d points %d", count, melted, stars, points)
	}
	if _, err := store.ResetCardStars(ctx, 1, 20, cost); !errors.Is(err, ErrCardNotFused) {
		t.Errorf("a reset at ★1 = %v, want ErrCardNotFused", err)
	}
	if _, err := store.ResetCardStars(ctx, 1, 99, cost); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("a reset of a card not held = %v, want ErrCardNotHeld", err)
	}
}
