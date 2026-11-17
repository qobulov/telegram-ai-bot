package service

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/ranks"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
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
	return s.userMayModerate(ctx, in.scope.ChatID, in.msg.From, code)
}

// userMayModerate is mayModerate for user in the chat, as a button's press
// needs it.
func (s *Service) userMayModerate(ctx context.Context, chatID int64, user *telegram.User, code string) (bool, error) {
	if user == nil {
		return false, nil
	}
	if s.isOwner(user) {
		return true, nil
	}
	if s.opts.Members != nil {
		if creator, err := s.opts.Members.GroupCreator(ctx, chatID); err == nil && creator != 0 && creator == user.ID {
			return true, nil
		}
	}
	if s.opts.Ranks != nil {
		mine, err := s.opts.Ranks.RankOf(ctx, chatID, user.ID)
		if err != nil {
			return false, err
		}
		overrides, err := s.opts.Ranks.CommandRanks(ctx, chatID)
		if err != nil {
			return false, err
		}
		if mine > 0 && mine >= ranks.Need(code, overrides) {
			return true, nil
		}
	}
	if s.opts.Members == nil {
		return false, nil
	}
	return s.opts.Members.IsGroupAdmin(ctx, chatID, user.ID)
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
	text, buttons, err := s.banListPage(ctx, in.scope.ChatID)
	if err != nil {
		return say("Ro'yxatni hozir ochib bo'lmadi.")
	}
	return outcome{text: text, buttons: buttons}
}

// banListPage is the chat's ban list with an unban button for the latest.
func (s *Service) banListPage(ctx context.Context, chatID int64) (string, [][]telegram.Button, error) {
	banned, err := s.opts.BanList.BannedMembers(ctx, chatID, banListShown)
	if err != nil {
		return "", nil, err
	}
	if len(banned) == 0 {
		return "🚫 Banlar ro'yxati bo'sh.", nil, nil
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
	return strings.Join(lines, "\n"), s.unbanButtons(banned), nil
}

// unbanData starts an unban button: "ub:<user id>".
const unbanData = "ub:"

// unbanButtonsShown bounds the buttons under /banlist, the latest bans first.
const unbanButtonsShown = 10

// unbanner is a GroupModerator that can also lift a ban.
type unbanner interface {
	UnbanChatMember(ctx context.Context, chatID, userID int64) error
}

func (s *Service) unbanButtons(banned []storage.BannedMember) [][]telegram.Button {
	if _, ok := s.opts.GroupModerator.(unbanner); !ok || s.opts.Buttons == nil || s.opts.Strikes == nil {
		return nil
	}
	var rows [][]telegram.Button
	for i, b := range banned {
		if i == unbanButtonsShown {
			break
		}
		name := []rune(cmp.Or(b.Name, b.Username, strconv.FormatInt(b.UserID, 10)))
		if len(name) > 20 {
			name = append(name[:20], '…')
		}
		rows = append(rows, []telegram.Button{{Text: "🔓 " + string(name), Data: unbanData + strconv.FormatInt(b.UserID, 10)}})
	}
	return rows
}

// pressedUnban lifts the ban of the member a /banlist button names, for a
// moderator who may ban, and notes it so the list drops them.
func (s *Service) pressedUnban(q *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	userID, err := strconv.ParseInt(strings.TrimPrefix(q.Data, unbanData), 10, 64)
	lifter, ok := s.opts.GroupModerator.(unbanner)
	answer := func(text string, alert bool) { _ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, text, alert) }
	if err != nil || !ok {
		answer("Bu tugma eskirgan.", true)
		return
	}
	if allowed, err := s.userMayModerate(ctx, q.Message.Chat.ID, q.From, "ban"); err != nil || !allowed {
		answer("Bandan adminlar va moderatorlar chiqaradi.", true)
		return
	}
	if err := lifter.UnbanChatMember(ctx, q.Message.Chat.ID, userID); err != nil {
		answer("Bandan chiqarib bo'lmadi: menga a'zolarni cheklash huquqi kerak.", true)
		return
	}
	_, err = s.opts.Strikes.RecordModeration(ctx, storage.ModerationAction{
		ChatID: q.Message.Chat.ID, Action: "unban", RequesterID: q.From.ID, TargetUserID: userID,
		Reason: "/banlist", UpdateID: -time.Now().UnixMicro(),
	})
	if err != nil {
		s.opts.Logger.Warn("could not note an unban", "chat_id", q.Message.Chat.ID, "error", err)
	}
	answer("🔓 Bandan chiqarildi.", false)
	s.journal(q.Message.Chat.ID, fmt.Sprintf("🔓 %s: %d bandan chiqarildi (/banlist)", plainLabel(q.From), userID))
	if text, buttons, err := s.banListPage(ctx, q.Message.Chat.ID); err == nil {
		_ = s.opts.Buttons.EditMessageMenu(ctx, q.Message.Chat.ID, q.Message.MessageID, text, buttons)
	}
}
