package service

import (
	"context"
	"fmt"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// punishmentFor is the owners' or the group creator's punishment of kind for a member in a group:
// the one set for them, or else the group's for anyone. ok is false when
// there is none, or its text cannot be read.
func punishmentFor(rules []storage.GroupRule, chatID, userID int64, kind string) (rule storage.GroupRule, p storage.Punishment, ok bool) {
	for _, r := range rules {
		if r.Kind != kind || r.ChatID != chatID || (r.UserID != 0 && r.UserID != userID) {
			continue
		}
		if !ok || r.UserID != 0 {
			if parsed, valid := storage.ParsePunishment(r.Text); valid {
				rule, p, ok = r, parsed, true
			}
		}
	}
	return rule, p, ok
}

// punish carries out an owners' punishment on the sender of msg, and says
// why in the group. Telegram lets no bot ban or mute an admin, so an admin
// is named to caller instead; a failure says the bot lacks the right.
func (s *Service) punish(ctx context.Context, msg *telegram.Message, p storage.Punishment, role, why, caller string) {
	who := mentionOf(msg.From) + " " + why
	if role != "" {
		s.postGuard(msg, withCaller(who+"; buyruqqa ko'ra jazolanishi kerak edi, lekin u admin.", caller))
		return
	}
	var err error
	var text string
	if p.Ban() {
		if s.opts.GroupModerator == nil {
			return
		}
		err = s.opts.GroupModerator.BanChatMember(ctx, msg.Chat.ID, msg.From.ID)
		text = "🚫 " + who + ", buyruqqa ko'ra guruhdan chiqarildi."
	} else {
		if s.opts.Restrictor == nil {
			return
		}
		err = s.opts.Restrictor.RestrictChatMember(ctx, telegram.Restriction{
			ChatID: msg.Chat.ID, UserID: msg.From.ID, Permissions: telegram.NoPermissions(),
			Until: s.opts.Now().Add(time.Duration(p.MuteHours) * time.Hour),
		})
		text = fmt.Sprintf("🔇 %s, buyruqqa ko'ra %d soat yozolmaydi.", who, p.MuteHours)
	}
	if err != nil {
		s.opts.Logger.Warn("guards: could not carry out a punishment", "chat_id", msg.Chat.ID, "user_id", msg.From.ID, "punishment", p.String(), "error", err)
		text = withCaller(who+"; buyruqni bajara olmadim: menda bu huquq yo'q.", caller)
	}
	s.postGuard(msg, text)
}

// withCaller adds a call to whom the guard tags, when there is one.
func withCaller(text, caller string) string {
	if caller == "" {
		return text
	}
	return text + " " + caller + ", qarab qo'ying."
}
