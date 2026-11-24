package storage

import (
	"context"
	"errors"
	"fmt"
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
	// The pictures migration 64 chose, and the cards migration 92 added,
	// are the real catalog's.
	for _, q := range []string{`DELETE FROM card_pictures`, `DELETE FROM card_catalog`} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
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
	open, err := store.OpenCards(ctx, alice, "Alice", 1, rarity(4), 15, day, now, CardQuest{})
	if err != nil || len(open.Draws) != 1 || open.Given != 2 || open.Tokens != 1 || open.Points != 5000 {
		t.Fatalf("first opening = %+v, %v", open, err)
	}
	if d := open.Draws[0]; d.Card.ID != 1 || !d.New || d.Count != 1 {
		t.Fatalf("first card = %+v", d)
	}
	if open, err = store.OpenCards(ctx, alice, "Alice", 1, rarity(4), 15, day, now.Add(time.Minute), CardQuest{}); err != nil || open.Given != 0 || open.Tokens != 0 || open.Draws[0].New || open.Draws[0].Count != 2 || open.Points != 5000 {
		t.Fatalf("the same card again = %+v, %v; want no points for the copy and no second gift", open, err)
	}
	// No token and no crystals: nothing opens, and it says what is short.
	_, err = store.OpenCards(ctx, alice, "Alice", 1, rarity(0), 15, day, now.Add(2*time.Hour), CardQuest{})
	var few *CardFundsError
	if !errors.As(err, &few) || few.Tokens != 0 || few.Shards != 0 || few.Need != 15 {
		t.Fatalf("a card with nothing to pay = %v", err)
	}
	// Ten at once: the token first, then crystals for the other nine.
	if _, err := store.pool.Exec(ctx, `UPDATE card_players SET tokens = 1, shards = 134 WHERE user_id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if _, err = store.OpenCards(ctx, alice, "Alice", 10, rarity(4), 15, day, now, CardQuest{}); !errors.As(err, &few) || few.Need != 135 || few.Shards != 134 {
		t.Fatalf("ten for one token and 134 crystals = %v; want refused, 135 needed", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE card_players SET shards = 140 WHERE user_id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if open, err = store.OpenCards(ctx, alice, "Alice", 10, rarity(4), 15, day, now, CardQuest{}); err != nil || len(open.Draws) != 10 || open.Tokens != 0 || open.Shards != 5 {
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
	if open, err = store.OpenCards(ctx, alice, "Alice", 1, rarity(4), 15, nextDay, start.AddDate(0, 0, 1).Add(time.Hour), CardQuest{}); err != nil || open.Tokens != 1 || open.Draws[0].Count != 13 {
		t.Fatalf("the next day's first = %+v, %v", open, err)
	}
	if open, err = store.OpenCards(ctx, bob, "Bob", 1, rarity(0), 15, CardDay{}, now, CardQuest{}); !errors.As(err, &few) {
		t.Fatalf("Bob with no day's tokens and nothing = %+v, %v", open, err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO card_players (user_id, name, tokens) VALUES ($1, 'Bob', 2)`, bob); err != nil {
		t.Fatal(err)
	}
	open, err = store.OpenCards(ctx, bob, "Bob", 1, rarity(0), 15, CardDay{}, now, CardQuest{})
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

// A batch counts each card's wait from the one before it: the roll of the
// card after a sure Legendary starts from nought again.
func TestOpenCardsCountsTheWaitInABatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{`DELETE FROM card_pictures`, `DELETE FROM card_catalog`} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	catalog := []Card{
		{ID: 1, Name: "Gojo", Anime: "JJK", Rarity: 3, Value: 2000},
		{ID: 2, Name: "Levi", Anime: "AoT", Rarity: 0, Value: 100},
	}
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	day := CardDay{Start: start, PerDay: 5}
	if _, err := store.GiveDayTokens(ctx, alice, "Alice", day); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE card_players SET since_legendary = 97, since_mythic = 10 WHERE user_id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	var seen []int
	roll := func(sinceLegendary, sinceMythic int) (int, bool) {
		seen = append(seen, sinceLegendary)
		if sinceLegendary+1 >= 100 {
			return 3, true
		}
		return 0, false
	}
	open, err := store.OpenCards(ctx, alice, "Alice", 5, roll, 15, day, start.Add(time.Hour), CardQuest{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{97, 98, 99, 0, 1}; fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("waits rolled = %v; want %v", seen, want)
	}
	if d := open.Draws[2]; d.Card.ID != 1 || !d.Guaranteed {
		t.Fatalf("third card = %+v; want the sure Legendary", d)
	}
	last := open.Draws[4]
	if last.SinceLegendary != 2 || last.SinceMythic != 15 {
		t.Fatalf("after the batch = %d, %d; want 2, 15", last.SinceLegendary, last.SinceMythic)
	}
	if p, _, _ := store.CardPlayerOf(ctx, alice); p.SinceLegendary != 2 || p.SinceMythic != 15 {
		t.Fatalf("the player's wait = %d, %d; want 2, 15", p.SinceLegendary, p.SinceMythic)
	}
}

// The catalog's stamp changes when a card is reworked, and not when one is
// only drawn, which every opening does.
func TestCardCatalogStamp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{`DELETE FROM card_pictures`, `DELETE FROM card_catalog`} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveCardCatalog(ctx, []Card{{ID: 1, Name: "Gojo", Anime: "JJK", ImageURL: "https://img/1.png", Rarity: 4, Value: 5000}}); err != nil {
		t.Fatal(err)
	}
	stamp := func() string {
		t.Helper()
		s, err := store.CardCatalogStamp(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := stamp()
	if _, err := store.pool.Exec(ctx, `UPDATE card_catalog SET drawn = drawn + 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if again := stamp(); again != first {
		t.Errorf("a draw changed the stamp: %q, then %q", first, again)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE card_catalog SET attack = 6000 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if reworked := stamp(); reworked == first {
		t.Errorf("a reworked card kept the stamp %q", first)
	}
}

// OwnedCounts gives the copies held of only the cards asked for, and
// leaves out a card never held.
func TestOwnedCounts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`DELETE FROM card_pictures`, `DELETE FROM card_catalog`,
		`INSERT INTO card_catalog (id, name, anime, image_url, favourites, rarity, attack, health, value, collection) VALUES
			(1, 'Gojo', 'JJK', '', 0, 4, 0, 0, 0, 'anime'), (2, 'Levi', 'AoT', '', 0, 0, 0, 0, 0, 'anime'), (3, 'Eren', 'AoT', '', 0, 0, 0, 0, 0, 'anime')`,
		`INSERT INTO card_owned (user_id, card_id, count) VALUES (77, 1, 3), (77, 2, 1), (78, 3, 5)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := store.OwnedCounts(ctx, 77, []int64{1, 3})
	if err != nil || len(counts) != 1 || counts[1] != 3 {
		t.Errorf("counts = %v, %v; want only card 1 with 3", counts, err)
	}
	if none, err := store.OwnedCounts(ctx, 77, nil); err != nil || len(none) != 0 {
		t.Errorf("no cards asked: %v, %v", none, err)
	}
}
