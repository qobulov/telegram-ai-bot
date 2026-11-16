package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"telegram-ai-bot/internal/ranks"
	"telegram-ai-bot/internal/storage"
)

// BanList lists a group's members the bot banned, for /banlist.
type BanList interface {
	BannedMembers(ctx context.Context, chatID int64, limit int) ([]storage.BannedMember, error)
}

// banListShown is how many bans /banlist shows.
const banListShown = 30

// groupOrder is a group message asking for something done to the whole
// group: "/banlist", "/amnesty", "/kickinactive 30" ("ha" confirming it).
func (s *Service) groupOrder(in incoming) (fields map[string]any, ok bool) {
	if !in.group {
		return nil, false
	}
	words := strings.Fields(strings.ToLower(commandArgument(in.text)))
	switch commandName(in.text) {
	case "/banlist":
		return map[string]any{"action": "banlist"}, s.opts.BanList != nil
	case "/amnesty":
		return map[string]any{"action": "amnesty"}, s.opts.Tools != nil
	case "/kickinactive":
		days, force := 0, false
		for _, w := range words {
			if n, err := strconv.Atoi(w); err == nil {
				days = n
			} else if w == "ha" || w == "да" || w == "yes" || w == "confirm" {
				force = true
			}
		}
		return map[string]any{"action": "kick_inactive", "days": days, "force": force}, s.opts.Tools != nil
	}
	written, _ := in.msg.Content()
	switch strings.Join(s.orderWords(written), " ") {
	case "banlist", "банлист", "banlar", "banlar ro'yxati":
		return map[string]any{"action": "banlist"}, s.opts.BanList != nil
	case "amnistiya", "амнистия", "amnesty":
		return map[string]any{"action": "amnesty"}, s.opts.Tools != nil
	}
	return nil, false
}

// isGroupOrder reports a message asking for something done to the group.
func (s *Service) isGroupOrder(in incoming) bool {
	_, ok := s.groupOrder(in)
	return ok
}

// groupCommand does a group-wide order: the ban list here, the rest through
// the moderate tool, which checks who may ask.
func (s *Service) groupCommand(ctx context.Context, in incoming) outcome {
	fields, _ := s.groupOrder(in)
	if fields["action"] == "banlist" {
		return s.banList(ctx, in)
	}
	return s.runModerate(ctx, in, fields)
}

// mayModerate reports whether the person asking may see what code opens:
// an owner or the creator, a Telegram admin, or a moderator whose rank
// opens it.
func (s *Service) mayModerate(ctx context.Context, in incoming, code string) (bool, error) {
	if s.opts.Ranks != nil {
		mine, err := s.askerRank(ctx, in)
		if err != nil {
			return false, err
		}
		overrides, err := s.opts.Ranks.CommandRanks(ctx, in.scope.ChatID)
		if err != nil {
			return false, err
		}
		if mine >= ranks.Above || (mine > 0 && mine >= ranks.Need(code, overrides)) {
			return true, nil
		}
	} else if s.isOwner(in.msg.From) {
		return true, nil
	}
	if s.opts.Members == nil {
		return false, nil
	}
	return s.opts.Members.IsGroupAdmin(ctx, in.scope.ChatID, in.userID)
}

// banList lists who the bot banned here and why, to its admins and
// moderators.
func (s *Service) banList(ctx context.Context, in incoming) outcome {
	allowed, err := s.mayModerate(ctx, in, "banlist")
	if err != nil {
		return say(textError.to(in))
	}
	if !allowed {
		return say("⛔ Banlar ro'yxatini adminlar va moderatorlar ko'ra oladi.")
	}
	banned, err := s.opts.BanList.BannedMembers(ctx, in.scope.ChatID, banListShown)
	if err != nil {
		return say("Ro'yxatni hozir ochib bo'lmadi.")
	}
	if len(banned) == 0 {
		return say("🚫 Banlar ro'yxati bo'sh.")
	}
	lines := []string{fmt.Sprintf("🚫 Banlar ro'yxati (%d)", len(banned))}
	for _, b := range banned {
		line := "• " + memberLabel(b.Name, b.Username, b.UserID)
		if b.Reason != "" {
			line += " — " + cardMarks.Replace(b.Reason)
		}
		line += fmt.Sprintf(" · %s, %s", memberLabel(b.ByName, "", b.By), b.At.In(s.location()).Format("02.01"))
		lines = append(lines, line)
	}
	return say(strings.Join(lines, "\n"))
}
