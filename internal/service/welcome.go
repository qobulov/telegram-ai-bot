package service

import (
	"context"
	"strings"

	"telegram-ai-bot/internal/storage"
)

// welcome greets people who joined a group with the welcome an owner set
// for it, "{name}" in it being their first names; in code, with no model.
// Bots joining, the bot itself among them, are not greeted, nor anyone in a
// group without a welcome or with the bot muted, nor those banned as
// spammers.
func (s *Service) welcome(in incoming, banned map[int64]bool) {
	if s.opts.Rules == nil || s.isMutedLately(in.scope.ChatID) {
		return
	}
	var names []string
	for _, u := range in.msg.NewChatMembers {
		if !u.IsBot && !banned[u.ID] && strings.TrimSpace(u.FirstName) != "" {
			names = append(names, strings.TrimSpace(u.FirstName))
		}
	}
	if len(names) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	rules, err := s.opts.Rules.GroupRules(ctx, in.scope.ChatID)
	if err != nil {
		s.opts.Logger.Warn("could not read the group's welcome", "chat_id", in.scope.ChatID, "error", err)
		return
	}
	for _, r := range rules {
		if r.Kind == storage.RuleWelcome && r.ChatID == in.scope.ChatID && r.Text != "" {
			s.reply(in, strings.ReplaceAll(r.Text, welcomeName, strings.Join(names, ", ")))
			return
		}
	}
}

// welcomeName is where a welcome names the newcomers.
const welcomeName = "{name}"
