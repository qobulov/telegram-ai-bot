package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/storage"
)

const (
	maxItemQuery = 60
	itemCardNote = "The item's card, with its stats, is sent as a sticker with your answer: answer in one short line at most, without listing the stats."
)

// ItemCards lists the Mobile Legends item cards imported with
// /import_items.
type ItemCards interface {
	ItemCards(ctx context.Context) ([]storage.ItemCardSticker, error)
}

// ItemCardOptions configures the mlbb_item_card tool.
type ItemCardOptions struct {
	Store ItemCards
}

// ItemCardTool sends a Mobile Legends item's card, a sticker with its icon
// and stats, when someone asks about the item.
func ItemCardTool(opts ItemCardOptions) Tool {
	return Tool{
		Name:        "mlbb_item_card",
		Description: "Send the card of a Mobile Legends item, a sticker with its icon and stats, when someone asks about an item, what it gives or for its picture. Give the item's English name.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"item": {"type": "string", "description": "The item's English name, such as Athena's Shield or Blade of Despair."}
			},
			"required": ["item"],
			"additionalProperties": false
		}`),
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return sendItemCard(ctx, opts, raw)
		},
	}
}

func sendItemCard(ctx context.Context, opts ItemCardOptions, raw json.RawMessage) (any, error) {
	var args struct {
		Item string `json:"item"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	wanted := itemKey(args.Item)
	if wanted == "" || len([]rune(args.Item)) > maxItemQuery {
		return nil, &ArgumentError{Message: fmt.Sprintf("item must be an item's name of at most %d characters", maxItemQuery)}
	}
	sink, ok := SinkFrom(ctx)
	if !ok {
		return nil, errors.New("cards cannot be sent from here")
	}
	cards, err := opts.Store.ItemCards(ctx)
	if err != nil {
		return nil, internal(err)
	}
	card, ok := findCard(cards, wanted)
	if !ok {
		return nil, &ArgumentError{Message: fmt.Sprintf("there is no card for %q; the cards are of: %s", args.Item, cardNames(cards))}
	}
	sink.AddSticker(card.FileID)
	return map[string]any{"item": card.Name, "note": itemCardNote}, nil
}

// findCard is the card named wanted, or else the one card whose name holds
// it or is held by it: "athena" finds Athena's Shield, while "boots",
// which several names hold, finds none.
func findCard(cards []storage.ItemCardSticker, wanted string) (storage.ItemCardSticker, bool) {
	var near []storage.ItemCardSticker
	for _, c := range cards {
		key := itemKey(c.Name)
		if key == wanted {
			return c, true
		}
		if strings.Contains(key, wanted) || strings.Contains(wanted, key) {
			near = append(near, c)
		}
	}
	if len(near) == 1 {
		return near[0], true
	}
	return storage.ItemCardSticker{}, false
}

// itemKey folds a name to its letters and digits, lower-cased.
func itemKey(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, name)
}

func cardNames(cards []storage.ItemCardSticker) string {
	if len(cards) == 0 {
		return "none yet"
	}
	names := make([]string, 0, len(cards))
	for _, c := range cards {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}
