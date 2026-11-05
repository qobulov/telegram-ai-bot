// Package cards is Kartalar, an anime card collection: AniList's most loved
// characters are its cards, the most loved the rarest; a player draws one
// at random every hour, of a rarity drawn by its odds, and collects them
// for points.
package cards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
)

// Rarity is a card's rarity, from Common up.
type Rarity int

const (
	Common Rarity = iota
	Rare
	Epic
	Legendary
	Mythic
)

// Rarities lists every rarity, the most common first.
var Rarities = []Rarity{Common, Rare, Epic, Legendary, Mythic}

// Every is how often a player may draw a card.
const Every = time.Hour

// CatalogSize is how many of AniList's most loved characters are cards.
const CatalogSize = 200

// rule is what one rarity is: how many cards of the catalog have it, the
// most loved first, its odds in a thousand draws, the points it is worth
// and its cards' attack and health.
type rule struct {
	cards, odds, value      int
	attackLeast, attackMost int
	healthLeast, healthMost int
}

// rules are by rarity: 15 mythic cards of the 200, drawn once in 67 draws.
var rules = map[Rarity]rule{
	Mythic:    {cards: 15, odds: 15, value: 5000, attackLeast: 4000, attackMost: 6000, healthLeast: 15000, healthMost: 22000},
	Legendary: {cards: 30, odds: 65, value: 2000, attackLeast: 2500, attackMost: 4000, healthLeast: 9000, healthMost: 15000},
	Epic:      {cards: 45, odds: 140, value: 800, attackLeast: 1500, attackMost: 2500, healthLeast: 6000, healthMost: 9000},
	Rare:      {cards: 60, odds: 280, value: 300, attackLeast: 800, attackMost: 1500, healthLeast: 3000, healthMost: 6000},
	Common:    {cards: 50, odds: 500, value: 100, attackLeast: 300, attackMost: 800, healthLeast: 1000, healthMost: 3000},
}

// Roll draws a rarity by the odds, from a number in [0, 1000).
func Roll(n int) Rarity {
	for _, r := range []Rarity{Mythic, Legendary, Epic, Rare} {
		if n < rules[r].odds {
			return r
		}
		n -= rules[r].odds
	}
	return Common
}

// RollRandom draws a rarity at random.
func RollRandom() Rarity {
	return Roll(rand.IntN(1000))
}

// OfRank is the rarity of the card at rank among the most loved, from 0.
func OfRank(rank int) Rarity {
	for _, r := range []Rarity{Mythic, Legendary, Epic, Rare} {
		if rank < rules[r].cards {
			return r
		}
		rank -= rules[r].cards
	}
	return Common
}

// Value is the points a card of rarity is worth.
func Value(r Rarity) int { return rules[r].value }

// Card is the card of a character at rank among the most loved: its rarity,
// its points, and its attack and health, the same each time for the same
// character, within its rarity's.
func Card(id int64, name, anime, image string, favourites, rank int) storage.Card {
	r := OfRank(rank)
	rl := rules[r]
	return storage.Card{
		ID: id, Name: name, Anime: anime, ImageURL: image, Favourites: favourites, Rarity: int(r), Value: rl.value,
		Attack: rounded(between(id, "attack", rl.attackLeast, rl.attackMost)),
		Health: rounded(between(id, "health", rl.healthLeast, rl.healthMost)),
	}
}

// between is a number in [least, most] that id and what always give.
func between(id int64, what string, least, most int) int {
	h := fnv.New32a()
	fmt.Fprintf(h, "%d:%s", id, what)
	return least + int(h.Sum32()%uint32(most-least+1))
}

// rounded rounds to tens, as cards print their numbers.
func rounded(n int) int { return (n + 5) / 10 * 10 }

// CatalogStore keeps the catalog of cards.
type CatalogStore interface {
	CardCatalogSize(ctx context.Context) (int, error)
	SaveCardCatalog(ctx context.Context, cards []storage.Card) error
}

// DefaultAniList is AniList's GraphQL API.
const DefaultAniList = "https://graphql.anilist.co"

// EnsureCatalog asks AniList at baseURL for the cards the first time, and
// when the catalog has fewer than it should.
func EnsureCatalog(ctx context.Context, store CatalogStore, client *http.Client, baseURL string) error {
	size, err := store.CardCatalogSize(ctx)
	if err != nil || size >= CatalogSize {
		return err
	}
	catalog, err := FetchCatalog(ctx, client, baseURL)
	if err != nil {
		return err
	}
	return store.SaveCardCatalog(ctx, catalog)
}

// errAniList is AniList answering with an error.
var errAniList = errors.New("cards: AniList refused")

// FetchCatalog asks AniList at baseURL for its CatalogSize most loved
// characters, as cards.
func FetchCatalog(ctx context.Context, client *http.Client, baseURL string) ([]storage.Card, error) {
	const perPage = 50
	var catalog []storage.Card
	for page := 1; len(catalog) < CatalogSize; page++ {
		characters, err := fetchPage(ctx, client, baseURL, page, perPage)
		if err != nil {
			return nil, err
		}
		for _, c := range characters {
			if len(catalog) == CatalogSize || c.Image.Large == "" {
				continue
			}
			anime := ""
			if len(c.Media.Nodes) > 0 {
				anime = c.Media.Nodes[0].Title.English
				if anime == "" {
					anime = c.Media.Nodes[0].Title.Romaji
				}
			}
			catalog = append(catalog, Card(c.ID, strings.TrimSpace(c.Name.Full), anime, c.Image.Large, c.Favourites, len(catalog)))
		}
		if len(characters) < perPage {
			break
		}
	}
	if len(catalog) < CatalogSize {
		return nil, fmt.Errorf("cards: AniList gave %d characters, not %d", len(catalog), CatalogSize)
	}
	return catalog, nil
}

type aniListCharacter struct {
	ID   int64 `json:"id"`
	Name struct {
		Full string `json:"full"`
	} `json:"name"`
	Image struct {
		Large string `json:"large"`
	} `json:"image"`
	Favourites int `json:"favourites"`
	Media      struct {
		Nodes []struct {
			Title struct {
				Romaji  string `json:"romaji"`
				English string `json:"english"`
			} `json:"title"`
		} `json:"nodes"`
	} `json:"media"`
}

const charactersQuery = `query ($page: Int, $perPage: Int) {
  Page(page: $page, perPage: $perPage) {
    characters(sort: FAVOURITES_DESC) {
      id name { full } image { large } favourites
      media(perPage: 1, sort: POPULARITY_DESC) { nodes { title { romaji english } } }
    }
  }
}`

func fetchPage(ctx context.Context, client *http.Client, baseURL string, page, perPage int) ([]aniListCharacter, error) {
	body, _ := json.Marshal(map[string]any{"query": charactersQuery, "variables": map[string]int{"page": page, "perPage": perPage}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cards: ask AniList: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", errAniList, resp.StatusCode)
	}
	var answer struct {
		Data struct {
			Page struct {
				Characters []aniListCharacter `json:"characters"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		return nil, fmt.Errorf("cards: read AniList: %w", err)
	}
	return answer.Data.Page.Characters, nil
}
