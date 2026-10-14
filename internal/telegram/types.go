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

	// Media. At most one of these is set on a message.
	Photo     []PhotoSize `json:"photo"` // the same picture in several sizes, smallest first
	Sticker   *Sticker    `json:"sticker"`
	Animation *Animation  `json:"animation"` // a GIF, which Telegram stores as a video
	Voice     *Voice      `json:"voice"`
}

// PhotoSize is one size of a photo, or a thumbnail.
type PhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// Sticker is a sticker. A static sticker is a WebP image; animated (Lottie)
// and video stickers cannot be shown as an image, but have a thumbnail.
type Sticker struct {
	FileID     string     `json:"file_id"`
	Emoji      string     `json:"emoji"`
	IsAnimated bool       `json:"is_animated"`
	IsVideo    bool       `json:"is_video"`
	FileSize   int64      `json:"file_size"`
	Thumbnail  *PhotoSize `json:"thumbnail"`
}

// Animation is a GIF or a silent video clip.
type Animation struct {
	FileID    string     `json:"file_id"`
	MimeType  string     `json:"mime_type"`
	Thumbnail *PhotoSize `json:"thumbnail"`
}

// Voice is a voice message: OGG audio encoded with Opus.
type Voice struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"` // seconds
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

// User is a Telegram account: the sender of a message, or the bot itself.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// ChatMember is a user's standing in a chat.
type ChatMember struct {
	Status string `json:"status"`
}

// IsAdmin reports whether the member owns or administers the chat.
func (m ChatMember) IsAdmin() bool {
	return m.Status == "creator" || m.Status == "administrator"
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
