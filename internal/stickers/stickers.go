// Package stickers imports the sticker packs Maria may send from, and has
// the model look at each sticker once to say when it fits.
package stickers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// Categories are what the model may ask a sticker for, and what each
// sticker is labelled with.
var Categories = []string{
	"funny", "sarcasm", "flirting", "jealousy", "sad", "comfort", "shock",
	"celebration", "facepalm", "angry", "wholesome", "cool",
}

// IsCategory reports one of Categories.
func IsCategory(name string) bool {
	return slices.Contains(Categories, name)
}

// maxImageBytes bounds a sticker's picture; Telegram's are well under it.
const maxImageBytes = 1 << 20

// Telegram reads a pack and downloads its pictures.
type Telegram interface {
	GetStickerSet(ctx context.Context, name string) (telegram.StickerSet, error)
	DownloadFile(ctx context.Context, fileID string, maxBytes int64) ([]byte, error)
}

// Store keeps the stickers and their labels.
type Store interface {
	SaveStickers(ctx context.Context, stickers []storage.PackSticker) (int, error)
	UnlabelledStickers(ctx context.Context, pack string) ([]storage.PackSticker, error)
	LabelSticker(ctx context.Context, fileUniqueID string, categories []string, description string, enabled bool) error
	PackStickers(ctx context.Context, pack string) (storage.StickerCount, error)
}

// Model looks at a sticker.
type Model interface {
	Complete(ctx context.Context, req ai.Request) (ai.Message, error)
}

// Importer brings a pack in and labels its stickers.
type Importer struct {
	Telegram Telegram
	Store    Store
	Model    Model
	// Workers is how many stickers are looked at at once.
	Workers int
	Logger  *slog.Logger
}

// Report is how far an import came.
type Report struct {
	Pack  string
	Added int
	storage.StickerCount
}

// packName finds a pack's name: the last part of a t.me/addstickers link,
// or the name itself.
var packName = regexp.MustCompile(`^(?:https?://)?(?:t\.me|telegram\.me)/addstickers/([A-Za-z0-9_]{1,64})/?$|^([A-Za-z0-9_]{1,64})$`)

// PackName is the pack named by link, a t.me/addstickers link or the name.
func PackName(link string) (string, bool) {
	m := packName.FindStringSubmatch(strings.TrimSpace(link))
	if m == nil {
		return "", false
	}
	return m[1] + m[2], true
}

// Import saves the pack's stickers and labels as many of those not yet
// labelled as ctx leaves time for. Importing again picks up where it
// stopped, so a pack too big for one request is done in a few.
func (im *Importer) Import(ctx context.Context, name string) (Report, error) {
	set, err := im.Telegram.GetStickerSet(ctx, name)
	if err != nil {
		return Report{}, fmt.Errorf("stickers: read the pack: %w", err)
	}
	pack := make([]storage.PackSticker, 0, len(set.Stickers))
	for _, st := range set.Stickers {
		pack = append(pack, storage.PackSticker{
			FileUniqueID: st.FileUniqueID, FileID: st.FileID, Pack: name, Emoji: st.Emoji, ImageFileID: imageOf(st),
		})
	}
	added, err := im.Store.SaveStickers(ctx, pack)
	if err != nil {
		return Report{}, err
	}
	waiting, err := im.Store.UnlabelledStickers(ctx, name)
	if err != nil {
		return Report{}, err
	}
	im.labelAll(ctx, waiting)
	count, err := im.Store.PackStickers(context.WithoutCancel(ctx), name)
	if err != nil {
		return Report{}, err
	}
	return Report{Pack: name, Added: added, StickerCount: count}, nil
}

// imageOf is what shows a sticker as a picture: itself when static, its
// thumbnail when it moves.
func imageOf(st telegram.Sticker) string {
	if !st.IsAnimated && !st.IsVideo {
		return st.FileID
	}
	if st.Thumbnail != nil {
		return st.Thumbnail.FileID
	}
	return ""
}

// labelAll labels stickers, Workers at a time, until they are all done or
// ctx ends. A sticker that fails is left for the next import.
func (im *Importer) labelAll(ctx context.Context, stickers []storage.PackSticker) {
	each(ctx, im.Workers, im.Logger, stickers, im.label)
}

// each runs look on stickers, workers at a time, until they are all done or
// ctx ends, logging a sticker that fails; it is left for the next import.
func each(ctx context.Context, workers int, logger *slog.Logger, stickers []storage.PackSticker, look func(context.Context, storage.PackSticker) error) {
	jobs := make(chan storage.PackSticker)
	var wg sync.WaitGroup
	for range max(1, workers) {
		wg.Go(func() {
			for st := range jobs {
				if err := look(ctx, st); err != nil && ctx.Err() == nil && logger != nil {
					logger.Warn("could not look at a sticker", "pack", st.Pack, "sticker", st.FileUniqueID, "error", err)
				}
			}
		})
	}
	for _, st := range stickers {
		select {
		case jobs <- st:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
}

// labelPrompt asks the model what a sticker is for.
const labelPrompt = `This is a Telegram sticker. Maria, a playful, sarcastic group-chat character, might send it after her reply, as a reaction.
Answer with JSON only, no other text: {"categories": [...], "description": "...", "fit": true}
- categories: up to three of %s, for the moods this sticker shows; [] if none fits.
- description: up to 12 words, in English, on what it shows and its mood.
- fit: false if it is sexual, hateful, gory or cruel, mocks a nation, religion or tragedy, or carries text that does; true otherwise.`

type labels struct {
	Categories  []string `json:"categories"`
	Description string   `json:"description"`
	Fit         bool     `json:"fit"`
}

// label has the model look at one sticker and keeps what it said. A
// sticker without a picture cannot be looked at, and is not sent.
func (im *Importer) label(ctx context.Context, st storage.PackSticker) error {
	if st.ImageFileID == "" {
		return im.Store.LabelSticker(ctx, st.FileUniqueID, nil, "", false)
	}
	image, err := im.Telegram.DownloadFile(ctx, st.ImageFileID, maxImageBytes)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	prompt := fmt.Sprintf(labelPrompt, strings.Join(Categories, ", "))
	reply, err := im.Model.Complete(ctx, ai.Request{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: prompt, Parts: []ai.ContentPart{ai.ImagePart(http.DetectContentType(image), image)},
	}}})
	if err != nil {
		return fmt.Errorf("model: %w", err)
	}
	got, err := parseLabels(reply.Content)
	if err != nil {
		return err
	}
	return im.Store.LabelSticker(ctx, st.FileUniqueID, got.Categories, got.Description, got.Fit && len(got.Categories) > 0)
}

// parseLabels reads the model's JSON, in a code fence or not, keeping only
// known categories.
func parseLabels(content string) (labels, error) {
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return labels{}, errors.New("no JSON in the model's answer")
	}
	var got labels
	if err := json.Unmarshal([]byte(content[start:end+1]), &got); err != nil {
		return labels{}, fmt.Errorf("the model's JSON: %w", err)
	}
	known := got.Categories[:0]
	for _, c := range got.Categories {
		if c = strings.ToLower(strings.TrimSpace(c)); IsCategory(c) && !slices.Contains(known, c) {
			known = append(known, c)
		}
	}
	got.Categories = known
	got.Description = strings.Join(strings.Fields(got.Description), " ")
	return got, nil
}
