package stickers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
)

// CardStore keeps a pack of item cards and the items' names.
type CardStore interface {
	SaveStickers(ctx context.Context, stickers []storage.PackSticker) (int, error)
	UnnamedCards(ctx context.Context, pack string) ([]storage.PackSticker, error)
	LabelSticker(ctx context.Context, fileUniqueID string, categories []string, description string, enabled bool) error
	PackCards(ctx context.Context, pack string) (storage.CardCount, error)
}

// CardImporter brings in a pack of Mobile Legends item cards and has the
// model name the item on each, so that a card can be sent when someone
// asks about its item.
type CardImporter struct {
	Telegram Telegram
	Store    CardStore
	Model    Model
	Workers  int
	Logger   *slog.Logger
}

// CardReport is how far an import of item cards came.
type CardReport struct {
	Pack string
	storage.CardCount
}

// maxItemName bounds a name the model gives.
const maxItemName = 60

// cardPrompt asks the model which item a card shows.
const cardPrompt = `This is a Telegram sticker from a pack of Mobile Legends: Bang Bang item cards: an item's icon with its stats, the text maybe in Uzbek.
Answer with JSON only, no other text: {"item": "..."}
- item: the item's name as the game writes it in English, such as "Athena's Shield", "Blade of Despair" or "Magic Shoes"; "" if the sticker is not one item's card.`

// Import saves the pack's stickers and names the item on as many of those
// not yet looked at as ctx leaves time for; importing again picks up where
// it stopped. A sticker named before as a mood loses its moods.
func (im *CardImporter) Import(ctx context.Context, name string) (CardReport, error) {
	set, err := im.Telegram.GetStickerSet(ctx, name)
	if err != nil {
		return CardReport{}, fmt.Errorf("stickers: read the pack: %w", err)
	}
	pack := make([]storage.PackSticker, 0, len(set.Stickers))
	for _, st := range set.Stickers {
		pack = append(pack, storage.PackSticker{
			FileUniqueID: st.FileUniqueID, FileID: st.FileID, Pack: name, Emoji: st.Emoji, ImageFileID: imageOf(st),
		})
	}
	if _, err := im.Store.SaveStickers(ctx, pack); err != nil {
		return CardReport{}, err
	}
	waiting, err := im.Store.UnnamedCards(ctx, name)
	if err != nil {
		return CardReport{}, err
	}
	each(ctx, im.Workers, im.Logger, waiting, im.name)
	count, err := im.Store.PackCards(context.WithoutCancel(ctx), name)
	if err != nil {
		return CardReport{}, err
	}
	return CardReport{Pack: name, CardCount: count}, nil
}

// name has the model read one card and keeps the item's name; a sticker
// that shows no item, or no picture, is marked as no card.
func (im *CardImporter) name(ctx context.Context, st storage.PackSticker) error {
	if st.ImageFileID == "" {
		return im.Store.LabelSticker(ctx, st.FileUniqueID, []string{storage.NotItemCard}, "", false)
	}
	image, err := im.Telegram.DownloadFile(ctx, st.ImageFileID, maxImageBytes)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	reply, err := im.Model.Complete(ctx, ai.Request{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: cardPrompt, Parts: []ai.ContentPart{ai.ImagePart(http.DetectContentType(image), image)},
	}}})
	if err != nil {
		return fmt.Errorf("model: %w", err)
	}
	item, err := parseItem(reply.Content)
	if err != nil {
		return err
	}
	if item == "" {
		return im.Store.LabelSticker(ctx, st.FileUniqueID, []string{storage.NotItemCard}, "", false)
	}
	return im.Store.LabelSticker(ctx, st.FileUniqueID, []string{storage.ItemCard}, item, true)
}

// parseItem reads the item's name from the model's JSON, in a code fence or
// not.
func parseItem(content string) (string, error) {
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return "", errors.New("no JSON in the model's answer")
	}
	var got struct {
		Item string `json:"item"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &got); err != nil {
		return "", fmt.Errorf("the model's JSON: %w", err)
	}
	item := strings.Join(strings.Fields(got.Item), " ")
	if len([]rune(item)) > maxItemName {
		return "", errors.New("the model's item name is too long")
	}
	return item, nil
}
