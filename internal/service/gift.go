package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// CardMailer sends the owners' gifts to the cards app's mailbox.
type CardMailer interface {
	SendCardMail(ctx context.Context, m storage.CardMail) (int64, error)
	MemberByUsername(ctx context.Context, username string) (int64, error)
}

// maxGift caps one /gift, against a slipped zero.
var maxGift = map[string]int{"box": 50, "tokens": 1000, "crystals": 100000}

const (
	textGiftUsage = "Ishlatish: /gift @user box 2 [izoh]\n" +
		"Turi: box (Almaz box), tokens yoki crystals.\n" +
		"Misol: /gift @Unbrokeb box 2 Tabriklaymiz!\n" +
		"@user o'rniga Telegram ID ham bo'ladi."
	textGiftUnknownUser = "%s topilmadi: bot uni hech bir guruhda ko'rmagan. Telegram ID bilan yozing."
	textGiftSent        = "📬 %s ga yuborildi: %s.\nIlovada 🎒 → Pochta orqali oladi."
	textGiftNotNotified = "\n⚠️ Unga xabar yetmadi (botga hali yozmagan), lekin sovg'a pochtasida turibdi."
	textGiftNotice      = "📬 Sizga sovg'a keldi: %s!\nKartalar ilovasida 🎒 → Pochta: /card"
)

// giftCommand is an owner's /gift: a letter with boxes, tokens or crystals
// to a player's mailbox in the cards app, and a note to the player.
func (s *Service) giftCommand(ctx context.Context, in incoming) string {
	if !s.isOwner(in.msg.From) || s.opts.Mail == nil {
		return textUnknown.to(in)
	}
	fields := strings.Fields(in.text)
	if len(fields) < 4 {
		return textGiftUsage
	}
	kind := strings.ToLower(fields[2])
	limit, known := maxGift[kind]
	amount, err := strconv.Atoi(fields[3])
	if !known || err != nil || amount < 1 || amount > limit {
		return textGiftUsage
	}
	who := fields[1]
	userID, err := strconv.ParseInt(who, 10, 64)
	if err != nil {
		userID, err = s.opts.Mail.MemberByUsername(ctx, who)
		if errors.Is(err, storage.ErrUnknownAuthor) {
			return fmt.Sprintf(textGiftUnknownUser, who)
		}
		if err != nil {
			s.opts.Logger.Error("could not find a gift's player", "who", who, "error", err)
			return textError.to(in)
		}
	}
	letter := storage.CardMail{UserID: userID, Note: strings.Join(fields[4:], " "), SentBy: in.msg.From.ID}
	switch kind {
	case "box":
		letter.Box, letter.Boxes = "box", amount
	case "tokens":
		letter.Tokens = amount
	case "crystals":
		letter.Crystals = amount
	}
	if _, err := s.opts.Mail.SendCardMail(ctx, letter); err != nil {
		s.opts.Logger.Error("could not send a gift", "user_id", userID, "error", err)
		return textError.to(in)
	}
	s.opts.Logger.Info("gift sent", "user_id", userID, "kind", kind, "amount", amount, "by", in.msg.From.ID)
	what := giftLabel(kind, amount)
	reply := fmt.Sprintf(textGiftSent, who, what)
	if _, err := s.opts.Sender.SendMessage(ctx, telegram.Outgoing{ChatID: userID, Text: fmt.Sprintf(textGiftNotice, what)}); err != nil {
		reply += textGiftNotNotified
	}
	return reply
}

func giftLabel(kind string, amount int) string {
	switch kind {
	case "box":
		return fmt.Sprintf("🎁 %d ta Almaz box", amount)
	case "tokens":
		return fmt.Sprintf("🎟 %d token", amount)
	default:
		return fmt.Sprintf("💎 %d kristall", amount)
	}
}
