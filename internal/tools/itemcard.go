package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/storage"
)

const (
	maxItemCards = 6
	maxItemQuery = 60
	itemCardNote = "The cards, with the items' stats, are sent as stickers after your answer, in this order: answer in one short line at most, without listing the stats, and say which items have no card."
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

// ItemCardTool sends Mobile Legends items' cards, stickers with an item's
// icon and stats, when someone asks about items or a build.
func ItemCardTool(opts ItemCardOptions) Tool {
	return Tool{
		Name:        "mlbb_item_card",
		Description: "Send the cards of Mobile Legends items, stickers with each item's icon and stats, when someone asks about items, what they give, or a build: up to 6 items in one call. Give the items' English names.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"items": {"type": "array", "items": {"type": "string"}, "minItems": 1, "maxItems": 6, "description": "The items' English names, such as Athena's Shield or Blade of Despair, in the order to send them."}
			},
			"required": ["items"],
			"additionalProperties": false
		}`),
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return sendItemCards(ctx, opts, raw)
		},
	}
}

func sendItemCards(ctx context.Context, opts ItemCardOptions, raw json.RawMessage) (any, error) {
	var args struct {
		Items []string `json:"items"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Items) < 1 || len(args.Items) > maxItemCards {
		return nil, &ArgumentError{Message: fmt.Sprintf("give 1 to %d items", maxItemCards)}
	}
	for _, item := range args.Items {
		if itemKey(item) == "" || len([]rune(item)) > maxItemQuery {
			return nil, &ArgumentError{Message: fmt.Sprintf("each item must be an item's name of at most %d characters", maxItemQuery)}
		}
	}
	sink, ok := SinkFrom(ctx)
	if !ok {
		return nil, errors.New("cards cannot be sent from here")
	}
	cards, err := opts.Store.ItemCards(ctx)
	if err != nil {
		return nil, internal(err)
	}
	var sent, missing []string
	for _, item := range args.Items {
		card, ok := findCard(cards, itemKey(item))
		if !ok || slices.Contains(sent, card.Name) {
			if !ok {
				missing = append(missing, item)
			}
			continue
		}
		sink.AddSticker(card.FileID)
		sent = append(sent, card.Name)
	}
	if len(sent) == 0 {
		return nil, &ArgumentError{Message: fmt.Sprintf("there is no card for %s; the cards are of: %s", strings.Join(missing, ", "), cardNames(cards))}
	}
	out := map[string]any{"sent": sent, "note": itemCardNote}
	if len(missing) > 0 {
		out["no_card"] = missing
	}
	return out, nil
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
