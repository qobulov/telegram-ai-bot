package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"telegram-ai-bot/internal/stickers"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
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
// may be sent now, and tells whether there are any to send at all.
type StickerPicker interface {
	PickSticker(ctx context.Context, chatID int64, category string, gap time.Duration) (string, error)
	HasStickers(ctx context.Context) (bool, error)
}

// noStickers tells the model, before any pack is imported, that it has no
// stickers: told nothing, it said "here you are" with nothing to show.
const noStickers = "[You have no stickers yet: an owner must import a pack first. Never write a sticker marker, and never say you sent or can send a sticker.]"

// noStickersHint is noStickers when there are no stickers to send, and ""
// otherwise, when that cannot be told, or when stickers are not wired at all.
func (s *Service) noStickersHint(ctx context.Context) string {
	if s.opts.Stickers == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	has, err := s.opts.Stickers.HasStickers(ctx)
	if err != nil || has {
		return ""
	}
	return noStickers
}

// GIFSearcher finds GIFs for a sticker category.
type GIFSearcher interface {
	Search(ctx context.Context, category string) ([]string, error)
}

// AnimationSender sends a GIF.
type AnimationSender interface {
	SendAnimation(ctx context.Context, chatID int64, animation string, replyToMessageID int64) (int64, error)
}

// MediaSlots spaces the stickers and GIFs a chat gets.
type MediaSlots interface {
	ClaimMediaSlot(ctx context.Context, chatID int64, gap time.Duration) (bool, error)
}

// sendMedia sends the sticker or GIF the model asked for, and returns its
// message's ID, or 0. A GIF of a mood that cannot be sent becomes a sticker
// of that mood; one of a topic, such as "pubg", has none to become. What
// was not sent is cleared from out, so that the log tells later turns only
// of what the chat saw.
func (s *Service) sendMedia(in incoming, out *outcome) int64 {
	if out.gif != "" && (s.opts.GIFs == nil || s.opts.AnimationSender == nil || s.opts.MediaSlots == nil) {
		out.sticker, out.gif = out.gif, ""
	}
	if out.gif != "" {
		id, claimed := s.sendGIF(in, out.gif)
		if id != 0 {
			return id
		}
		if claimed && stickers.IsCategory(out.gif) {
			// The slot is this answer's already; a sticker may use it.
			out.sticker = out.gif
			out.gif = ""
			return s.sendStickerWithin(in, out, 0)
		}
		if claimed {
			// A GIF of a topic was asked for and none is about it: say so
			// rather than leave the asker without an answer.
			out.gif, out.text = "", textNoGIF
			return s.reply(in, textNoGIF)
		}
		out.gif = ""
		return 0
	}
	return s.sendStickerWithin(in, out, stickerGap)
}

func (s *Service) sendStickerWithin(in incoming, out *outcome, gap time.Duration) int64 {
	id := s.sendSticker(in, out.sticker, gap)
	if id == 0 {
		out.sticker = ""
	}
	return id
}

// sendGIF sends a GIF of category, a mood or a topic, and returns its
// message's ID, or 0, and whether it took the chat's slot for one. A
// failure after that costs only the GIF.
func (s *Service) sendGIF(in incoming, category string) (int64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	// A GIF of a topic was asked for, so the gap that keeps the bot's own
	// stickers a seasoning does not hold it back.
	gap := stickerGap
	if !stickers.IsCategory(category) {
		gap = 0
	}
	claimed, err := s.opts.MediaSlots.ClaimMediaSlot(ctx, in.msg.Chat.ID, gap)
	if err != nil || !claimed {
		return 0, false
	}
	links, err := s.opts.GIFs.Search(ctx, category)
	if err != nil || len(links) == 0 {
		s.opts.Logger.Warn("could not find a GIF", "chat_id", in.msg.Chat.ID, "category", category, "error", err)
		return 0, true
	}
	sentID, err := s.opts.AnimationSender.SendAnimation(ctx, in.msg.Chat.ID, links[rand.IntN(len(links))], in.replyTarget())
	if err != nil {
		s.opts.Logger.Warn("could not send a GIF", "chat_id", in.msg.Chat.ID, "category", category, "error", err)
		return 0, true
	}
	return sentID, true
}

// AudioSender sends a track.
type AudioSender interface {
	SendAudio(ctx context.Context, track telegram.Track) (int64, error)
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
func (s *Service) sendSticker(in incoming, category string, gap time.Duration) int64 {
	if category == "" || s.opts.Stickers == nil || s.opts.StickerSender == nil || !stickers.IsCategory(category) {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	fileID, err := s.opts.Stickers.PickSticker(ctx, in.msg.Chat.ID, category, gap)
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
