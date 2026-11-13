package service

import (
	"context"
	"fmt"

	"telegram-ai-bot/internal/telegram"
)

// greetGroup says who the bot is in a group it was just added to: how to
// call it, its games, and, when it is not an admin, what it could do as one.
// Of 31 groups, 18 removed it or saw it leave, most within a day of adding it
// and before anyone spoke to it, as nothing told them what it was for.
func (s *Service) greetGroup(change *telegram.ChatMemberUpdated) {
	if !joinedGroup(change) || s.opts.Sender == nil {
		return
	}
	language := appLanguage(change.From)
	text := fmt.Sprintf(textBotJoined.inLanguage(language), s.opts.Bot.Username)
	if change.NewChatMember.Status != "administrator" {
		text += "\n" + textBotJoinedAdmin.inLanguage(language)
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, err := s.opts.Sender.SendMessage(ctx, telegram.Outgoing{ChatID: change.Chat.ID, Text: text}); err != nil {
		s.opts.Logger.Warn("could not greet a group", "chat_id", change.Chat.ID, "error", err)
	}
}

// joinedGroup reports the bot added to a group it was not in: not a change
// of its rights in one it is in already.
func joinedGroup(change *telegram.ChatMemberUpdated) bool {
	if change.Chat.Type != telegram.ChatTypeGroup && change.Chat.Type != telegram.ChatTypeSupergroup {
		return false
	}
	switch change.OldChatMember.Status {
	case "member", "administrator", "restricted":
		return false
	}
	switch change.NewChatMember.Status {
	case "member", "administrator":
		return true
	}
	return false
}

var (
	textBotJoined = localText{
		uz: "👋 Salom! Men guruhingiz uchun AI yordamchiman.\n💬 Menga yozish: xabarimga javob qiling yoki @%s deb yozing.\n🎴 Kartalar o'yini: /karta · ilova: /kartalar\n🎮 Boshqa o'yinlar: /oyinlar",
		ru: "👋 Привет! Я ИИ-помощник для вашей группы.\n💬 Чтобы написать мне, ответьте на моё сообщение или упомяните @%s.\n🎴 Игра в карты: /karta · приложение: /kartalar\n🎮 Другие игры: /oyinlar",
		en: "👋 Hi! I am an AI helper for your group.\n💬 To talk to me, reply to my message or mention @%s.\n🎴 The card game: /karta · the app: /kartalar\n🎮 More games: /oyinlar",
	}
	textBotJoinedAdmin = localText{
		uz: "🛡 Spam va reklamani tozalashim uchun meni admin qiling.",
		ru: "🛡 Сделайте меня админом, чтобы я убирал спам и рекламу.",
		en: "🛡 Make me an admin so that I can clear spam and ads.",
	}
)
