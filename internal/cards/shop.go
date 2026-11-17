package cards

import (
	"fmt"
	"strconv"
	"strings"

	"telegram-ai-bot/internal/storage"
)

// CrystalPack is crystals sold for Telegram Stars.
type CrystalPack struct {
	ID       string `json:"id"`
	Crystals int    `json:"crystals"`
	Stars    int    `json:"stars"`
}

// CrystalPacks are the crystals on sale, the bigger the cheaper each.
var CrystalPacks = []CrystalPack{
	{ID: "c100", Crystals: 100, Stars: 10},
	{ID: "c300", Crystals: 300, Stars: 25},
	{ID: "c700", Crystals: 700, Stars: 50},
	{ID: "c1500", Crystals: 1500, Stars: 99},
}

// ShopItem is what crystals buy in the shop: tokens, or a pack of Cards
// with at least one of Least's rarity.
type ShopItem struct {
	ID     string `json:"id"`
	Price  int    `json:"price"`
	Tokens int    `json:"tokens,omitempty"`
	Cards  int    `json:"cards,omitempty"`
	// Least is the rarity a pack is sure to hold one of.
	Least Rarity `json:"least,omitempty"`
}

// ShopItems are the shop's goods. Tokens come cheaper than opening cards for
// crystals; a pack's sure card is worth more than its odds.
var ShopItems = []ShopItem{
	{ID: "t5", Price: 60, Tokens: 5},
	{ID: "t12", Price: 135, Tokens: 12},
	{ID: "epic", Price: 120, Cards: 5, Least: Epic},
	{ID: "legendary", Price: 350, Cards: 5, Least: Legendary},
	{ID: "mythic", Price: 2000, Cards: 5, Least: Mythic},
}

// ShopItemByID is the shop's item by its ID.
func ShopItemByID(id string) (ShopItem, bool) {
	for _, item := range ShopItems {
		if item.ID == id {
			return item, true
		}
	}
	return ShopItem{}, false
}

// CrystalPackByID is the crystal pack by its ID.
func CrystalPackByID(id string) (CrystalPack, bool) {
	for _, p := range CrystalPacks {
		if p.ID == id {
			return p, true
		}
	}
	return CrystalPack{}, false
}

// PackRoll draws n cards by roll, the last raised to least when none before
// it was as rare: a pack holds at least one card of least's rarity.
func PackRoll(roll func() Rarity, n int, least Rarity) storage.CardRoll {
	drawn, best := 0, Common
	return func(int, int) (int, bool) {
		drawn++
		r := roll()
		raised := false
		if drawn == n && best < least && r < least {
			r, raised = least, true
		}
		best = max(best, r)
		return int(r), raised
	}
}

// crystalPrefix starts a crystal invoice's payload: "cr:<user>:<pack>".
const crystalPrefix = "cr:"

// CrystalPayload is the payload of the invoice for pack, made out to user.
func CrystalPayload(userID int64, pack CrystalPack) string {
	return fmt.Sprintf("%s%d:%s", crystalPrefix, userID, pack.ID)
}

// ParseCrystalPayload reads a crystal invoice's payload; ok is false for any
// other, such as premium's.
func ParseCrystalPayload(payload string) (userID int64, pack CrystalPack, ok bool) {
	rest, isCrystal := strings.CutPrefix(payload, crystalPrefix)
	if !isCrystal {
		return 0, CrystalPack{}, false
	}
	who, id, found := strings.Cut(rest, ":")
	userID, err := strconv.ParseInt(who, 10, 64)
	if !found || err != nil {
		return 0, CrystalPack{}, false
	}
	pack, ok = CrystalPackByID(id)
	return userID, pack, ok
}
