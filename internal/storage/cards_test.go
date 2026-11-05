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
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatalf("saving the catalog again: %v", err)
	}
	if sizes, err := store.CardCollections(ctx); err != nil || sizes["anime"] != 2 {
		t.Fatalf("collections = %v, %v", sizes, err)
	}
	if link, err := store.CardImage(ctx, 1); err != nil || link != "https://img/1.png" {
		t.Fatalf("picture = %q, %v", link, err)
	}
	if link, err := store.CardImage(ctx, 99); err != nil || link != "" {
		t.Fatalf("no such card's picture = %q, %v", link, err)
	}

	now := time.Now()
	draw, err := store.DrawCard(ctx, alice, "Alice", 4, time.Hour, now)
	if err != nil || draw.Card.ID != 1 || !draw.New || draw.Count != 1 || draw.Points != 5000 {
		t.Fatalf("first draw = %+v, %v", draw, err)
	}
	_, err = store.DrawCard(ctx, alice, "Alice", 0, time.Hour, now.Add(10*time.Minute))
	var wait *CardWaitError
	if !errors.As(err, &wait) || wait.Wait.Round(time.Second) != 50*time.Minute {
		t.Fatalf("a draw ten minutes later = %v, want a 50-minute wait", err)
	}
	if draw, err = store.DrawCard(ctx, alice, "Alice", 4, time.Hour, now.Add(time.Hour)); err != nil || draw.New || draw.Count != 2 || draw.Points != 10000 {
		t.Fatalf("the same card again = %+v, %v", draw, err)
	}
	if draw, err = store.DrawCard(ctx, bob, "Bob", 0, 0, now); err != nil || draw.Card.ID != 2 || draw.Points != 100 {
		t.Fatalf("Bob's draw = %+v, %v", draw, err)
	}

	total, owned, points, err := store.CardCounts(ctx, alice)
	if err != nil || total[4] != 1 || total[0] != 1 || owned[4] != 1 || owned[0] != 0 || points != 10000 {
		t.Errorf("Alice's counts = %v %v %d, %v", total, owned, points, err)
	}
	cards, err := store.OwnedCards(ctx, alice, -1)
	if err != nil || len(cards) != 1 || cards[0].Name != "Gojo" || cards[0].Count != 2 {
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
