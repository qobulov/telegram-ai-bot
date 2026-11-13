package cards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"telegram-ai-bot/internal/storage"
)

// The odds add up to a thousand draws, the rarest least often, and the
// catalog's cards are parted 15, 30, 45, 60 and the rest by how loved they
// are.
func TestRarityRules(t *testing.T) {
	drawn := map[Rarity]int{}
	for n := range 1000 {
		drawn[Roll(n)]++
	}
	if drawn[Mythic] != 15 || drawn[Legendary] != 65 || drawn[Epic] != 140 || drawn[Rare] != 280 || drawn[Common] != 500 {
		t.Errorf("odds = %v", drawn)
	}
	held := map[Rarity]int{}
	for rank := range CatalogSize {
		held[OfRank(rank)]++
	}
	if held[Mythic] != 15 || held[Legendary] != 30 || held[Epic] != 45 || held[Rare] != 60 || held[Common] != CatalogSize-150 {
		t.Errorf("catalog = %v", held)
	}
	if OfRank(0) != Mythic || OfRank(CatalogSize-1) != Common {
		t.Error("the most loved is not the rarest")
	}
}

// A character's card is the same every time, its numbers within its
// rarity's, rounded to tens.
func TestCardNumbers(t *testing.T) {
	a, b := Card(127691, "Satoru Gojou", "JJK", "https://x/y.png", 39194, 0), Card(127691, "Satoru Gojou", "JJK", "https://x/y.png", 39194, 0)
	if a != b || a.Rarity != int(Mythic) || a.Value != 5000 || a.Attack%10 != 0 || a.Attack < 4000 || a.Attack > 6000 || a.Health < 15000 || a.Health > 22000 {
		t.Errorf("card = %+v", a)
	}
}

// The catalog is AniList's most loved characters, page by page, with their
// anime's English title, else its romaji one; one with no picture is left out.
func TestFetchCatalog(t *testing.T) {
	var pages int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct{ Page, PerPage int } `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		pages++
		var characters []map[string]any
		for i := range req.Variables.PerPage {
			id := (req.Variables.Page-1)*req.Variables.PerPage + i + 1
			image := fmt.Sprintf("https://img/%d.png", id)
			if id == 3 {
				image = ""
			}
			characters = append(characters, map[string]any{
				"id": id, "name": map[string]string{"full": fmt.Sprintf(" Hero %d ", id)}, "image": map[string]string{"large": image},
				"favourites": 100000 - id, "media": map[string]any{"nodes": []any{map[string]any{"title": map[string]string{"romaji": "Romaji", "english": ""}}}},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Page": map[string]any{"characters": characters}}})
	}))
	defer server.Close()

	catalog, err := FetchCatalog(context.Background(), server.Client(), server.URL)
	if err != nil || len(catalog) != CatalogSize || pages != (CatalogSize+49)/50+1 {
		t.Fatalf("catalog of %d in %d pages, %v", len(catalog), pages, err)
	}
	if catalog[0].Name != "Hero 1" || catalog[0].Anime != "Romaji" || catalog[2].ID != 4 || catalog[0].Rarity != int(Mythic) || catalog[CatalogSize-1].Rarity != int(Common) {
		t.Errorf("first cards = %+v %+v %+v", catalog[0], catalog[2], catalog[CatalogSize-1])
	}
}

// A long wait for a rare card ends in one: the 30th draw without a Legendary
// or better is one, the 100th without a Mythic is one, and a roll better
// than the sure one is kept.
func TestWithPity(t *testing.T) {
	common := WithPity(func() Rarity { return Common })
	for _, tc := range []struct {
		sinceLegendary, sinceMythic int
		want                        Rarity
		sure                        bool
	}{
		{0, 0, Common, false},
		{LegendaryPity - 2, 50, Common, false},
		{LegendaryPity - 1, 50, Legendary, true},
		{5, MythicPity - 1, Mythic, true},
		{LegendaryPity - 1, MythicPity - 1, Mythic, true},
	} {
		r, sure := common(tc.sinceLegendary, tc.sinceMythic)
		if Rarity(r) != tc.want || sure != tc.sure {
			t.Errorf("after %d and %d draws: %d, %v; want %d, %v", tc.sinceLegendary, tc.sinceMythic, r, sure, tc.want, tc.sure)
		}
	}
	if r, sure := WithPity(func() Rarity { return Mythic })(LegendaryPity-1, 0); Rarity(r) != Mythic || sure {
		t.Errorf("a Mythic roll on a sure Legendary = %d, %v", r, sure)
	}
	if l, m := PityIn(storage.CardDraw{SinceLegendary: 4, SinceMythic: 40}); l != LegendaryPity-4 || m != MythicPity-40 {
		t.Errorf("PityIn = %d, %d", l, m)
	}
}

// catalogBook is a catalog of anime cards alone, which records what is
// saved to it.
type catalogBook struct {
	held  []storage.Card
	saved []storage.Card
}

func (b *catalogBook) CardCatalog(context.Context) ([]storage.Card, error) { return b.held, nil }
func (b *catalogBook) CardCollections(context.Context) (map[string]int, error) {
	return map[string]int{"anime": len(b.held)}, nil
}
func (b *catalogBook) SaveCardCatalog(_ context.Context, cards []storage.Card) error {
	b.saved = append(b.saved, cards...)
	return nil
}

// A larger catalog adds the characters it lacks and leaves the cards there
// are as players hold them, however AniList's ranks moved.
func TestEnsureAddsOnlyNewCards(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct{ Page, PerPage int } `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var characters []map[string]any
		for i := range req.Variables.PerPage {
			id := (req.Variables.Page-1)*req.Variables.PerPage + i + 1
			characters = append(characters, map[string]any{
				"id": id, "name": map[string]string{"full": fmt.Sprintf("Hero %d", id)}, "image": map[string]string{"large": "https://img/x.png"},
				"favourites": 100000 - id, "media": map[string]any{"nodes": []any{}},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Page": map[string]any{"characters": characters}}})
	}))
	defer server.Close()
	kept := storage.Card{ID: 1, Name: "Hero 1", Rarity: int(Legendary), Collection: "anime"}
	book := &catalogBook{held: []storage.Card{kept}}
	catalog := &Catalog{Store: book, Client: server.Client(), AniList: server.URL}
	if err := catalog.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(book.saved) != CatalogSize-1 {
		t.Fatalf("saved %d cards, want %d", len(book.saved), CatalogSize-1)
	}
	for _, c := range book.saved {
		if c.ID == kept.ID {
			t.Fatalf("the card there was saved again: %+v", c)
		}
	}
}
