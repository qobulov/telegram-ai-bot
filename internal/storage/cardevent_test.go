package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An event pull costs a token, refused with too few and after the event;
// the 50th pull in a row without the event's card gives it and starts the
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
	ev := CardEvent{Card: satoko, Ends: end, Pity: 50, Price: 1, Roll: rarity(0), Pick: never}

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

	// 49 pulls without her, and the 50th gives her, sure.
	setTokens(100)
	for range 4 {
		if open, err = store.OpenEventCards(ctx, player, "Ann", 10, ev, CardDay{}, now); err != nil {
			t.Fatal(err)
		}
	}
	if open.Pulls != 41 || open.Tokens != 60 {
		t.Fatalf("after 41 pulls = %d pulls, %d tokens", open.Pulls, open.Tokens)
	}
	if open, err = store.OpenEventCards(ctx, player, "Ann", 10, ev, CardDay{}, now); err != nil {
		t.Fatal(err)
	}
	for i, d := range open.Draws {
		if got := d.Card.ID == satoko; got != (i == 8) || d.Guaranteed != (i == 8) {
			t.Errorf("pull %d = %+v", 42+i, d)
		}
	}
	if open.Pulls != 1 || open.Tokens != 50 || open.Points != 4100 || !open.Draws[8].New {
		t.Fatalf("after the pity = %d pulls, %d tokens, %d points", open.Pulls, open.Tokens, open.Points)
	}

	// A Mythic is her when the pick says so, any other Mythic when not.
	picks := 0
	ev.Roll, ev.Pick = rarity(4), func() bool { picks++; return picks%2 == 1 }
	if open, err = store.OpenEventCards(ctx, player, "Ann", 10, ev, CardDay{}, now); err != nil {
		t.Fatal(err)
	}
	for i, d := range open.Draws {
		want := int64(1)
		if i%2 == 0 {
			want = satoko
		}
		if d.Card.ID != want || d.Guaranteed {
			t.Errorf("Mythic pull %d = %d, want %d", i, d.Card.ID, want)
		}
	}
	if open.Pulls != 1 || open.Tokens != 40 || open.Draws[9].SinceMythic != 0 {
		t.Fatalf("after the Mythic pulls = %+v", open)
	}
	var pulls, draws int
	if err := store.pool.QueryRow(ctx, `SELECT event_pulls, draws FROM card_players WHERE user_id = $1`, player).Scan(&pulls, &draws); err != nil || pulls != 1 || draws != 61 {
		t.Fatalf("event_pulls = %d, draws = %d, %v", pulls, draws, err)
	}
	if got, card, err := store.CardEventPulls(ctx, player, satoko); err != nil || got != 1 || card.Name != "Satoko Kirigaya" {
		t.Fatalf("CardEventPulls = %d %+v %v", got, card, err)
	}
	if got, _, err := store.CardEventPulls(ctx, 999, satoko); err != nil || got != 0 {
		t.Fatalf("a newcomer's pulls = %d %v", got, err)
	}
}
