package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
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

// aiGroupsCommand answers an owner's /aigroups in private: the groups with
// AI on.
func (s *Service) aiGroupsCommand(ctx context.Context, in incoming) outcome {
	if !s.isOwner(in.msg.From) {
		return say(textUnknown.to(in))
	}
	groups, err := s.opts.Extras.GroupsWithSetting(ctx, storage.SettingAI, "on")
	if err != nil {
		return say(textError.to(in))
	}
	if len(groups) == 0 {
		return say("🤖 Hech bir guruhda AI suhbat yoqilmagan. Yoqish: guruhda /aichat on")
	}
	lines := []string{fmt.Sprintf("🤖 AI suhbat yoqilgan guruhlar (%d):", len(groups))}
	for _, g := range groups {
		title := cardMarks.Replace(g.Title)
		if title == "" {
			title = fmt.Sprint(g.ChatID)
		}
		lines = append(lines, "• "+title)
	}
	lines = append(lines, "", "Boshqa guruhlarda faqat buyruqlar va o'yinlar ishlaydi, limitsiz.")
	return say(strings.Join(lines, "\n"))
}
