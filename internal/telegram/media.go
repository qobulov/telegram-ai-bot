package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"strconv"
	"strings"
)

// Album sizes Telegram accepts in one sendMediaGroup.
const (
	MinAlbum = 2
	MaxAlbum = 10
)

// Album is 2 to 10 pictures sent together, shown as one grid, with Caption
// under the first.
type Album struct {
	ChatID           int64
	ReplyToMessageID int64
	Photos           [][]byte
	Caption          string
	// BusinessConnectionID, when set, sends it on behalf of that
	// connection's business account.
	BusinessConnectionID string
}

type inputMediaPhoto struct {
	Type            string          `json:"type"`
	Media           string          `json:"media"`
	Caption         string          `json:"caption,omitempty"`
	CaptionEntities []messageEntity `json:"caption_entities,omitempty"`
}

// SendMediaGroup uploads an album and returns the ID of its first message,
// which carries the caption: Telegram makes a message of each picture.
func (c *Client) SendMediaGroup(ctx context.Context, album Album) (int64, error) {
	if len(album.Photos) < MinAlbum || len(album.Photos) > MaxAlbum {
		return 0, fmt.Errorf("telegram sendMediaGroup: %d pictures, want %d to %d", len(album.Photos), MinAlbum, MaxAlbum)
	}
	body, contentType, err := albumForm(album)
	if err != nil {
		return 0, fmt.Errorf("telegram sendMediaGroup: encode: %w", err)
	}
	var sent []sentMessage
	if err := c.postForm(ctx, "sendMediaGroup", body, contentType, &sent); err != nil {
		return 0, err
	}
	if len(sent) == 0 {
		return 0, errors.New("telegram sendMediaGroup: no message in the result")
	}
	return sent[0].MessageID, nil
}

// albumForm is the album as a multipart form: the media list names each
// picture's part, "attach://photoN".
func albumForm(album Album) (io.Reader, string, error) {
	media := make([]inputMediaPhoto, len(album.Photos))
	for i := range album.Photos {
		media[i] = inputMediaPhoto{Type: "photo", Media: "attach://photo" + strconv.Itoa(i)}
	}
	if caption := strings.TrimSpace(album.Caption); caption != "" {
		media[0].Caption, media[0].CaptionEntities = formatted(cutRunes(caption, CaptionLimit))
	}
	encoded, err := json.Marshal(media)
	if err != nil {
		return nil, "", err
	}
	fields := map[string]string{"chat_id": strconv.FormatInt(album.ChatID, 10), "media": string(encoded)}
	if reply := replyTo(album.ReplyToMessageID); reply != nil {
		encoded, err := json.Marshal(reply)
		if err != nil {
			return nil, "", err
		}
		fields["reply_parameters"] = string(encoded)
	}
	if album.BusinessConnectionID != "" {
		fields["business_connection_id"] = album.BusinessConnectionID
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			return nil, "", err
		}
	}
	for i, photo := range album.Photos {
		part, err := form.CreateFormFile("photo"+strconv.Itoa(i), "image"+strconv.Itoa(i)+".png")
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(photo); err != nil {
			return nil, "", err
		}
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return &body, form.FormDataContentType(), nil
}

// DiceEmoji are the animated dice Telegram rolls itself: a die, darts,
// basketball, football, bowling and a slot machine.
var DiceEmoji = []string{"🎲", "🎯", "🏀", "⚽", "🎳", "🎰"}

type sendDiceParams struct {
	ChatID          int64            `json:"chat_id"`
	Emoji           string           `json:"emoji"`
	ReplyParameters *replyParameters `json:"reply_parameters,omitempty"`
}

type sentDice struct {
	MessageID int64 `json:"message_id"`
	Dice      struct {
		Value int `json:"value"`
	} `json:"dice"`
}

// SendDice has Telegram roll emoji, one of DiceEmoji, as a reply to
// replyToMessageID when it is not zero, and returns the sent message's ID
// and the value Telegram rolled: 1 to 6 for 🎲 🎯 🎳, 1 to 5 for 🏀 ⚽, 1
// to 64 for 🎰. Telegram draws it; the bot cannot choose it.
func (c *Client) SendDice(ctx context.Context, chatID int64, emoji string, replyToMessageID int64) (messageID int64, value int, err error) {
	var sent sentDice
	params := sendDiceParams{ChatID: chatID, Emoji: emoji, ReplyParameters: replyTo(replyToMessageID)}
	if err := c.call(ctx, "sendDice", params, &sent); err != nil {
		return 0, 0, err
	}
	return sent.MessageID, sent.Dice.Value, nil
}
