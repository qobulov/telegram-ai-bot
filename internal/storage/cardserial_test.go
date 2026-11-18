package storage

import (
	"context"
	"testing"
	"time"
)

// Every card drawn is counted in its copies ever drawn, whoever draws it
// and however: a draw's Serial is that count with it. A limited card never
// comes from a draw or a pack, only from its event.
func TestCardSerials(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const alice, bob = int64(51), int64(52)
	const satoko, gojo = int64(298761), int64(1)
	for _, q := range []string{`DELETE FROM card_pictures`, `DELETE FROM card_catalog`} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	catalog := []Card{
		{ID: satoko, Name: "Satoko Kirigaya", Anime: "Firefly Wedding", Rarity: 4, Value: 4000},
		{ID: gojo, Name: "Gojo", Anime: "JJK", Rarity: 4, Value: 4000},
	}
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{alice, bob} {
		if _, err := store.GiveDayTokens(ctx, id, "P", CardDay{}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `UPDATE card_players SET tokens = 100, shards = 1000 WHERE user_id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	want := func(draws []CardDraw, card int64, first int) {
		t.Helper()
		for i, d := range draws {
			if d.Card.ID != card || d.Serial != first+i {
				t.Errorf("draw %d = card %d, serial %d; want card %d, serial %d", i, d.Card.ID, d.Serial, card, first+i)
			}
		}
	}

	open, err := store.OpenCards(ctx, alice, "Alice", 3, rarity(4), 15, CardDay{}, now)
	if err != nil {
		t.Fatal(err)
	}
	want(open.Draws, gojo, 1)
	if open, err = store.OpenCardPack(ctx, bob, "Bob", 2, rarity(4), 0, now); err != nil {
		t.Fatal(err)
	}
	want(open.Draws, gojo, 4)

	end := now.Add(time.Hour)
	ev := CardEvent{Card: satoko, Ends: end, Pity: 50, Price: 1, Roll: rarity(4), Pick: func() bool { return true }}
	pulled, err := store.OpenEventCards(ctx, bob, "Bob", 2, ev, CardDay{}, now)
	if err != nil {
		t.Fatal(err)
	}
	want(pulled.Draws, satoko, 1)
	ev.Pick = func() bool { return false }
	if pulled, err = store.OpenEventCards(ctx, alice, "Alice", 1, ev, CardDay{}, now); err != nil {
		t.Fatal(err)
	}
	want(pulled.Draws, gojo, 6)

	// Forced Mythics, 40 of them, are never her outside the event.
	for range 2 {
		if open, err = store.OpenCards(ctx, alice, "Alice", 10, rarity(4), 15, CardDay{}, now); err != nil {
			t.Fatal(err)
		}
		for _, d := range open.Draws {
			if d.Card.ID == satoko {
				t.Fatal("a draw gave the limited card")
			}
		}
		if open, err = store.OpenCardPack(ctx, bob, "Bob", 10, rarity(4), 0, now); err != nil {
			t.Fatal(err)
		}
		for _, d := range open.Draws {
			if d.Card.ID == satoko {
				t.Fatal("a pack gave the limited card")
			}
		}
	}
	want(open.Draws, gojo, 37)
}
