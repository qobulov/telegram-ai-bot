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

	open, err := store.OpenCards(ctx, alice, "Alice", 3, rarity(4), 15, CardDay{}, now, CardQuest{})
	if err != nil {
		t.Fatal(err)
	}
	want(open.Draws, gojo, 1)
	if open, err = store.OpenCardPack(ctx, bob, "Bob", 2, rarity(4), 0, now, CardQuest{}); err != nil {
		t.Fatal(err)
	}
	want(open.Draws, gojo, 4)

	end := now.Add(time.Hour)
	ev := CardEvent{Card: satoko, Ends: end, Pity: 130, Price: 1, Roll: rarity(4), Pick: func() bool { return true }}
	pulled, err := store.OpenEventCards(ctx, bob, "Bob", 2, ev, CardDay{}, now, CardQuest{})
	if err != nil {
		t.Fatal(err)
	}
	// She comes once to a player: Bob's second forced pick is another Mythic.
	want(pulled.Draws[:1], satoko, 1)
	want(pulled.Draws[1:], gojo, 6)
	ev.Pick = func() bool { return false }
	if pulled, err = store.OpenEventCards(ctx, alice, "Alice", 1, ev, CardDay{}, now, CardQuest{}); err != nil {
		t.Fatal(err)
	}
	want(pulled.Draws, gojo, 7)

	// Forced Mythics, 40 of them, are never her outside the event.
	for range 2 {
		if open, err = store.OpenCards(ctx, alice, "Alice", 10, rarity(4), 15, CardDay{}, now, CardQuest{}); err != nil {
			t.Fatal(err)
		}
		for _, d := range open.Draws {
			if d.Card.ID == satoko {
				t.Fatal("a draw gave the limited card")
			}
		}
		if open, err = store.OpenCardPack(ctx, bob, "Bob", 10, rarity(4), 0, now, CardQuest{}); err != nil {
			t.Fatal(err)
		}
		for _, d := range open.Draws {
			if d.Card.ID == satoko {
				t.Fatal("a pack gave the limited card")
			}
		}
	}
	want(open.Draws, gojo, 38)
}

// A limited card's holders are numbered by when each first got it: the
// first holder's copy is 1, the next 2; other cards have none.
func TestLimitedCardHoldersAreNumbered(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const satoko = int64(298761)
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO card_catalog (id, name, anime, image_url, favourites, rarity, attack, health, value, collection)
		VALUES (298761, 'Satoko Kirigaya', 'Firefly Wedding', '', 0, 4, 1, 1, 4000, 'anime'), (1, 'Gojo', 'JJK', '', 0, 4, 1, 1, 4000, 'anime')
		ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	for i, user := range []int64{alice, bob} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO card_players (user_id, name) VALUES ($1, 'P') ON CONFLICT DO NOTHING`, user); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO card_owned (user_id, card_id, count, first_at) VALUES ($1, $2, 1, now() - make_interval(mins => $3)), ($1, 1, 1, now())`,
			user, satoko, 10-i); err != nil {
			t.Fatal(err)
		}
	}
	for want, user := range map[int]int64{1: alice, 2: bob} {
		cards, err := store.OwnedCards(ctx, user, 4)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cards {
			if c.ID == satoko && c.Serial != want {
				t.Errorf("player %d's Satoko is No.%d, want %d", user, c.Serial, want)
			}
			if c.ID == 1 && c.Serial != 0 {
				t.Errorf("a common Mythic got number %d", c.Serial)
			}
		}
	}
}
