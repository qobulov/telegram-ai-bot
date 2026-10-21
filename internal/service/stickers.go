package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"telegram-ai-bot/internal/stickers"
	"telegram-ai-bot/internal/storage"
)

const (
	// stickerGap is the least time between two stickers in one chat, so
	// that they stay a seasoning.
	stickerGap = 30 * time.Second
	// importBudget is how long one /import_stickers labels for, inside a
	// serverless request's minute; sending it again goes on.
	importBudget = 40 * time.Second
)

// StickerPicker chooses a sticker of a category for a chat, or says none
// may be sent now.
type StickerPicker interface {
	PickSticker(ctx context.Context, chatID int64, category string, gap time.Duration) (string, error)
}

// StickerSender sends a sticker.
type StickerSender interface {
	SendSticker(ctx context.Context, chatID int64, fileID string, replyToMessageID int64) (int64, error)
}

// StickerImporter brings a sticker pack in.
type StickerImporter interface {
	Import(ctx context.Context, name string) (stickers.Report, error)
}

// sendSticker sends a sticker of category after an answer and returns its
// message's ID, or 0 when none was sent: no category, an unknown one, none
// of it, or one sent to the chat too recently. A failure costs only the
// sticker, never the answer.
func (s *Service) sendSticker(in incoming, category string) int64 {
	if category == "" || s.opts.Stickers == nil || s.opts.StickerSender == nil || !stickers.IsCategory(category) {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	fileID, err := s.opts.Stickers.PickSticker(ctx, in.msg.Chat.ID, category, stickerGap)
	if errors.Is(err, storage.ErrNoSticker) {
		return 0
	}
	if err != nil {
		s.opts.Logger.Warn("could not pick a sticker", "chat_id", in.msg.Chat.ID, "category", category, "error", err)
		return 0
	}
	sentID, err := s.opts.StickerSender.SendSticker(ctx, in.msg.Chat.ID, fileID, in.replyTarget())
	if err != nil {
		s.opts.Logger.Warn("could not send a sticker", "chat_id", in.msg.Chat.ID, "category", category, "error", err)
		return 0
	}
	return sentID
}

// importStickers is /import_stickers <link or name>, for the bot's owners:
// it imports the pack and has the model label as many of its stickers as
// importBudget allows, and says how far it came.
func (s *Service) importStickers(ctx context.Context, in incoming) string {
	if !s.isOwner(in.msg.From) {
		return textForbidden
	}
	if s.opts.StickerImporter == nil {
		return "Stikerlar bu botda yoqilmagan."
	}
	_, arg, _ := strings.Cut(strings.TrimSpace(in.text), " ")
	name, ok := stickers.PackName(arg)
	if !ok {
		return "Pack havolasini yozing: /import_stickers https://t.me/addstickers/nomi"
	}
	ctx, cancel := context.WithTimeout(ctx, importBudget)
	defer cancel()
	report, err := s.opts.StickerImporter.Import(ctx, name)
	if err != nil {
		s.opts.Logger.Warn("sticker import failed", "pack", name, "error", err)
		return "Pack'ni o'qib bo'lmadi: nomi to'g'rimi?"
	}
	text := fmt.Sprintf("%s: %d ta stiker, %d tasi ko'rib chiqildi, %d tasi yuborishga yaroqli.", report.Pack, report.Total, report.Labelled, report.Enabled)
	if left := report.Total - report.Labelled; left > 0 {
		text += fmt.Sprintf(" Qolgan %d tasi uchun buyruqni yana yuboring.", left)
	}
	return text
}
