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

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// actionDice is how a roll is recorded in the moderation audit, as a poll
// is: that is what paces them.
const actionDice = "dice"

const (
	// A chat gets diceLimit rolls per diceWindow, so that games cannot
	// flood a group: each is a message with an animation.
	diceLimit  = 3
	diceWindow = 5 * time.Minute
	// diceTimeout is above the registry's 5 s default, as for a poll.
	diceTimeout = 15 * time.Second
)

const (
	notInAChatForDice = "dice can only be rolled in a chat"
	diceAskedNote     = "This message already rolled; one roll per message. Don't say how the first one landed unless you were told."
	diceNoAnswer      = "Telegram did not answer, so the roll may be in the chat or not; don't roll again for this message"
)

// diceTooSoon says how many rolls a chat gets, for the model to pass on.
var diceTooSoon = fmt.Sprintf("this chat had %d rolls in the last %d minutes; wait a little before the next", diceLimit, int(diceWindow.Minutes()))

// DiceSender has Telegram roll an animated die in a chat.
type DiceSender interface {
	SendDice(ctx context.Context, chatID int64, emoji string, replyToMessageID int64) (messageID int64, value int, err error)
}

// DiceOptions configures the send_dice tool. Sender and Store are required;
// Store is the audit polls are counted in.
type DiceOptions struct {
	Sender DiceSender
	Store  PollCounter
	Now    func() time.Time
}

type diceArgs struct {
	Emoji string `json:"emoji"`
}

// DiceTool lets anyone have the bot roll one of Telegram's animated dice
// in the chat the turn is in, group or private, and tells the model how it
// landed. Telegram draws the value: neither the model nor the asker can
// choose it.
func DiceTool(opts DiceOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	d := dice{opts}
	return Tool{
		Name: "send_dice",
		Description: "Roll one of Telegram's animated dice in this chat when someone asks to roll, throw, play or try their luck " +
			"(\"zar tashla\", \"kubik otib ber\", \"o'ynaymiz\", \"бросай кубик\", \"сыграем\"): 🎲 a die, 🎯 darts, 🏀 basketball, ⚽ football, 🎳 bowling, 🎰 a slot machine. " +
			"Telegram draws the result and you get it back: comment on it in one short line, and never name a result without calling this. " +
			"One roll per message; a chat gets 3 rolls every 5 minutes.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"emoji": {"type": "string", "enum": ["🎲", "🎯", "🏀", "⚽", "🎳", "🎰"], "description": "Which to roll; 🎲 when the game is not named."}
			},
			"additionalProperties": false
		}`),
		Timeout:    diceTimeout,
		NoBusiness: true,
		Run:        d.run,
	}
}

type dice struct {
	DiceOptions
}

// run records the roll, then has Telegram make it, as a poll is made: the
// record, unique per update, keeps a redelivered update from rolling twice,
// and counts against the chat at once. A roll that surely did not happen
// has its record dropped again.
func (d dice) run(ctx context.Context, raw json.RawMessage) (any, error) {
	var args diceArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	emoji := cmp.Or(strings.TrimSpace(args.Emoji), telegram.DiceEmoji[0])
	if !slices.Contains(telegram.DiceEmoji, emoji) {
		return nil, &ArgumentError{Message: "emoji must be one of " + strings.Join(telegram.DiceEmoji, " ")}
	}
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID == 0 || caller.UpdateID == 0 {
		return nil, &DeniedError{Message: notInAChatForDice}
	}
	first, err := d.Store.RecordModeration(ctx, storage.ModerationAction{
		ChatID: caller.ChatID, Action: actionDice, RequesterID: caller.UserID, UpdateID: caller.UpdateID,
	})
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"already_handled": true, "action": actionDice, "note": diceAskedNote}, nil
	}
	if err := d.checkPace(ctx, caller.ChatID); err != nil {
		return nil, d.forget(ctx, caller, err)
	}
	_, value, err := d.Sender.SendDice(ctx, caller.ChatID, emoji, caller.MessageID)
	if err != nil {
		if _, refused := telegramRefusal(err); refused {
			return nil, d.forget(ctx, caller, telegramFailure(err))
		}
		return nil, errors.New(diceNoAnswer)
	}
	return rolled(emoji, value), nil
}

// checkPace allows a chat diceLimit rolls per diceWindow, this one counted.
func (d dice) checkPace(ctx context.Context, chatID int64) error {
	made, err := d.Store.ChatModeration(ctx, chatID, actionDice, d.Now().Add(-diceWindow))
	if err != nil {
		return internal(err)
	}
	if made > diceLimit {
		return &ArgumentError{Message: diceTooSoon}
	}
	return nil
}

// forget drops the record of a roll that did not happen and returns why.
func (d dice) forget(ctx context.Context, caller Caller, why error) error {
	if err := d.Store.ForgetModeration(context.WithoutCancel(ctx), caller.UpdateID, actionDice); err != nil {
		return errors.Join(why, internal(err))
	}
	return why
}

// diceMax is the highest value of each die.
var diceMax = map[string]int{"🎲": 6, "🎯": 6, "🏀": 5, "⚽": 5, "🎳": 6, "🎰": 64}

// rolled is the result for the model: the value and, worked out here so
// that the model need not know Telegram's tables, what it means.
func rolled(emoji string, value int) map[string]any {
	return map[string]any{
		"emoji":   emoji,
		"value":   value,
		"max":     diceMax[emoji],
		"outcome": diceOutcome(emoji, value),
		"note":    "The animation is in the chat and lands on this. Comment in one short line; don't describe the animation.",
	}
}

// diceOutcome says what value means for emoji, as Telegram's animations
// show it.
func diceOutcome(emoji string, value int) string {
	switch emoji {
	case "🎯":
		switch value {
		case 1:
			return "missed the board"
		case 6:
			return "bullseye"
		}
		return "hit the board, not the centre"
	case "🏀":
		if value >= 4 {
			return "scored"
		}
		return "missed"
	case "⚽":
		if value >= 3 {
			return "goal"
		}
		return "missed"
	case "🎳":
		switch value {
		case 1:
			return "missed every pin"
		case 6:
			return "strike"
		}
		return "knocked down some pins"
	case "🎰":
		return slotOutcome(value)
	}
	return fmt.Sprintf("rolled %d", value)
}

// slotReels are the slot machine's symbols, in the order Telegram counts
// them: value-1 holds the three reels, left first, two bits each.
var slotReels = []string{"bar", "grapes", "lemon", "seven"}

// slotOutcome names the three reels of a slot value, 1 to 64, and whether
// they won: three of a kind, three sevens, 64, being the jackpot.
func slotOutcome(value int) string {
	if value < 1 || value > 64 {
		return fmt.Sprintf("rolled %d", value)
	}
	v := value - 1
	reels := []string{slotReels[v&3], slotReels[v>>2&3], slotReels[v>>4&3]}
	shown := strings.Join(reels, " ")
	switch {
	case value == 64:
		return shown + ": jackpot"
	case reels[0] == reels[1] && reels[1] == reels[2]:
		return shown + ": three of a kind, a win"
	}
	return shown + ": no win"
}
