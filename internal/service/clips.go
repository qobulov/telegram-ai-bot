package service

import (
	"context"
	_ "embed"
	"math/rand/v2"

	"telegram-ai-bot/internal/telegram"
)

// The clips the guard answers a swear at the bot with, chosen by the
// owners: "Блокка кетдинг", wide, and "Aqlini kiritib", square, which is
// sent round.
var (
	//go:embed clips/blokka.mp4
	blokkaClip []byte
	//go:embed clips/aqlini.mp4
	aqliniClip []byte

	swearClips = []telegram.Clip{{Data: blokkaClip}, {Data: aqliniClip, Round: true}}
)

// ClipSender uploads a short video.
type ClipSender interface {
	SendClip(ctx context.Context, clip telegram.Clip) (int64, error)
}

// replyWithClip answers msg with one of the swear clips, at random, and
// logs it as the bot's own message.
func (s *Service) replyWithClip(ctx context.Context, msg *telegram.Message) {
	if s.opts.ClipSender == nil {
		return
	}
	clip := swearClips[rand.IntN(len(swearClips))]
	clip.ChatID, clip.ReplyToMessageID = msg.Chat.ID, msg.MessageID
	sentID, err := s.opts.ClipSender.SendClip(ctx, clip)
	if err != nil {
		s.opts.Logger.Warn("swear guard: could not send a clip", "chat_id", msg.Chat.ID, "error", err)
		return
	}
	s.ambient.add(msg.Chat.ID, sentID, s.opts.Bot.ID, botAuthor, "<video in answer to the swearing>")
}
