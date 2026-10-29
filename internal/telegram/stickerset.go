package telegram

import (
	"context"
	"fmt"
	"strconv"
)

// MaxStickerSetTitle is the longest title Telegram takes for a sticker set.
const MaxStickerSetTitle = 64

// MaxStickerEmoji is how many emoji a sticker may stand for.
const MaxStickerEmoji = 20

// InputSticker is a static sticker to put in a set: a file uploaded with
// UploadStickerFile, and the emoji it stands for, 1 to MaxStickerEmoji.
type InputSticker struct {
	FileID string
	Emoji  []string
}

type inputSticker struct {
	Sticker   string   `json:"sticker"`
	Format    string   `json:"format"`
	EmojiList []string `json:"emoji_list"`
}

func (s InputSticker) params() inputSticker {
	return inputSticker{Sticker: s.FileID, Format: "static", EmojiList: s.Emoji}
}

// UploadStickerFile uploads a static sticker's PNG for userID, the owner
// of the set it is for, and returns its file ID for InputSticker.
func (c *Client) UploadStickerFile(ctx context.Context, userID int64, png []byte) (string, error) {
	body, contentType, err := uploadForm(upload{
		fields: map[string]string{"user_id": strconv.FormatInt(userID, 10), "sticker_format": "static"},
		field:  "sticker", filename: "sticker.png", data: png,
	})
	if err != nil {
		return "", fmt.Errorf("telegram uploadStickerFile: encode: %w", err)
	}
	var file struct {
		FileID string `json:"file_id"`
	}
	if err := c.postForm(ctx, "uploadStickerFile", body, contentType, &file); err != nil {
		return "", err
	}
	return file.FileID, nil
}

type newStickerSetParams struct {
	UserID   int64          `json:"user_id"`
	Name     string         `json:"name"`
	Title    string         `json:"title"`
	Stickers []inputSticker `json:"stickers"`
}

// CreateNewStickerSet makes a set of static stickers with its first
// sticker. It belongs to userID, and name must end in "_by_" and the
// bot's username.
func (c *Client) CreateNewStickerSet(ctx context.Context, userID int64, name, title string, first InputSticker) error {
	params := newStickerSetParams{UserID: userID, Name: name, Title: title, Stickers: []inputSticker{first.params()}}
	return c.call(ctx, "createNewStickerSet", params, nil)
}

type addStickerParams struct {
	UserID  int64        `json:"user_id"`
	Name    string       `json:"name"`
	Sticker inputSticker `json:"sticker"`
}

// AddStickerToSet adds a sticker at the end of a set the bot made, as
// userID, the set's owner.
func (c *Client) AddStickerToSet(ctx context.Context, userID int64, name string, sticker InputSticker) error {
	return c.call(ctx, "addStickerToSet", addStickerParams{UserID: userID, Name: name, Sticker: sticker.params()}, nil)
}

type stickerParams struct {
	Sticker string `json:"sticker"`
}

// DeleteStickerFromSet takes a sticker, by its file ID, out of a set the
// bot made.
func (c *Client) DeleteStickerFromSet(ctx context.Context, fileID string) error {
	return c.call(ctx, "deleteStickerFromSet", stickerParams{Sticker: fileID}, nil)
}

type stickerSetTitleParams struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// SetStickerSetTitle renames a set the bot made.
func (c *Client) SetStickerSetTitle(ctx context.Context, name, title string) error {
	return c.call(ctx, "setStickerSetTitle", stickerSetTitleParams{Name: name, Title: title}, nil)
}
