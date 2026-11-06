package service

import (
	"context"
	"fmt"

	"telegram-ai-bot/internal/telegram"
)

// BoostBook keeps the boosts given to groups, so that each is thanked once.
type BoostBook interface {
	NoteBoost(ctx context.Context, chatID int64, boostID string) (bool, error)
}

// Thanks for a boost, in the booster's app language: by first name, or
// to nobody in particular for a boost Telegram gives no person for.
var (
	textBoostThanks = localText{
		uz: "Rahmat, %s, guruhga boost berganingiz uchun! 🚀",
		ru: "Спасибо, %s, за буст группы! 🚀",
		en: "Thank you, %s, for boosting the group! 🚀",
	}
	textBoostThanksAll = localText{
		uz: "Guruhga boost uchun rahmat! 🚀",
		ru: "Спасибо за буст группы! 🚀",
		en: "Thanks for boosting the group! 🚀",
	}
)

// thankBoost thanks a group, in code and without the model, for a boost
// Telegram reports, which it does only where the bot is an admin. Each
// boost is noted first and thanked once: Telegram sends one again when it
// changes, such as when it is renewed. When it cannot be noted, or the bot
// is muted there, nothing is said: thanks said twice, or in a group that
// asked for silence, are worse than none.
func (s *Service) thankBoost(update *telegram.ChatBoostUpdated) {
	chat, boost := update.Chat, update.Boost
	isGroup := chat.Type == telegram.ChatTypeGroup || chat.Type == telegram.ChatTypeSupergroup
	if s.opts.Boosts == nil || !isGroup || boost.BoostID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	isNew, err := s.opts.Boosts.NoteBoost(ctx, chat.ID, boost.BoostID)
	if err != nil {
		s.opts.Logger.Warn("could not note a boost; not thanking for it", "chat_id", chat.ID, "error", err)
		return
	}
	if !isNew || s.isMutedLately(chat.ID) {
		return
	}
	ctx, cancel = context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, err := s.opts.Sender.SendMessage(ctx, telegram.Outgoing{ChatID: chat.ID, Text: boostThanks(boost.Source.User)}); err != nil {
		s.opts.Logger.Warn("could not thank for a boost", "chat_id", chat.ID, "error", err)
		return
	}
	s.opts.Logger.Info("thanked for a boost", "chat_id", chat.ID, "source", boost.Source.Source)
}

// boostThanks thanks booster by first name, in their app's language.
func boostThanks(booster *telegram.User) string {
	language := appLanguage(booster)
	if booster == nil || authorLabel(booster.FirstName) == unknownAuthor {
		return textBoostThanksAll.inLanguage(language)
	}
	return fmt.Sprintf(textBoostThanks.inLanguage(language), authorLabel(booster.FirstName))
}
