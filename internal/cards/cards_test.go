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
// they are, the rest Common.
func TestRarityRules(t *testing.T) {
	drawn := map[Rarity]int{}
	for n := range 1000 {
		drawn[Roll(n)]++
	}
	if drawn[Mythic] != 5 || drawn[Legendary] != 25 || drawn[Epic] != 90 || drawn[Rare] != 250 || drawn[Common] != 630 {
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
// rank: Son Goku is Mythic at 130, Gojo Mythic at 1, Sasuke Legendary; anyone else is at
// most Epic, however loved.
func TestChosenRarities(t *testing.T) {
	if RarityOf(246, 130) != Mythic || RarityOf(17, 32) != Mythic || RarityOf(127691, 0) != Mythic || RarityOf(90202, 50) != Mythic || RarityOf(913, 190) != Legendary {
		t.Error("a chosen character's rarity")
	}
	if RarityOf(99998, 9) != Epic || RarityOf(99999, 400) != Common || RarityOf(88572, 7) != Mythic {
		t.Errorf("an unlisted one at 9 = %v, an unknown at 400 = %v, Emilia = %v", RarityOf(99998, 9), RarityOf(99999, 400), RarityOf(88572, 7))
	}
	if len(mythicIDs) != 32 || len(legendaryIDs) != 99 || RarityOf(129928, 0) != Mythic || RarityOf(89334, 9) != Legendary || RarityOf(203633, 900) != Mythic || RarityOf(341135, 900) != Legendary || len(epicIDs) != 7 || RarityOf(298761, 900) != Mythic || RarityOf(298762, 900) != Epic {
		t.Errorf("chosen %d Mythic and %d Legendary", len(mythicIDs), len(legendaryIDs))
	}
	// The Seven Deadly Sins' knights raised (migration 101): Lancelot and
	// Tristan Mythic, Zeldris, Mael, Gawain and Percival Legendary; Arthur
	// stays Epic.
	for id, want := range map[int64]Rarity{265209: Mythic, 249281: Mythic, 131559: Legendary, 126851: Legendary, 282753: Legendary, 206212: Legendary, 122210: Epic} {
		if got := RarityOf(id, 1400); got != want {
			t.Errorf("RarityOf(%d) = %v, want %v", id, got, want)
		}
	}
	// The best known women made Legendary (migration 99), at any rank.
	for _, id := range []int64{137079, 120649, 133676, 89361, 61, 94, 22037, 138102, 133700, 138100, 723, 36828} {
		if RarityOf(id, 0) != Legendary || RarityOf(id, 1400) != Legendary {
			t.Errorf("%d is %v, not Legendary", id, RarityOf(id, 0))
		}
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
				"favourites": 100000 - id, "media": map[string]any{"edges": []any{
					map[string]any{"characterRole": "BACKGROUND", "node": map[string]any{"title": map[string]string{"romaji": "Cameo", "english": "Cameo"}}},
					map[string]any{"characterRole": "MAIN", "node": map[string]any{"title": map[string]string{"romaji": "Romaji", "english": ""}}},
				}},
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
				"favourites": 100000 - id, "media": map[string]any{"edges": []any{}},
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

// A token comes every 2 hours, the periods starting at even hours in
// Tashkent; one away gathers a token a period, twelve at most, and one never
// given any gets one.
func TestATokenEveryTwoHours(t *testing.T) {
	now := time.Date(2026, 11, 16, 20, 30, 0, 0, time.UTC) // 01:30 in Tashkent
	start := PeriodStart(now)
	if !start.Equal(time.Date(2026, 11, 17, 0, 0, 0, 0, Location)) || !Today(now).Next().Equal(start.Add(2*time.Hour)) {
		t.Errorf("period start = %v, next %v", start, Today(now).Next())
	}
	player := storage.CardPlayer{FreeDay: start, Tokens: 1}
	if n := Tokens(player, now); n != 1 {
		t.Errorf("given this period, one held = %d", n)
	}
	player.FreeDay = start.Add(-6 * time.Hour)
	if n := Tokens(player, now); n != 1+3 {
		t.Errorf("three periods away = %d, want 4", n)
	}
	player.FreeDay = start.AddDate(0, 0, -5)
	if n := Tokens(player, now); n != 1+TokenMost {
		t.Errorf("five days away = %d, want %d", n, 1+TokenMost)
	}
	if n := Tokens(storage.CardPlayer{}, now); n != 1 {
		t.Errorf("never given = %d", n)
	}
}

// A picture kept on the bot's own pages is a path, fetched from the site.
func TestFetchPictureOfAPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/kartalar/art/40.jpg" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("\xff\xd8JPEG"))
	}))
	defer server.Close()
	c := &Catalog{Client: server.Client(), Site: server.URL + "/"}
	if data, _, err := c.FetchPicture(context.Background(), "/kartalar/art/40.jpg"); err != nil || string(data) != "\xff\xd8JPEG" {
		t.Errorf("picture = %q, %v", data, err)
	}
}

// The 100th card in a row without a Legendary or better is one for sure,
// and the 200th without a Mythic is a Mythic; a roll as good or better is
// left as it is.
func TestWithPity(t *testing.T) {
	common := WithPity(func() Rarity { return Common })
	mythic := WithPity(func() Rarity { return Mythic })
	for _, c := range []struct {
		roll                        func(int, int) (int, bool)
		sinceLegendary, sinceMythic int
		want                        Rarity
		sure                        bool
	}{
		{common, 0, 0, Common, false},
		{common, 98, 98, Common, false},
		{common, 99, 99, Legendary, true},
		{common, 10, 199, Mythic, true},
		{common, 99, 199, Mythic, true},
		{mythic, 99, 199, Mythic, false},
	} {
		if r, sure := c.roll(c.sinceLegendary, c.sinceMythic); Rarity(r) != c.want || sure != c.sure {
			t.Errorf("after %d and %d: %d, %v; want %d, %v", c.sinceLegendary, c.sinceMythic, r, sure, c.want, c.sure)
		}
	}
}
