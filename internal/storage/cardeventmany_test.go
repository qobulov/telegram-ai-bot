package storage

import (
	"context"
	"slices"
	"testing"
	"time"
)

// An event of several cards counts its own pulls apart from the Firefly
// event's; a Mythic the pick names is one of its cards, those the player
// lacks first, and a holder still gets them (it is not Once); its pity gives
// one too. No other pull gives a card only an event gives.
func TestAnEventOfSeveralCards(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const player, other = int64(77), int64(78)
	giyu, zenitsu := DemonSlayerCards[0], DemonSlayerCards[1]
	for _, q := range []string{`DELETE FROM card_pictures`, `DELETE FROM card_catalog`} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	catalog := []Card{
		{ID: giyu, Name: "Giyu", Anime: "Demon Slayer", Rarity: 4, Value: 4000},
		{ID: zenitsu, Name: "Zenitsu", Anime: "Demon Slayer", Rarity: 4, Value: 4000},
		{ID: 1, Name: "Gojo", Anime: "JJK", Rarity: 4, Value: 4000},
		{ID: 2, Name: "Levi", Anime: "AoT", Rarity: 0, Value: 100},
	}
	if err := store.SaveCardCatalog(ctx, catalog); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{player, other} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO card_players (user_id, name, tokens, event_pulls) VALUES ($1, 'Ann', 100, 9)`, id); err != nil {
			t.Fatal(err)
		}
	}
	end := time.Date(2026, 11, 28, 20, 0, 0, 0, time.UTC)
	now := end.Add(-time.Hour)
	pick := true
	ev := CardEvent{Key: "demon-slayer", Cards: []int64{giyu, zenitsu}, Ends: end, Pity: 300, Price: 1,
		Roll: rarity(4), Pick: func() bool { return pick }}

	open, err := store.OpenEventCards(ctx, player, "Ann", 2, ev, CardDay{}, now, CardQuest{})
	if err != nil {
		t.Fatal(err)
	}
	got := []int64{open.Draws[0].Card.ID, open.Draws[1].Card.ID}
	slices.Sort(got)
	want := []int64{giyu, zenitsu}
	slices.Sort(want)
	if !slices.Equal(got, want) || open.Pulls != 0 {
		t.Fatalf("two picked Mythics = %v, pulls %d; want one of each, the lacking first", got, open.Pulls)
	}
	if open, err = store.OpenEventCards(ctx, player, "Ann", 1, ev, CardDay{}, now, CardQuest{}); err != nil || !slices.Contains(ev.Cards, open.Draws[0].Card.ID) {
		t.Fatalf("a holder's picked Mythic = %+v %v", open, err)
	}

	pick = false
	if open, err = store.OpenEventCards(ctx, player, "Ann", 10, ev, CardDay{}, now, CardQuest{}); err != nil {
		t.Fatal(err)
	}
	for _, d := range open.Draws {
		if d.Card.ID != 1 {
			t.Fatalf("an unpicked Mythic = %+v, want Gojo, never an event card", d.Card)
		}
	}
	if got, cards, err := store.CardEventPulls(ctx, player, "demon-slayer", ev.Cards); err != nil || got != 10 || open.Pulls != 10 || len(cards) != 2 || cards[0].Name != "Giyu" {
		t.Fatalf("the event's pulls = %d %d %+v %v, want 10", got, open.Pulls, cards, err)
	}
	var fireflyPulls int
	if err := store.pool.QueryRow(ctx, `SELECT event_pulls FROM card_players WHERE user_id = $1`, player).Scan(&fireflyPulls); err != nil || fireflyPulls != 9 {
		t.Fatalf("the Firefly event's pulls = %d %v, want them untouched at 9", fireflyPulls, err)
	}

	if _, err := store.pool.Exec(ctx, `UPDATE card_event_pulls SET pulls = 299 WHERE user_id = $1`, player); err != nil {
		t.Fatal(err)
	}
	if open, err = store.OpenEventCards(ctx, player, "Ann", 1, ev, CardDay{}, now, CardQuest{}); err != nil || !open.Draws[0].Guaranteed || !slices.Contains(ev.Cards, open.Draws[0].Card.ID) || open.Pulls != 0 {
		t.Fatalf("the 300th pull = %+v %v, want a sure event card", open, err)
	}

	drawn, err := store.OpenCards(ctx, other, "Bob", 20, rarity(4), 0, CardDay{}, now, CardQuest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range drawn.Draws {
		if CardEventOnly(d.Card.ID) {
			t.Fatalf("a draw gave %+v, which only its event gives", d.Card)
		}
	}
}
