package cards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"telegram-ai-bot/internal/storage"
)

// DefaultPokeWallet is PokéWallet's API, which holds trading cards of many
// games. Its pictures want the key too, so they reach a page through the
// bot's server and the key is sent nowhere else.
const DefaultPokeWallet = "https://api.pokewallet.io"

// tradingSet is a set of a trading card game that is a collection of cards.
// Prefix is where its game's API is and ImagePrefix its pictures, which for
// One Piece are not under its prefix.
type tradingSet struct {
	Collection, Title, Prefix, ImagePrefix, SetID string
}

// tradingSets are one well-known English set of each game close to anime.
var tradingSets = []tradingSet{
	{"pokemon", "Pokémon", "", "", "23237"},                // Scarlet & Violet 151
	{"onepiece", "One Piece", "/op", "", "3188"},           // Romance Dawn
	{"dragonball", "Dragon Ball", "/dbs", "/dbs", "23401"}, // Awakened Pulse
	{"yugioh", "Yu-Gi-Oh!", "/ygo", "/ygo", "22881"},       // Legend of Blue Eyes
	{"digimon", "Digimon", "/dgm", "/dgm", "3035"},         // New Awakening
}

// tradingRarities are the games' rarities as Kartalar's; a card of any other,
// a promo, a code card, an energy marker, is no card of the collection.
var tradingRarities = map[string]Rarity{
	"Common": Common, "C": Common,
	"Uncommon": Rare, "UC": Rare,
	"Rare": Epic, "R": Epic, "Holo Rare": Epic,
	"Double Rare": Legendary, "Ultra Rare": Legendary, "Super Rare": Legendary, "SR": Legendary,
	"Illustration Rare": Legendary, "Leader": Legendary, "L": Legendary,
	"Special Illustration Rare": Mythic, "Secret Rare": Mythic, "SEC": Mythic, "Hyper Rare": Mythic,
}

// setRarities are a game's own rarities where they are not tradingRarities':
// Yu-Gi-Oh!'s Rare is its second, so that it had no Rare cards.
var setRarities = map[string]map[string]Rarity{
	"yugioh": {"Rare": Rare, "R": Rare, "Super Rare": Epic, "SR": Epic, "Ultra Rare": Legendary, "UR": Legendary},
}

// rarityOf is a card's rarity in Kartalar, by its game's.
func rarityOf(collection, rarity string) (Rarity, bool) {
	if r, ok := setRarities[collection][rarity]; ok {
		return r, true
	}
	r, ok := tradingRarities[rarity]
	return r, ok
}

// setQuota is how many cards of each rarity one set gives at most, a hundred
// in all, so that a set's many commons do not fill it.
var setQuota = map[Rarity]int{Mythic: 8, Legendary: 15, Epic: 22, Rare: 25, Common: 30}

// tradingName is a card number in a name: "Blastoise ex - 200/165",
// "Trunks : Future - FB01-036 (Alternate Art)".
var tradingName = regexp.MustCompile(`\s+-\s+[A-Za-z0-9]*-?\d+(/\d+)?\b`)

type tradingCard struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Rarity      string `json:"rarity"`
	ProductType string `json:"product_type"`
	Info        *struct {
		Name        string `json:"name"`
		Rarity      string `json:"rarity"`
		ProductType string `json:"product_type"`
	} `json:"card_info"`
}

// fields are a card's name, rarity and type: One Piece's API has them on the
// card, the others under its card_info.
func (c tradingCard) fields() (name, rarity, productType string) {
	if c.Info != nil {
		return c.Info.Name, c.Info.Rarity, c.Info.ProductType
	}
	return c.Name, c.Rarity, c.ProductType
}

// tradingID is a card's ID in the catalog: PokéWallet's are long strings, and
// a negative number keeps clear of AniList's.
func tradingID(id string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return -int64(h.Sum64()>>1) - 1
}

