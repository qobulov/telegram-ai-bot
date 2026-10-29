package telegram

import (
	"context"
	"errors"
	"strings"
)

type sendGameParams struct {
	ChatID          int64            `json:"chat_id"`
	GameShortName   string           `json:"game_short_name"`
	ReplyParameters *replyParameters `json:"reply_parameters,omitempty"`
}

// SendGame posts one of the bot's games, by the short name it was given in
// @BotFather, as a reply to replyToMessageID when it is not zero, and
// returns the message's ID. Without a keyboard Telegram puts its own
// "Play" button under it, whose press comes back as a callback query with
// the game's short name.
func (c *Client) SendGame(ctx context.Context, chatID int64, shortName string, replyToMessageID int64) (int64, error) {
	var sent sentMessage
	params := sendGameParams{ChatID: chatID, GameShortName: shortName, ReplyParameters: replyTo(replyToMessageID)}
	if err := c.call(ctx, "sendGame", params, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

type answerGameParams struct {
	CallbackQueryID string `json:"callback_query_id"`
	URL             string `json:"url"`
}

// AnswerGameCallback answers a press of a game's Play button with the page
// that opens the game. Telegram opens it only for a game button.
func (c *Client) AnswerGameCallback(ctx context.Context, id, url string) error {
	return c.call(ctx, "answerCallbackQuery", answerGameParams{CallbackQueryID: id, URL: url}, nil)
}

// GameMessage is the game message a score belongs to: ChatID and
// MessageID for one the bot sent, or InlineMessageID for one sent in
// inline mode.
type GameMessage struct {
	UserID          int64  `json:"user_id"`
	ChatID          int64  `json:"chat_id,omitempty"`
	MessageID       int64  `json:"message_id,omitempty"`
	InlineMessageID string `json:"inline_message_id,omitempty"`
}

type setGameScoreParams struct {
	GameMessage
	Score int `json:"score"`
}

// SetGameScore records a player's score on a game message, which Telegram
// shows in the message's table. A score no better than their best is kept
// out, and reported as improved false rather than as an error.
func (c *Client) SetGameScore(ctx context.Context, game GameMessage, score int) (improved bool, err error) {
	err = c.call(ctx, "setGameScore", setGameScoreParams{GameMessage: game, Score: score}, nil)
	var refused *APIError
	if errors.As(err, &refused) && strings.Contains(refused.Description, "BOT_SCORE_NOT_MODIFIED") {
		return false, nil
	}
	return err == nil, err
}

// GameHighScore is one row of a game's table.
type GameHighScore struct {
	Position int  `json:"position"`
	User     User `json:"user"`
	Score    int  `json:"score"`
}

// GetGameHighScores is a game message's table as Telegram gives it to a
// player: their row and their neighbours', and the top three.
func (c *Client) GetGameHighScores(ctx context.Context, game GameMessage) ([]GameHighScore, error) {
	var scores []GameHighScore
	if err := c.call(ctx, "getGameHighScores", game, &scores); err != nil {
		return nil, err
	}
	return scores, nil
}

// WebAppInfo is the page of a Mini App.
type WebAppInfo struct {
	URL string `json:"url"`
}

type menuButton struct {
	Type   string      `json:"type"`
	Text   string      `json:"text,omitempty"`
	WebApp *WebAppInfo `json:"web_app,omitempty"`
}

type setChatMenuButtonParams struct {
	ChatID     int64      `json:"chat_id,omitempty"`
	MenuButton menuButton `json:"menu_button"`
}

// SetMenuWebApp makes the menu button of one private chat with the bot
// open a Mini App, labelled text. Telegram has such a button in private
// chats only.
func (c *Client) SetMenuWebApp(ctx context.Context, chatID int64, text, url string) error {
	params := setChatMenuButtonParams{ChatID: chatID, MenuButton: menuButton{Type: "web_app", Text: text, WebApp: &WebAppInfo{URL: url}}}
	return c.call(ctx, "setChatMenuButton", params, nil)
}

// SetMenuCommands puts the default menu button of private chats back to
// the list of commands.
func (c *Client) SetMenuCommands(ctx context.Context) error {
	return c.call(ctx, "setChatMenuButton", setChatMenuButtonParams{MenuButton: menuButton{Type: "commands"}}, nil)
}
