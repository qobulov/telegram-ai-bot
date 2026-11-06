package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/telegram"
)

const (
	// giftTimeout covers the gifts, the balance, the member's lookup and the
	// purchase, each a call to Telegram.
	giftTimeout = 20 * time.Second
	// maxGiftsListed keeps the list, cheapest first, within the result.
	maxGiftsListed = 40
	// maxGiftTextRunes is the longest text Telegram shows with a gift.
	maxGiftTextRunes = 128
	// giftAction is what the gift does to its member, for moderation's
	// lookup of a tag: they must be in the group.
	giftAction = "send_gift"

	giftActionList = "list"
	giftActionSend = "send"
)

// Refusals of the send_gift tool, in words the model can be given.
const (
	giftOwnersOnly  = "only my owners can have me send gifts"
	giftInAGroup    = "gifts are sent from a group: the owner replies there to the member's message, or tags them with @, and asks"
	giftNotAsked    = "their own message does not ask for a gift, so nothing was sent; send one only when an owner asks for it in words, such as \"sovg'a yubor\""
	giftNoTarget    = "reply to the message of the member the gift is for, or tag that one member with @ in the request"
	giftToABot      = "bots cannot get gifts; it must be a person"
	giftSoldOut     = "that gift is sold out; list the gifts and pick another"
	giftUnknown     = "there is no gift with that gift_id now; list the gifts and use one of their ids"
	giftSentNote    = "Sent. Tell them in one short line, in their language, what was sent to whom."
	giftListNote    = "These are the gifts on sale, cheapest first, with their cost in Stars, and your Stars balance. A gift that costs more than the balance cannot be sent: say how many Stars are missing, and that an owner can add Stars to the bot's balance."
	giftLowBalance  = "my Stars balance is %d and that gift costs %d, so it was not sent; %d more Stars are needed on the bot's balance"
	giftTextTooLong = "text is at most 128 characters"
)

// giftWords ask for a gift: "sovg'a yubor", "подари", "gift".
var giftWords = []string{"sovg", "совг", "gift", "подар", "hadya", "tuhfa", "туҳфа", "тухфа"}

// GiftShop buys gifts on Telegram with the bot's own Stars.
type GiftShop interface {
	GetAvailableGifts(ctx context.Context) ([]telegram.Gift, error)
	SendGift(ctx context.Context, userID int64, giftID, text string) error
	GetMyStarBalance(ctx context.Context) (int, error)
}

// GiftOptions configures the send_gift tool. Every field is required.
type GiftOptions struct {
	Shop GiftShop
	// Members finds a tagged member, as the moderate tool does: the same
	// confirmation from Telegram that the tag names that member.
	Members *Moderation
	BotID   int64
}

type giftArgs struct {
	Action string `json:"action"`
	GiftID string `json:"gift_id"`
	Text   string `json:"text"`
}

type listedGift struct {
	ID    string `json:"id"`
	Emoji string `json:"emoji,omitempty"`
	Stars int    `json:"stars"`
	// Left is how many remain of a limited gift.
	Left *int `json:"left,omitempty"`
}

// GiftTool lets the bot's owners spend its Stars on Telegram gifts for a
// group's members. Who gets one is never the model's choice: it is the
// member the owner replied to or tagged, taken from the update, and the
// owner's own words must ask for a gift, so that a line in the chat cannot
// talk the model into spending. The balance is checked before buying.
func GiftTool(opts GiftOptions) Tool {
	return Tool{
		Name: "send_gift",
		Description: "Owners only: Telegram gifts, bought with your own Stars. " +
			"list: the gifts on sale with their cost in Stars, and your Stars balance. " +
			"send (gift_id from list, optional text of up to 128 characters shown with it): send that gift, in a group, to the member the owner replied to or tagged with @. " +
			"Use it only when an owner asks for a gift in their own words; when the balance is too low, say how many Stars are missing. Never say a gift was sent unless the tool says so.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["list", "send"]},
				"gift_id": {"type": "string", "description": "For send: the id of a gift from list."},
				"text": {"type": "string", "description": "For send: an optional short message shown with the gift, at most 128 characters."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		OwnerOnly:      true,
		Timeout:        giftTimeout,
		MaxOutputBytes: 8000,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runGift(ctx, opts, raw)
		},
	}
}

func runGift(ctx context.Context, opts GiftOptions, raw json.RawMessage) (any, error) {
	var args giftArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	// OwnerOnly already keeps others out; this is checked in code too, as
	// it spends the bot's money.
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Human || !caller.Owner {
		return nil, &DeniedError{Message: giftOwnersOnly}
	}
	switch args.Action {
	case giftActionList:
		return listGifts(ctx, opts)
	case giftActionSend:
		return sendGift(ctx, opts, caller, args)
	}
	return nil, &ArgumentError{Message: "action must be list or send"}
}

