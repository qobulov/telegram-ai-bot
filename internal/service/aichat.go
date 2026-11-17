package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	// aiMemory is how long a group's AI setting is trusted before it is
	// read again.
	aiMemory = 2 * time.Minute
	// aiOffNoticeEvery is how often a group without AI is told so, when
	// someone talks to the bot there.
	aiOffNoticeEvery = 6 * time.Hour
)

// aiAllowed reports whether a group may talk with the model: one its owner
// turned it on for, with /aichat; an owner of the bot always may. Without
// a store every group may.
func (s *Service) aiAllowed(ctx context.Context, in incoming) bool {
	if !in.group || s.opts.Extras == nil || s.isOwner(in.msg.From) {
		return true
	}
	if on, ok := s.aiChats.get(in.scope.ChatID); ok {
		return on
	}
	value, err := s.opts.Extras.GroupSetting(ctx, in.scope.ChatID, storage.SettingAI)
	if err != nil {
		s.opts.Logger.Warn("could not read a group's AI setting", "chat_id", in.scope.ChatID, "error", err)
		return false
	}
	on := value == "on"
	s.aiChats.set(in.scope.ChatID, on)
	return on
}

// aiOff answers talk addressed to the bot in a group without AI: once in a
// while it says so, otherwise nothing.
func (s *Service) aiOff(in incoming) outcome {
	if _, told := s.aiOffTold.get(in.scope.ChatID); told {
		s.passOver(in)
		return outcome{skipped: "ai_off", passedOver: true}
	}
	s.aiOffTold.set(in.scope.ChatID, true)
	return outcome{text: "🤖 Bu guruhda AI suhbat yoqilmagan. Buyruqlar ishlaydi: /help", skipped: "ai_off"}
}

// aiChatCommand answers an owner's /aichat in a group: "on" or "off", or
// alone it switches; to anyone else it says nothing, as the command may be
// another bot's.
func (s *Service) aiChatCommand(ctx context.Context, in incoming) outcome {
	if !s.isOwner(in.msg.From) {
		return outcome{}
	}
	current, err := s.opts.Extras.GroupSetting(ctx, in.scope.ChatID, storage.SettingAI)
	if err != nil {
		return say(textError.to(in))
	}
	on := current != "on"
	switch strings.ToLower(strings.TrimSpace(commandArgument(in.text))) {
	case "on", "yoq", "ha":
		on = true
	case "off", "o'chir", "ochir", "yo'q":
		on = false
	}
	value := ""
	if on {
		value = "on"
	}
	if err := s.opts.Extras.SetGroupSetting(ctx, in.scope.ChatID, storage.SettingAI, value, in.userID); err != nil {
		return say(textError.to(in))
	}
	s.aiChats.set(in.scope.ChatID, on)
	if on {
		return say("🤖 AI suhbat yoqildi: endi meni chaqirib gaplashish mumkin. O'chirish: /aichat")
	}
	return say("🤖 AI suhbat o'chirildi: bu yerda faqat buyruqlar va o'yinlar ishlaydi. Yoqish: /aichat")
}

// aiData starts the button of a group in /aigroups: "ai:<chat ID>".
const aiData = "ai:"

// aiGroupsShown is how many of the bot's groups /aigroups lists, the
// latest active first.
const aiGroupsShown = 40

// aiGroupsCommand answers an owner's /aigroups in private: the bot's groups,
// each a button that turns AI on or off there.
func (s *Service) aiGroupsCommand(ctx context.Context, in incoming) outcome {
	if !s.isOwner(in.msg.From) {
		return say(textUnknown.to(in))
	}
	text, buttons, err := s.aiGroupsMenu(ctx)
	if err != nil {
		return say(textError.to(in))
	}
	return outcome{text: text, buttons: buttons}
}

func (s *Service) aiGroupsMenu(ctx context.Context) (string, [][]telegram.Button, error) {
	groups, err := s.opts.Extras.Groups(ctx, time.Now(), aiGroupsShown)
	if err != nil {
		return "", nil, err
	}
	on, err := s.opts.Extras.GroupsWithSetting(ctx, storage.SettingAI, "on")
	if err != nil {
		return "", nil, err
	}
	allowed := make(map[int64]bool, len(on))
	for _, g := range on {
		allowed[g.ChatID] = true
	}
	var rows [][]telegram.Button
	count := 0
	for _, g := range groups {
		if g.BotStatus != "member" && g.BotStatus != "administrator" {
			continue
		}
		mark := "❌"
		if allowed[g.ChatID] {
			mark, count = "✅", count+1
		}
		title := []rune(cardMarks.Replace(g.Title))
		if len(title) > 40 {
			title = append(title[:40], '…')
		}
		rows = append(rows, []telegram.Button{{Text: mark + " " + string(title), Data: aiData + fmt.Sprint(g.ChatID)}})
	}
	if len(rows) == 0 {
		return "🤖 Bot hali hech bir guruhda yo'q.", nil, nil
	}
	text := fmt.Sprintf("🤖 AI suhbat: %d ta guruhda yoqilgan.\nGuruhni bossangiz, unda AI yoqiladi yoki o'chadi. ❌ guruhlarda faqat buyruqlar, o'yinlar va moderatsiya ishlaydi, limitsiz.", count)
	return text, rows, nil
}

// pressedAIGroup turns AI on or off in the group pressed, for an owner, and
// shows the list again.
func (s *Service) pressedAIGroup(q *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	answer := func(text string, alert bool) { _ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, text, alert) }
	chatID, err := strconv.ParseInt(strings.TrimPrefix(q.Data, aiData), 10, 64)
	if err != nil || !s.isOwner(q.From) {
		answer("Buni faqat bot egalari o'zgartiradi.", true)
		return
	}
	current, err := s.opts.Extras.GroupSetting(ctx, chatID, storage.SettingAI)
	if err != nil {
		answer("Hozir bo'lmadi.", true)
		return
	}
	value, said := "on", "✅ AI yoqildi"
	if current == "on" {
		value, said = "", "❌ AI o'chirildi"
	}
	if err := s.opts.Extras.SetGroupSetting(ctx, chatID, storage.SettingAI, value, q.From.ID); err != nil {
		answer("Hozir bo'lmadi.", true)
		return
	}
	s.aiChats.set(chatID, value == "on")
	answer(said, false)
	if text, buttons, err := s.aiGroupsMenu(ctx); err == nil {
		_ = s.opts.Buttons.EditMessageMenu(ctx, q.Message.Chat.ID, q.Message.MessageID, text, buttons)
	}
}
