package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A gift moves one copy and its points; giving the last copy not melted
// takes the card out of the collection; a player gives at most the limit a
// day; a trade is done once, by the player it was offered to.
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
	rules := CardTradeRules{Limit: 2, Day: 24 * time.Hour, Now: time.Now()}
	held := func(user, card int64) (count, melted int) {
		_ = store.pool.QueryRow(ctx, `SELECT count, melted FROM card_owned WHERE user_id = $1 AND card_id = $2`, user, card).Scan(&count, &melted)
		return
	}
	player := func(user int64) (points int64, cards int) {
		_ = store.pool.QueryRow(ctx, `SELECT points, cards FROM card_players WHERE user_id = $1`, user).Scan(&points, &cards)
		return
	}

	found, err := store.HeldCardsNamed(ctx, 1, "LEV", 5)
	if err != nil || len(found) != 1 || found[0].ID != 20 {
		t.Fatalf("found = %+v, %v", found, err)
	}
	if found, _ = store.HeldCardsNamed(ctx, 1, "%", 5); len(found) != 0 {
		t.Errorf("a %% matched %+v", found)
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
	if p, n := player(1); p != 5300 || n != 3 {
		t.Errorf("Alice = %d points, %d cards", p, n)
	}
	if p, n := player(2); p != 2000 || n != 1 {
		t.Errorf("Bob = %d points, %d cards", p, n)
	}
	// Her last Levi not melted: the card leaves her collection.
	if err := store.GiveCard(ctx, -100, 1, 2, 20, rules); err != nil {
		t.Fatal(err)
	}
	if c, _ := held(1, 20); c != 0 {
		t.Errorf("Alice still holds Levi ×%d", c)
	}
	if _, n := player(1); n != 2 {
		t.Errorf("Alice holds %d cards", n)
	}
	var limit *CardGiveLimitError
	if err := store.GiveCard(ctx, -100, 1, 2, 30, rules); !errors.As(err, &limit) || limit.UserID != 1 {
		t.Errorf("a third gift = %v", err)
	}
	if err := store.GiveCard(ctx, -100, 2, 3, 20, rules); !errors.Is(err, ErrCardNoPlayer) {
		t.Errorf("a gift to one who never drew = %v", err)
	}
	if err := store.GiveCard(ctx, -100, 2, 1, 30, rules); !errors.Is(err, ErrCardNotHeld) {
		t.Errorf("a card not held = %v", err)
	}

	// Bob offers a Levi for Alice's Naruto.
	id, err := store.OfferCardTrade(ctx, -100, 2, 1, 20, 30, rules.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptCardTrade(ctx, id, 2, rules); !errors.Is(err, ErrCardTradeOver) {
		t.Errorf("accepted by the one who offered = %v", err)
	}
	if _, err := store.AcceptCardTrade(ctx, id, 1, rules); !errors.As(err, &limit) || limit.UserID != 1 {
		t.Errorf("Alice at her limit = %v", err)
	}
	rules.Now = rules.Now.Add(time.Hour)
	rules.Limit = 5
	trade, err := store.AcceptCardTrade(ctx, id, 1, rules)
	if err != nil || trade.Give.Name != "Levi" || trade.Take.Name != "Naruto Uzumaki" {
		t.Fatalf("trade = %+v, %v", trade, err)
	}
	if c, _ := held(1, 20); c != 1 {
		t.Errorf("Alice's Levi back = %d", c)
	}
	if c, _ := held(2, 30); c != 1 {
		t.Errorf("Bob's Naruto = %d", c)
	}
	if _, err := store.AcceptCardTrade(ctx, id, 1, rules); !errors.Is(err, ErrCardTradeOver) {
		t.Errorf("accepted twice = %v", err)
	}
	off, err := store.OfferCardTrade(ctx, -100, 2, 1, 30, 20, rules.Now)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.CallOffCardTrade(ctx, off, 3); err != nil || ok {
		t.Errorf("called off by a stranger = %v, %v", ok, err)
	}
	if ok, err := store.CallOffCardTrade(ctx, off, 1); err != nil || !ok {
		t.Errorf("called off = %v, %v", ok, err)
	}
	if _, err := store.AcceptCardTrade(ctx, off, 1, rules); !errors.Is(err, ErrCardTradeOver) {
		t.Errorf("a trade called off accepted = %v", err)
	}
}
