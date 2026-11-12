// Package cards is Kartalar, an anime card collection: AniList's most loved
// characters are its cards, the most loved the rarest; a player draws one
// at random every hour, of a rarity drawn by its odds, and collects them
// for points.
package cards

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
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

// CatalogSize is how many of AniList's most loved characters are cards: the
// rarities take the first 200 by rank, and the rest are Common.
const CatalogSize = 500

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

// LegendaryPity and MythicPity are the draws in a row that bring a player
// no Legendary or better, and no Mythic, the last of which is one for sure:
// by the odds alone 412 draws had brought no Mythic, which players took for
// there being none to draw.
const (
	LegendaryPity = 30
	MythicPity    = 100
)

// WithPity draws a rarity by roll, raised to Mythic on a player's
// MythicPity-th draw in a row without one, and to Legendary on their
// LegendaryPity-th without one or better.
func WithPity(roll func() Rarity) storage.CardRoll {
	return func(sinceLegendary, sinceMythic int) (int, bool) {
		r := roll()
		switch {
		case r < Mythic && sinceMythic+1 >= MythicPity:
			return int(Mythic), true
		case r < Legendary && sinceLegendary+1 >= LegendaryPity:
			return int(Legendary), true
		}
		return int(r), false
	}
}

// PityIn is in how many draws a player's next sure Legendary and Mythic
// are, after the draw that left them these counts.
func PityIn(draw storage.CardDraw) (legendary, mythic int) {
	return LegendaryPity - draw.SinceLegendary, MythicPity - draw.SinceMythic
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
		ID: id, Name: name, Anime: anime, ImageURL: image, Favourites: favourites, Rarity: int(r), Value: rl.value, Collection: "anime",
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
	CardCollections(ctx context.Context) (map[string]int, error)
	SaveCardCatalog(ctx context.Context, cards []storage.Card) error
	CardSetVersions(ctx context.Context) (map[string]int, error)
	CardCatalog(ctx context.Context) ([]storage.Card, error)
	FillCardSet(ctx context.Context, collection string, version int, cards []storage.Card, merges map[int64]int64) error
}

// DefaultAniList is AniList's GraphQL API.
const DefaultAniList = "https://graphql.anilist.co"

// retryTrading is how long after a failed try PokéWallet is asked again: every
// draw asking a service that is down would be slow and spend its limit.
const retryTrading = 10 * time.Minute

// Catalog fills the catalog of cards. Its zero addresses mean the services'
// own, and without Key there are no trading cards.
type Catalog struct {
	Store      CatalogStore
	Client     *http.Client
	AniList    string
	PokeWallet string
	Key        string
	Logger     *slog.Logger

	mu    sync.Mutex
	tried time.Time
}

func (c *Catalog) pokeWallet() string { return cmp.Or(c.PokeWallet, DefaultPokeWallet) }

// Ensure asks for the collections the catalog does not have yet, and for the
// trading ones an older setsVersion filled, adding what it missed. Only the
// anime cards it cannot do without: trading cards that cannot be had are
// logged and tried again later, and the game goes on without them.
func (c *Catalog) Ensure(ctx context.Context) error {
	sizes, err := c.Store.CardCollections(ctx)
	if err != nil {
		return err
	}
	if sizes["anime"] < CatalogSize {
		catalog, err := FetchCatalog(ctx, c.Client, cmp.Or(c.AniList, DefaultAniList))
		if err != nil {
			return err
		}
		fresh, err := c.newCards(ctx, catalog)
		if err != nil {
			return err
		}
		if err := c.Store.SaveCardCatalog(ctx, fresh); err != nil {
			return err
		}
	}
	if c.Key == "" {
		return nil
	}
	versions, err := c.Store.CardSetVersions(ctx)
	if err != nil {
		return err
	}
	if !c.mayTry(versions) {
		return nil
	}
	for _, set := range tradingSets {
		if versions[set.Collection] >= setsVersion {
			continue
		}
		err := c.fillSet(ctx, set)
		if err != nil && c.Logger != nil {
			c.Logger.Warn("could not fill a card collection", "collection", set.Collection, "error", err)
		}
	}
	return nil
}

// fillSet reads a set again and makes the collection its cards, an older
// reading's art variants of a character merged into the one kept.
func (c *Catalog) fillSet(ctx context.Context, set tradingSet) error {
	cards, err := fetchSet(ctx, c.Client, c.pokeWallet(), c.Key, set)
	if err != nil {
		return err
	}
	held, err := c.Store.CardCatalog(ctx)
	if err != nil {
		return err
	}
	return c.Store.FillCardSet(ctx, set.Collection, setsVersion, cards, variantsOf(held, cards, set.Collection))
}

// variantsOf maps each card of the collection the new reading left out to the
// card it kept of the same character and rarity, if there is one.
func variantsOf(held, cards []storage.Card, collection string) map[int64]int64 {
	kept := map[string]int64{}
	reading := map[int64]bool{}
	for _, c := range cards {
		kept[fmt.Sprint(c.Rarity, character(c.Name))] = c.ID
		reading[c.ID] = true
	}
	merges := map[int64]int64{}
	for _, h := range held {
		if h.Collection != collection || reading[h.ID] {
			continue
		}
		if id, ok := kept[fmt.Sprint(h.Rarity, character(h.Name))]; ok {
			merges[h.ID] = id
		}
	}
	return merges
}

// mayTry reports that a set is missing or old and none was tried lately.
func (c *Catalog) mayTry(versions map[string]int) bool {
	missing := false
	for _, set := range tradingSets {
		missing = missing || versions[set.Collection] < setsVersion
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !missing || time.Since(c.tried) < retryTrading {
		return false
	}
	c.tried = time.Now()
	return true
}

// FetchPicture is a card's picture. The key goes only to PokéWallet.
func (c *Catalog) FetchPicture(ctx context.Context, link string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, "", err
	}
	if c.Proxied(link) {
		req.Header.Set("X-API-Key", c.Key)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("cards: fetch a picture: %w", withoutURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("cards: fetch a picture: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, pictureLimit+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > pictureLimit {
		return nil, "", errors.New("cards: picture too large")
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// Proxied reports a picture that wants the key, which a page must get through
// the bot's server.
func (c *Catalog) Proxied(link string) bool {
	return strings.HasPrefix(link, c.pokeWallet()+"/")
}

// newCards are the cards of catalog not in the store yet: a card there keeps
// its rarity and numbers, as players hold it, however AniList's ranks moved.
func (c *Catalog) newCards(ctx context.Context, catalog []storage.Card) ([]storage.Card, error) {
	held, err := c.Store.CardCatalog(ctx)
	if err != nil {
		return nil, err
	}
	known := make(map[int64]bool, len(held))
	for _, card := range held {
		known[card.ID] = true
	}
	var fresh []storage.Card
	for _, card := range catalog {
		if !known[card.ID] {
			fresh = append(fresh, card)
		}
	}
	return fresh, nil
}

// pictureLimit bounds a card's picture.
const pictureLimit = 3 << 20

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
