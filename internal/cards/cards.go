// Package cards is Kartalar, an anime card collection: AniList's most loved
// characters are its cards, the most loved the rarest; a player opens two
// free a day at random, of a rarity drawn by its odds, and collects them
// for points, a card counted once however many copies they hold.
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
	"net/url"
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

// A player is given PerDay tokens every TokenEvery, the periods starting at
// even hours in Location, and gathers at most TokenMost periods' while away;
// each token opens a card, and crystals open more.
const (
	PerDay     = 1
	TokenEvery = 2 * time.Hour
	TokenMost  = 12
)

// Location is where the card game's token periods start: Tashkent, which
// keeps no summer time.
var Location = time.FixedZone("Tashkent", 5*60*60)

// PeriodStart is the even hour in Location that began now's token period.
func PeriodStart(now time.Time) time.Time {
	local := now.In(Location)
	hours := int(TokenEvery / time.Hour)
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour()-local.Hour()%hours, 0, 0, 0, Location)
}

// Today is the period as the tokens count it.
func Today(now time.Time) storage.CardDay {
	return storage.CardDay{Start: PeriodStart(now), PerDay: PerDay, Every: TokenEvery, Most: TokenMost}
}

// Tokens is how many tokens the player may spend now: those they hold, and
// those due since they were last given some.
func Tokens(p storage.CardPlayer, now time.Time) int {
	var last *time.Time
	if !p.FreeDay.IsZero() {
		last = &p.FreeDay
	}
	return p.Tokens + storage.DueTokens(last, Today(now))
}

// CatalogSize is how many of AniList's most loved characters are cards: the
// rarities take the first 325 by rank, and the rest are Common. It was 500,
// and the players found too many anime missing.
const CatalogSize = 1500

// rule is what one rarity is: how many cards of the catalog have it, the
// most loved first, its odds in a thousand draws, the points it is worth
// and its cards' attack and health.
type rule struct {
	cards, odds, value      int
	attackLeast, attackMost int
	healthLeast, healthMost int
}