// listGifts is the gifts on sale, cheapest first, and the balance.
func listGifts(ctx context.Context, opts GiftOptions) (any, error) {
	gifts, balance, err := giftsAndBalance(ctx, opts.Shop)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(gifts, func(a, b telegram.Gift) int { return cmp.Compare(a.StarCount, b.StarCount) })
	listed := make([]listedGift, 0, min(len(gifts), maxGiftsListed))
	for _, gift := range gifts {
		if soldOut(gift) {
			continue
		}
		if len(listed) == maxGiftsListed {
			break
		}
		item := listedGift{ID: gift.ID, Emoji: gift.Sticker.Emoji, Stars: gift.StarCount}
		if gift.TotalCount > 0 {
			left := gift.RemainingCount
			item.Left = &left
		}
		listed = append(listed, item)
	}
	return map[string]any{"balance": balance, "gifts": listed, "note": giftListNote}, nil
}

// sendGift buys the gift for the member the owner means, once their own
// words ask for one and the balance covers it.
func sendGift(ctx context.Context, opts GiftOptions, caller Caller, args giftArgs) (any, error) {
	switch {
	case caller.ChatID >= 0 || caller.UpdateID == 0:
		return nil, &DeniedError{Message: giftInAGroup}
	case !askedForGift(caller.Words):
		return nil, &DeniedError{Message: giftNotAsked}
	case strings.TrimSpace(args.GiftID) == "":
		return nil, &ArgumentError{Message: "gift_id is required for send; list the gifts first"}
	}
	text := strings.TrimSpace(args.Text)
	if utf8.RuneCountInString(text) > maxGiftTextRunes {
		return nil, &ArgumentError{Message: giftTextTooLong}
	}
	member, err := giftTarget(ctx, opts, caller)
	if err != nil {
		return nil, err
	}
	gifts, balance, err := giftsAndBalance(ctx, opts.Shop)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(gifts, func(g telegram.Gift) bool { return g.ID == strings.TrimSpace(args.GiftID) })
	if i < 0 {
		return nil, &ArgumentError{Message: giftUnknown}
	}
	gift := gifts[i]
	switch {
	case soldOut(gift):
		return nil, &DeniedError{Message: giftSoldOut}
	case balance < gift.StarCount:
		return nil, &DeniedError{Message: fmt.Sprintf(giftLowBalance, balance, gift.StarCount, gift.StarCount-balance)}
	}
	if err := opts.Shop.SendGift(ctx, member.UserID, gift.ID, text); err != nil {
		return nil, giftFailure(err)
	}
	return map[string]any{
		"done":    true,
		"gift":    gift.Sticker.Emoji,
		"stars":   gift.StarCount,
		"to":      member.Name,
		"balance": balance - gift.StarCount,
		"note":    giftSentNote,
	}, nil
}

// giftTarget is whom a gift is for: the author of the message the owner
// replied to, unless that is their own, or else the one member they tagged,
// once Telegram confirms who that is. Never anyone the model names.
func giftTarget(ctx context.Context, opts GiftOptions, caller Caller) (Target, error) {
	var member Target
	replied := caller.Target
	switch {
	case replied != nil && replied.UserID != caller.UserID:
		member = *replied
	case len(caller.Mentions) > 1:
		return Target{}, &DeniedError{Message: manyMentions}
	case len(caller.Mentions) == 1:
		tagged, err := opts.Members.tagged(ctx, caller.ChatID, caller.Mentions[0], giftAction)
		if err != nil {
			return Target{}, err
		}
		member = tagged
	default:
		return Target{}, &DeniedError{Message: giftNoTarget}
	}
	if member.IsBot || member.UserID == opts.BotID || isTelegramAccount(member.UserID) {
		return Target{}, &DeniedError{Message: giftToABot}
	}
	return member, nil
}

// giftsAndBalance asks Telegram for the gifts on sale and the bot's Stars.
func giftsAndBalance(ctx context.Context, shop GiftShop) ([]telegram.Gift, int, error) {
	gifts, err := shop.GetAvailableGifts(ctx)
	if err != nil {
		return nil, 0, giftFailure(err)
	}
	balance, err := shop.GetMyStarBalance(ctx)
	if err != nil {
		return nil, 0, giftFailure(err)
	}
	return gifts, balance, nil
}

// soldOut reports a limited gift with none left.
func soldOut(gift telegram.Gift) bool {
	return gift.TotalCount > 0 && gift.RemainingCount <= 0
}

// giftFailure is Telegram's refusal, for the model to pass on, or a hidden
// failure for anything else.
func giftFailure(err error) error {
	var refused *telegram.APIError
	if errors.As(err, &refused) {
		return &DeniedError{Message: "Telegram refused: " + refused.Description}
	}
	return internal(err)
}

// askedForGift reports whether words, what the owner wrote themselves, ask
// for a gift.
func askedForGift(words string) bool {
	folded := foldApostrophes.Replace(strings.ToLower(words))
	return slices.ContainsFunc(giftWords, func(word string) bool { return strings.Contains(folded, word) })
}
