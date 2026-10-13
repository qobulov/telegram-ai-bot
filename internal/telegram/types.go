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
// documents and other non-text content; a photo or document may carry a
// Caption instead.
type Message struct {
	MessageID       int64           `json:"message_id"`
	From            *User           `json:"from"`
	Chat            Chat            `json:"chat"`
	Text            string          `json:"text"`
	Entities        []MessageEntity `json:"entities"`
	Caption         string          `json:"caption"`
	CaptionEntities []MessageEntity `json:"caption_entities"`
	// ReplyToMessage is the message this one replies to, if any.
	ReplyToMessage *Message `json:"reply_to_message"`
	// SenderChat is set when the message was sent on behalf of a chat: a
	// channel, or a group posting as itself (anonymous administrators).
	SenderChat *Chat `json:"sender_chat"`
	// IsAutomaticForward marks a channel post forwarded automatically to the
	// channel's linked discussion group.
	IsAutomaticForward bool `json:"is_automatic_forward"`
}

// User is a Telegram account: the sender of a message, or the bot itself.
type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}

// Chat is where a message was sent.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// Chat types. Channels are not handled by the bot.
const (
	ChatTypePrivate    = "private"
	ChatTypeGroup      = "group"
	ChatTypeSupergroup = "supergroup"
)

// Outgoing is a text message to send.
type Outgoing struct {
	ChatID int64
	Text   string
	// ReplyToMessageID makes the message a reply to that message in the same
	// chat. Zero sends an ordinary message.
	ReplyToMessageID int64
}

// APIError is an error returned by the Bot API.
type APIError struct {
	Code        int
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: %d %s", e.Code, e.Description)
}
