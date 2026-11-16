package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A player draws a card of the rarity asked for, again only after every,
// a card held again counting up and adding its points; the counts by
// rarity, the list and the table follow.
func TestCardsAreDrawnAndCollected(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	catalog := []Card{
		{ID: 1, Name: "Gojo", Anime: "JJK", ImageURL: "https://img/1.png", Favourites: 900, Rarity: 4, Attack: 5000, Health: 18000, Value: 5000},
		{ID: 2, Name: "Levi", Anime: "AoT", ImageURL: "https://img/2.png", Favourites: 800, Rarity: 0, Attack: 400, Health: 1500, Value: 100},
	}
	// The pictures migration 64 chose are for the real catalog's cards.
	if _, err := store.pool.Exec(ctx, `DELETE FROM card_pictures`); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatalf("saving the catalog again: %v", err)
	}
	if sizes, err := store.CardCollections(ctx); err != nil || sizes["anime"] != 2 {
		t.Fatalf("collections = %v, %v", sizes, err)
	}
	// A picture chosen by hand outlasts the catalog's being saved again.
	if _, err := store.pool.Exec(ctx, `INSERT INTO card_pictures (card_id, image_url) VALUES (2, 'https://cdn.myanimelist.net/levi.jpg')`); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatal(err)
	}
	picture := func(id int64) string {
		var link string
		_ = store.pool.QueryRow(ctx, `SELECT image_url FROM card_catalog WHERE id = $1`, id).Scan(&link)
		return link
	}
	if link := picture(2); link != "https://cdn.myanimelist.net/levi.jpg" {
		t.Fatalf("the chosen picture = %q", link)
	}
	if link := picture(1); link != "https://img/1.png" {
		t.Fatalf("picture = %q", link)
	}

	// Alice is given two tokens a day, here; a copy adds no points.
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	now := start.Add(10 * time.Hour)
	day := CardDay{Start: start, PerDay: 2}
	open, err := store.OpenCards(ctx, alice, "Alice", 1, rarity(4), 15, day, now)
	if err != nil || len(open.Draws) != 1 || open.Given != 2 || open.Tokens != 1 || open.Points != 5000 {
		t.Fatalf("first opening = %+v, %v", open, err)
	}
	if d := open.Draws[0]; d.Card.ID != 1 || !d.New || d.Count != 1 {
		t.Fatalf("first card = %+v", d)
	}
	if open, err = store.OpenCards(ctx, alice, "Alice", 1, rarity(4), 15, day, now.Add(time.Minute)); err != nil || open.Given != 0 || open.Tokens != 0 || open.Draws[0].New || open.Draws[0].Count != 2 || open.Points != 5000 {
		t.Fatalf("the same card again = %+v, %v; want no points for the copy and no second gift", open, err)
	}
	// No token and no crystals: nothing opens, and it says what is short.
	_, err = store.OpenCards(ctx, alice, "Alice", 1, rarity(0), 15, day, now.Add(2*time.Hour))
	var few *CardFundsError
	if !errors.As(err, &few) || few.Tokens != 0 || few.Shards != 0 || few.Need != 15 {
		t.Fatalf("a card with nothing to pay = %v", err)
	}
	// Ten at once: the token first, then crystals for the other nine.
	if _, err := store.pool.Exec(ctx, `UPDATE card_players SET tokens = 1, shards = 134 WHERE user_id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if _, err = store.OpenCards(ctx, alice, "Alice", 10, rarity(4), 15, day, now); !errors.As(err, &few) || few.Need != 135 || few.Shards != 134 {
		t.Fatalf("ten for one token and 134 crystals = %v; want refused, 135 needed", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE card_players SET shards = 140 WHERE user_id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if open, err = store.OpenCards(ctx, alice, "Alice", 10, rarity(4), 15, day, now); err != nil || len(open.Draws) != 10 || open.Tokens != 0 || open.Shards != 5 {
		t.Fatalf("ten for one token and 135 crystals = %+v, %v", open, err)
	}
	// The next day brings two tokens more.
	nextDay := CardDay{Start: start.AddDate(0, 0, 1), PerDay: 2}
	if given, err := store.GiveDayTokens(ctx, alice, "Alice", nextDay); err != nil || given != 2 {
		t.Fatalf("the next day's tokens = %d, %v", given, err)
	}
	if given, _ := store.GiveDayTokens(ctx, alice, "Alice", nextDay); given != 0 {
		t.Errorf("the day's tokens given twice: %d", given)
	}
	if p, _, _ := store.CardPlayerOf(ctx, alice); p.Tokens != 2 || !p.FreeDay.Equal(nextDay.Start) {
		t.Errorf("Alice after the next day's gift = %+v", p)
	}
	if open, err = store.OpenCards(ctx, alice, "Alice", 1, rarity(4), 15, nextDay, start.AddDate(0, 0, 1).Add(time.Hour)); err != nil || open.Tokens != 1 || open.Draws[0].Count != 13 {
		t.Fatalf("the next day's first = %+v, %v", open, err)
	}
	if open, err = store.OpenCards(ctx, bob, "Bob", 1, rarity(0), 15, CardDay{}, now); !errors.As(err, &few) {
		t.Fatalf("Bob with no day's tokens and nothing = %+v, %v", open, err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO card_players (user_id, name, tokens) VALUES ($1, 'Bob', 2)`, bob); err != nil {
		t.Fatal(err)
	}
	open, err = store.OpenCards(ctx, bob, "Bob", 1, rarity(0), 15, CardDay{}, now)
	if err != nil || open.Draws[0].Card.ID != 2 || open.Points != 100 {
		t.Fatalf("Bob's card = %+v, %v", open, err)
	}

	total, owned, points, err := store.CardCounts(ctx, alice)
	if err != nil || total[4] != 1 || total[0] != 1 || owned[4] != 1 || owned[0] != 0 || points != 5000 {
		t.Errorf("Alice's counts = %v %v %d, %v", total, owned, points, err)
	}
	cards, err := store.OwnedCards(ctx, alice, -1)
	if err != nil || len(cards) != 1 || cards[0].Name != "Gojo" || cards[0].Count != 13 {
		t.Errorf("Alice's cards = %+v, %v", cards, err)
	}
	if cards, err = store.OwnedCards(ctx, alice, 0); err != nil || len(cards) != 0 {
		t.Errorf("Alice's common cards = %+v, %v", cards, err)
	}
	top, err := store.CardTop(ctx, 5)
	if err != nil || len(top) != 2 || top[0].UserID != alice || top[0].Cards != 1 || top[1].Name != "Bob" {
		t.Errorf("top = %+v, %v", top, err)
	}
}

// rarity is a roll that always picks r.
func rarity(r int) CardRoll { return func(int, int) (int, bool) { return r, false } }
