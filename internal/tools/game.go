package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
)

// actionGame is how a game sent is recorded in the moderation audit, as a
// roll of the dice is: that is what paces them.
const actionGame = "game"

const (
	// A chat gets gameLimit games per gameWindow: each is a message with a
	// picture and a button, and one is enough to play many times.
	gameLimit   = 2
	gameWindow  = 10 * time.Minute
	gameTimeout = 15 * time.Second
)

const (
	notInAChatForGame = "the game can only be sent in a chat"
	gameAskedNote     = "This message already got the game; one per message."
	gameNoAnswer      = "Telegram did not answer, so the game may be in the chat or not; don't send it again for this message"
	gameSentNote      = "The game is in the chat with its Play button: %s. Say in one short line that it is there; scores show under it."
	gameUnknown       = "there is no game by that name"
)

// gameAbout says what each of the bot's games is, by its short name.
var gameAbout = map[string]string{
	"tarozi":  "Tarozi, fruits on a bazaar scale, 40 seconds to say which pan goes down",
	"ilon":    "Kosmik ilon, a snake in space that eats planets and grows, with levels of walls, bonuses and ever quicker moves",
	"otinchi": "O'tinchi, a woodcutter chopping an old plane tree from left or right against the clock, dodging its branches, golden pieces worth five",
	"boss":    "Dev jangi, a boss raid: the whole chat taps one giant together, with damage numbers, critical blows, upgrades, and the bot announcing who hit it most",
}

var gameTooSoon = fmt.Sprintf("this chat got %d games in the last %d minutes; the one sent is still there to play", gameLimit, int(gameWindow.Minutes()))

// GameSender posts one of the bot's games in a chat.
type GameSender interface {
	SendGame(ctx context.Context, chatID int64, shortName string, replyToMessageID int64) (int64, error)
}

// GameOptions configures the send_game tool. All are required but Now.
type GameOptions struct {
	Sender GameSender
	// Store is the audit games are counted in, with polls and dice.
	Store PollCounter
	// Games are the games' short names in @BotFather, the first sent when
	// none is named.
	Games []string
	Now   func() time.Time
}

// GameTool lets anyone have the bot post one of its games in the chat the
// turn is in, as /game, /ilon, /otinchi and /boss do: the members play it inside Telegram,
// and their scores are kept on the message.
func GameTool(opts GameOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	g := gameTool{opts}
	var about []string
	for _, name := range opts.Games {
		about = append(about, name+": "+gameAbout[name])
	}
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{"game": map[string]any{
			"type": "string", "enum": opts.Games,
			"description": "Which game; " + opts.Games[0] + " when none is named. " + strings.Join(about, ". "),
		}},
		"additionalProperties": false,
	})
	return Tool{
		Name: "send_game",
		Description: "Post one of your own games in this chat when someone wants to play a game with you or the group, or asks for one to pass the time " +
			"(\"o'yin o'ynaymiz\", \"o'yin tashla\", \"ilon o'ynaymiz\", \"daraxt chopamiz\", \"dev uramiz\", \"boss\", \"давай поиграем в игру\", \"скинь игру\"); scores are kept under the message. " +
			"For dice, darts or a slot machine use send_dice instead. One per message; a chat gets 2 every 10 minutes.",
		Schema:  schema,
		Timeout: gameTimeout,
		Run:     g.run,
	}
}

type gameTool struct {
	GameOptions
}

// run records the game, then sends it, as a roll is made: the record,
// unique per update, keeps a redelivered update from sending it twice, and
// a game that surely was not sent has its record dropped again.
func (g gameTool) run(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Game string `json:"game"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Game == "" {
		args.Game = g.Games[0]
	}
	if !slices.Contains(g.Games, args.Game) {
		return nil, &ArgumentError{Message: gameUnknown}
	}
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID == 0 || caller.UpdateID == 0 {
		return nil, &DeniedError{Message: notInAChatForGame}
	}
	first, err := g.Store.RecordModeration(ctx, storage.ModerationAction{
		ChatID: caller.ChatID, Action: actionGame, RequesterID: caller.UserID, UpdateID: caller.UpdateID,
	})
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"already_handled": true, "action": actionGame, "note": gameAskedNote}, nil
	}
	if err := g.checkPace(ctx, caller.ChatID); err != nil {
		return nil, g.forget(ctx, caller, err)
	}
	if _, err := g.Sender.SendGame(ctx, caller.ChatID, args.Game, caller.MessageID); err != nil {
		if _, refused := telegramRefusal(err); refused {
			return nil, g.forget(ctx, caller, telegramFailure(err))
		}
		return nil, errors.New(gameNoAnswer)
	}
	return map[string]any{"sent": true, "game": args.Game, "note": fmt.Sprintf(gameSentNote, gameAbout[args.Game])}, nil
}

// checkPace allows a chat gameLimit games per gameWindow, this one counted.
func (g gameTool) checkPace(ctx context.Context, chatID int64) error {
	sent, err := g.Store.ChatModeration(ctx, chatID, actionGame, g.Now().Add(-gameWindow))
	if err != nil {
		return internal(err)
	}
	if sent > gameLimit {
		return &ArgumentError{Message: gameTooSoon}
	}
	return nil
}

// forget drops the record of a game that was not sent and returns why.
func (g gameTool) forget(ctx context.Context, caller Caller, why error) error {
	if err := g.Store.ForgetModeration(context.WithoutCancel(ctx), caller.UpdateID, actionGame); err != nil {
		return errors.Join(why, internal(err))
	}
	return why
}
