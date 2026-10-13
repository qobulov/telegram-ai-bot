// Package telegram is a small client for the Telegram Bot API: long polling
// for updates and sending text messages.
package telegram

import (
	"fmt"
	"time"
)

// Update is one incoming event. Message is nil for update kinds the bot does
// not subscribe to.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Message is an incoming chat message. Text is empty for photos, stickers,
// documents and other non-text content.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

// User is the sender of a message.
type User struct {
	ID    int64 `json:"id"`
	IsBot bool  `json:"is_bot"`
}

// Chat is where a message was sent. Type is "private", "group", "supergroup"
// or "channel".
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// ChatTypePrivate is the Chat.Type of a one-to-one conversation with the bot.
const ChatTypePrivate = "private"

// APIError is an error returned by the Bot API.
type APIError struct {
	Code        int
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: %d %s", e.Code, e.Description)
}