// setsVersion is the version of how a set is read: 2 reads all its pages, as
// the first read only the first 200 cards and missed the Pokémon set's best,
// takes one card per character and rarity, as its art variants filled whole
// rarities with one character, gives Yu-Gi-Oh! its Rare cards, and takes the
// pictures at 600 by 838, as the 200 by 280 ones looked blurred on a phone.
const setsVersion = 2

// maxSetPages bounds the pages of a set read, PokéWallet counting each.
const maxSetPages = 5

// setPage is one page of a set's cards.
type setPage struct {
	Set struct {
		Name string `json:"name"`
	} `json:"set"`
	Cards      []tradingCard `json:"cards"`
	Data       []tradingCard `json:"data"` // One Piece's
	Pagination struct {
		TotalPages int `json:"total_pages"`
	} `json:"pagination"`
}

// fetchSetPage asks for one page of a set.
func fetchSetPage(ctx context.Context, client *http.Client, baseURL, key string, set tradingSet, page int) (setPage, error) {
	var answer setPage
	link := fmt.Sprintf("%s%s/sets/%s?limit=200&page=%d", baseURL, set.Prefix, url.PathEscape(set.SetID), page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return answer, err
	}
	req.Header.Set("X-API-Key", key)
	resp, err := client.Do(req)
	if err != nil {
		return answer, fmt.Errorf("cards: ask PokéWallet: %w", withoutURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return answer, fmt.Errorf("cards: PokéWallet refused %s: status %d", set.Collection, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&answer); err != nil {
		return answer, fmt.Errorf("cards: read PokéWallet: %w", err)
	}
	return answer, nil
}

// variant is the end of a card's name that tells its art from another's:
// "Shanks (Parallel) (Manga) (Alternate Art)", "Mew ex (151 Metal Card)".
var variant = regexp.MustCompile(`(\s*\([^)]*\))+$`)

// character is the character a card's name is of.
func character(name string) string {
	return strings.ToLower(strings.TrimSpace(variant.ReplaceAllString(name, "")))
}

// fetchSet is a set's cards, all its pages, by rarity up to its quota and one
// per character and rarity.
func fetchSet(ctx context.Context, client *http.Client, baseURL, key string, set tradingSet) ([]storage.Card, error) {
	first, err := fetchSetPage(ctx, client, baseURL, key, set, 1)
	if err != nil {
		return nil, err
	}
	all := append(first.Cards, first.Data...)
	for page := 2; page <= min(first.Pagination.TotalPages, maxSetPages); page++ {
		next, err := fetchSetPage(ctx, client, baseURL, key, set, page)
		if err != nil {
			return nil, err
		}
		all = append(all, append(next.Cards, next.Data...)...)
	}
	taken := map[Rarity]int{}
	seen := map[string]bool{}
	var cards []storage.Card
	for _, c := range all {
		name, rarity, productType := c.fields()
		r, known := rarityOf(set.Collection, rarity)
		name = strings.TrimSpace(tradingName.ReplaceAllString(name, ""))
		who := fmt.Sprint(r, character(name))
		if !known || productType != "card" || taken[r] == setQuota[r] || seen[who] {
			continue
		}
		taken[r]++
		seen[who] = true
		id := tradingID(c.ID)
		rl := rules[r]
		cards = append(cards, storage.Card{
			ID: id, Name: name, Anime: set.Title + " · " + first.Set.Name,
			ImageURL: baseURL + set.ImagePrefix + "/images/" + url.PathEscape(c.ID) + "?size=high", Rarity: int(r), Value: rl.value, Collection: set.Collection,
			Attack: rounded(between(id, "attack", rl.attackLeast, rl.attackMost)),
			Health: rounded(between(id, "health", rl.healthLeast, rl.healthMost)),
		})
	}
	if len(cards) == 0 {
		return nil, fmt.Errorf("cards: PokéWallet gave no cards of %s", set.Collection)
	}
	return cards, nil
}

// withoutURL drops the address from a transport error.
func withoutURL(err error) error {
	var u *url.Error
	if errors.As(err, &u) {
		return u.Err
	}
	return err
}
