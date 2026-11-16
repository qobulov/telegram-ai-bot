package cards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// The odds add up to a thousand draws, the rarest least often, and the
// catalog's cards are parted 25, 50, 100, 150 and the rest by how loved
// they are, the catalog's 501 in proportion.
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
	if held[Mythic] != 25 || held[Legendary] != 50 || held[Epic] != 100 || held[Rare] != 150 || held[Common] != CatalogSize-325 {
		t.Errorf("catalog = %v", held)
	}
	if OfRank(0) != Mythic || OfRank(CatalogSize-1) != Common {
		t.Error("the most loved is not the rarest")
	}
}

// A character's card is the same every time, its numbers within its
// rarity's, rounded to tens.
func TestCardNumbers(t *testing.T) {
	a, b := Card(246, "Gokuu Son", "Dragon Ball Z", "https://x/y.png", 9000, 130), Card(246, "Gokuu Son", "Dragon Ball Z", "https://x/y.png", 9000, 130)
	if a != b || a.Rarity != int(Mythic) || a.Value != 4000 || a.Attack%10 != 0 || a.Attack < 4000 || a.Attack > 6000 || a.Health < 15000 || a.Health > 22000 {
		t.Errorf("card = %+v", a)
	}
}

// Mythic and Legendary are the characters chosen by hand, whatever their
// rank: Son Goku is Mythic at 130, Gojo Legendary at 1; anyone else is at
// most Epic, however loved.
func TestChosenRarities(t *testing.T) {
	if RarityOf(246, 130) != Mythic || RarityOf(17, 32) != Mythic || RarityOf(127691, 0) != Legendary || RarityOf(913, 190) != Legendary {
		t.Error("a chosen character's rarity")
	}
	if RarityOf(89334, 9) != Epic || RarityOf(99999, 400) != Common || RarityOf(88572, 7) != Mythic {
		t.Errorf("Reigen at 9 = %v, an unknown at 400 = %v, Emilia = %v", RarityOf(89334, 9), RarityOf(99999, 400), RarityOf(88572, 7))
	}
	if len(mythicIDs) != 25 || len(legendaryIDs) != 59 || len(epicIDs) != 8 {
		t.Errorf("chosen %d Mythic and %d Legendary", len(mythicIDs), len(legendaryIDs))
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

// No card is ever sure: the roll alone gives the rarity, however long the
// player waited.
func TestByOdds(t *testing.T) {
	common := ByOdds(func() Rarity { return Common })
	for _, since := range [][2]int{{0, 0}, {29, 99}, {500, 500}} {
		if r, sure := common(since[0], since[1]); Rarity(r) != Common || sure {
			t.Errorf("after %v draws: %d, %v; want Common, not sure", since, r, sure)
		}
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

// The day's free cards start at midnight in Tashkent: three each day, the
// next day's at the next midnight.
func TestThreeFreeCardsADay(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 30, 0, 0, time.UTC) // 01:30 in Tashkent
	if start := DayStart(now); !start.Equal(time.Date(2026, 11, 17, 0, 0, 0, 0, Location)) {
		t.Errorf("day start = %v", start)
	}
	player := storage.CardPlayer{FreeDay: DayStart(now), FreeUsed: 2}
	if FreeLeft(player, now) != 1 || !NextFree(player, now).Equal(now) {
		t.Errorf("one left = %d, next %v", FreeLeft(player, now), NextFree(player, now))
	}
	player.FreeUsed = 3
	if FreeLeft(player, now) != 0 || !NextFree(player, now).Equal(DayStart(now).AddDate(0, 0, 1)) {
		t.Errorf("none left = %d, next %v", FreeLeft(player, now), NextFree(player, now))
	}
	player.FreeDay = DayStart(now).AddDate(0, 0, -1)
	if FreeLeft(player, now) != PerDay {
		t.Errorf("yesterday's = %d left, want %d", FreeLeft(player, now), PerDay)
	}
}
