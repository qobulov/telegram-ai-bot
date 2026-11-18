package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An event pull costs a token, refused with too few and after the event;
// the 200th pull in a row without the event's card gives it and starts the
// count again, and a Mythic is the event's card when the pick says so.
func TestCardEventPulls(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const player = int64(42)
	const satoko = int64(298761)
	for _, q := range []string{`DELETE FROM card_pictures`, `DELETE FROM card_catalog`} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	catalog := []Card{
		{ID: satoko, Name: "Satoko Kirigaya", Anime: "Firefly Wedding", Rarity: 4, Value: 4000},
		{ID: 1, Name: "Gojo", Anime: "JJK", Rarity: 4, Value: 4000},
		{ID: 2, Name: "Levi", Anime: "AoT", Rarity: 0, Value: 100},
	}
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GiveDayTokens(ctx, player, "Ann", CardDay{}); err != nil {
		t.Fatal(err)
	}
	setTokens := func(n int) {
		if _, err := store.pool.Exec(ctx, `UPDATE card_players SET tokens = $2 WHERE user_id = $1`, player, n); err != nil {
			t.Fatal(err)
		}
	}
	end := time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)
	now := end.Add(-time.Hour)
	never := func() bool { return false }
	ev := CardEvent{Card: satoko, Ends: end, Pity: 200, Price: 1, Roll: rarity(0), Pick: never}

	setTokens(5)
	var few *CardTokensError
	if _, err := store.OpenEventCards(ctx, player, "Ann", 10, ev, CardDay{}, now); !errors.As(err, &few) || few.Tokens != 5 || few.Need != 10 {
		t.Fatalf("ten pulls with five tokens = %v %+v", err, few)
	}
	if _, err := store.OpenEventCards(ctx, player, "Ann", 1, ev, CardDay{}, end); !errors.Is(err, ErrCardEventOver) {
		t.Fatalf("a pull at the end = %v", err)
	}
	open, err := store.OpenEventCards(ctx, player, "Ann", 1, ev, CardDay{}, now)
	if err != nil || len(open.Draws) != 1 || open.Tokens != 4 || open.Pulls != 1 || open.Draws[0].Card.ID != 2 {
		t.Fatalf("one pull = %+v, %v", open, err)
	}

	// 199 pulls without her, and the 200th gives her, sure.
	setTokens(300)
	for range 19 {
		if open, err = store.OpenEventCards(ctx, player, "Ann", 10, ev, CardDay{}, now); err != nil {
			t.Fatal(err)
		}
	}
	if open.Pulls != 191 || open.Tokens != 110 {
		t.Fatalf("after 191 pulls = %d pulls, %d tokens", open.Pulls, open.Tokens)
	}
	if open, err = store.OpenEventCards(ctx, player, "Ann", 10, ev, CardDay{}, now); err != nil {
		t.Fatal(err)
	}
	for i, d := range open.Draws {
		if got := d.Card.ID == satoko; got != (i == 8) || d.Guaranteed != (i == 8) {
			t.Errorf("pull %d = %+v", 192+i, d)
		}
	}
	if open.Pulls != 0 || open.Tokens != 100 || open.Points != 4100 || !open.Draws[8].New {
		t.Fatalf("after the pity = %d pulls, %d tokens, %d points", open.Pulls, open.Tokens, open.Points)
	}

	// She is held once at most: holding her, the pick gives another Mythic
	// and the pity no longer counts.
	ev.Roll, ev.Pick = rarity(4), func() bool { return true }
	if open, err = store.OpenEventCards(ctx, player, "Ann", 10, ev, CardDay{}, now); err != nil {
		t.Fatal(err)
	}
	for i, d := range open.Draws {
		if d.Card.ID != 1 || d.Guaranteed {
			t.Errorf("Mythic pull %d, holding her = %d, want another Mythic", i, d.Card.ID)
		}
	}
	if open.Pulls != 0 || open.Tokens != 90 || open.Draws[9].SinceMythic != 0 {
		t.Fatalf("after the Mythic pulls = %+v", open)
	}
	var pulls, draws int
	if err := store.pool.QueryRow(ctx, `SELECT event_pulls, draws FROM card_players WHERE user_id = $1`, player).Scan(&pulls, &draws); err != nil || pulls != 0 || draws != 211 {
		t.Fatalf("event_pulls = %d, draws = %d, %v", pulls, draws, err)
	}
	if got, card, err := store.CardEventPulls(ctx, player, satoko); err != nil || got != 0 || card.Name != "Satoko Kirigaya" {
		t.Fatalf("CardEventPulls = %d %+v %v", got, card, err)
	}
	if got, _, err := store.CardEventPulls(ctx, 999, satoko); err != nil || got != 0 {
		t.Fatalf("a newcomer's pulls = %d %v", got, err)
	}
}

// A player new to the event gets her from a Mythic the pick names, once:
// the next Mythics the pick names are others.
func TestTheLimitedCardOncePerPlayer(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const satoko = int64(298761)
	catalog := []Card{
		{ID: 1, Name: "Gojo", Anime: "JJK", Rarity: 4, Value: 4000, Collection: "anime"},
		{ID: satoko, Name: "Satoko Kirigaya", Anime: "Firefly Wedding", Rarity: 4, Value: 4000, Collection: "anime"},
	}
	for _, q := range []string{`DELETE FROM card_pictures`, `DELETE FROM card_catalog`} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatal(err)
	}
	player := int64(4242)
	if _, err := store.pool.Exec(ctx, `INSERT INTO card_players (user_id, name, tokens) VALUES ($1, 'Ann', 10)`, player); err != nil {
		t.Fatal(err)
	}
	end := time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)
	ev := CardEvent{Card: satoko, Ends: end, Pity: 200, Price: 1, Roll: rarity(4), Pick: func() bool { return true }}
	open, err := store.OpenEventCards(ctx, player, "Ann", 10, ev, CardDay{}, end.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, d := range open.Draws {
		if d.Card.ID == satoko {
			got++
		}
	}
	if got != 1 || open.Draws[0].Card.ID != satoko || open.Pulls != 0 {
		t.Errorf("ten forced Mythic pulls gave her %d times (first %d), pulls %d; want once, first", got, open.Draws[0].Card.ID, open.Pulls)
	}
}