// rules are by rarity. Mythic and Legendary are the characters anime fans
// at large know, not today's votes: the leads of the best known series and
// their legendary rivals and companions, chosen by hand in mythicIDs and
// legendaryIDs. The rest go by rank among AniList's most loved, never above
// Epic: 100 Epic, 150 Rare and the others Common. A Mythic is drawn once in
// 67 draws, and worth 4 000 points, a Legendary 2 000, an Epic 400, a Rare
// 200 and a Common 100.
var rules = map[Rarity]rule{
	Mythic:    {cards: 25, odds: 5, value: 4000, attackLeast: 4000, attackMost: 6000, healthLeast: 15000, healthMost: 22000},
	Legendary: {cards: 50, odds: 25, value: 2000, attackLeast: 2500, attackMost: 4000, healthLeast: 9000, healthMost: 15000},
	Epic:      {cards: 100, odds: 90, value: 400, attackLeast: 1500, attackMost: 2500, healthLeast: 6000, healthMost: 9000},
	Rare:      {cards: 150, odds: 250, value: 200, attackLeast: 800, attackMost: 1500, healthLeast: 3000, healthMost: 6000},
	Common:    {cards: 176, odds: 630, value: 100, attackLeast: 300, attackMost: 800, healthLeast: 1000, healthMost: 3000},
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

// ByOdds draws a rarity by roll alone: no card is ever sure, however long
// a player waited for a rare one.
func ByOdds(roll func() Rarity) storage.CardRoll {
	return func(int, int) (int, bool) { return int(roll()), false }
}

// mythicIDs are the Mythic cards' AniList IDs, 16 men and 11 women: the
// leads of the best known series, Son Goku, Naruto, Luffy, Ichigo, Eren,
// Edward Elric, Light, Tanjiro, Saitama, Gon, Lelouch, Spike, Guts, Jotaro and
// Meliodas, Gojo, whom the owners made Mythic, and the best known women, Mikasa, Maomao, Frieren, Makima, Nezuko,
// Emilia, Kurisu, Violet Evergarden, Zero Two and Mai Sakurajima, and
// Firefly Wedding's Satoko Kirigaya, whom the owners made Mythic (migration 94).
var mythicIDs = map[int64]bool{
	246: true, 17: true, 40: true, 5: true, 40882: true, 11: true, 80: true, 126071: true, 73935: true, 30: true,
	417: true, 1: true, 422: true, 4003: true, 72921: true, 127691: true,
	40881: true, 126824: true, 176754: true, 137080: true, 127518: true, 88572: true, 34470: true, 90169: true, 124381: true, 127222: true,
	298761: true,
}

// legendaryIDs are the Legendary cards': the leads' legendary rivals and
// companions, Vegeta, Sasuke, Kakashi, Itachi, Levi, L, Zoro, Killua and
// their like, Escanor and Ban, and other well known leads, Deku, Itadori,
// Shinji, Kaneki, Denji, Gintoki, Natsu, Asta, Yusuke, Mob, Hanamichi,
// Thorfinn, Rimuru and theirs, and the best known women, Rem, Rei Ayanami,
// Power, Kaguya Shinomiya, Marin Kitagawa, Megumin, Robin, Asuka, Hitagi
// Senjougahara, Yor and Anya Forger, Nobara Kugisaki, Nami and Asuna, the
// last twelve made Legendary at the owners' asking (migration 99).
var legendaryIDs = map[int64]bool{
	90202: true, 77605: true, 913: true, 13: true, 85: true, 14: true, 53901: true, 2535: true, 2423: true, 62: true,
	305: true, 727: true, 2072: true, 13767: true, 45627: true, 46494: true, 46496: true, 71: true, 75: true, 27: true,
	28: true, 31: true, 133701: true, 126635: true, 68: true, 12: true, 88892: true, 89220: true, 129133: true,
	129131: true, 129130: true, 6356: true, 10529: true, 4004: true, 162: true, 2075: true, 2257: true, 601: true, 10138: true,
	123962: true, 124142: true, 88573: true, 35252: true, 138101: true, 64769: true, 88575: true, 86: true, 89028: true, 127212: true,
	89: true, 87275: true, 130102: true, 672: true, 5187: true, 123285: true, 729: true, 89616: true, 310: true,
	137079: true, 120649: true, 133676: true, 89361: true, 61: true, 94: true, 22037: true, 138102: true, 133700: true, 138100: true,
	723: true, 36828: true,
}

// epicIDs are the Epic cards chosen by hand, whatever their rank: The Seven
// Deadly Sins' Elizabeth, Merlin, King, Diane, Zeldris, Mael and Gowther,
// Firefly Wedding's Shinpei Gotou (migration 94) and the Four Knights of the
// Apocalypse, whom the players asked for.
var epicIDs = map[int64]bool{
	298762: true, 72923: true, 89180: true, 83801: true, 74653: true, 131559: true, 126851: true, 88823: true,
	// Four Knights of the Apocalypse: Percival, Lancelot, Tristan, Gawain
	// and Arthur, added by hand (migration 92).
	206212: true, 265209: true, 249281: true, 282753: true, 122210: true,
}

// RarityOf is the rarity of a character at rank among the most loved, from
// 0: Mythic, Legendary or Epic when chosen so, and otherwise by rank, at
// most Epic.
func RarityOf(id int64, rank int) Rarity {
	switch {
	case mythicIDs[id]:
		return Mythic
	case legendaryIDs[id]:
		return Legendary
	case epicIDs[id]:
		return Epic
	}
	return min(OfRank(rank), Epic)
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
	return CardOf(id, name, anime, image, favourites, RarityOf(id, rank))
}

// CardOf is the card of a character of rarity r, its numbers within r's.
func CardOf(id int64, name, anime, image string, favourites int, r Rarity) storage.Card {
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
	CardCatalog(ctx context.Context) ([]storage.Card, error)
}

// DefaultAniList is AniList's GraphQL API.
const DefaultAniList = "https://graphql.anilist.co"

// Catalog fills the catalog of cards, from AniList. Its zero address means
// AniList's own.
type Catalog struct {
	Store   CatalogStore
	Client  *http.Client
	AniList string
	// Site is where a picture kept on the bot's own pages is, such as
	// "https://maria.vercel.app": a card's picture may be a path there,
	// "/kartalar/art/40.jpg", which the Mini App opens as it is.
	Site   string
	Logger *slog.Logger
}

// Ensure asks AniList for the anime cards when the catalog has fewer than
// CatalogSize, adding those it does not have yet.
func (c *Catalog) Ensure(ctx context.Context) error {
	sizes, err := c.Store.CardCollections(ctx)
	if err != nil || sizes["anime"] >= CatalogSize {
		return err
	}
	catalog, err := FetchCatalog(ctx, c.Client, cmp.Or(c.AniList, DefaultAniList))
	if err != nil {
		return err
	}
	fresh, err := c.newCards(ctx, catalog)
	if err != nil {
		return err
	}
	return c.Store.SaveCardCatalog(ctx, fresh)
}

// FetchPicture is a card's picture: a path on the bot's own pages is
// fetched from Site.
func (c *Catalog) FetchPicture(ctx context.Context, link string) ([]byte, string, error) {
	if strings.HasPrefix(link, "/") {
		link = strings.TrimSuffix(c.Site, "/") + link
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, "", err
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
		characters, err := fetchPageAgain(ctx, client, baseURL, page, perPage)
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

// errAniListBusy is AniList asking to slow down.
var errAniListBusy = errors.New("cards: AniList is busy")

// aniListWaits are the pauses before asking a page again after AniList
// asked to slow down: it lets 30 to 90 requests through a minute.
var aniListWaits = []time.Duration{3 * time.Second, 10 * time.Second, 30 * time.Second}

// fetchPageAgain is fetchPage, asked again after a pause when AniList says
// it is too busy.
func fetchPageAgain(ctx context.Context, client *http.Client, baseURL string, page, perPage int) ([]aniListCharacter, error) {
	for _, wait := range aniListWaits {
		characters, err := fetchPage(ctx, client, baseURL, page, perPage)
		if !errors.Is(err, errAniListBusy) {
			return characters, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return fetchPage(ctx, client, baseURL, page, perPage)
}

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
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errAniListBusy
	}
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

// withoutURL drops the address from a transport error.
func withoutURL(err error) error {
	var u *url.Error
	if errors.As(err, &u) {
		return u.Err
	}
	return err
}
