package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// InviteBook keeps who brought whom into each group.
type InviteBook interface {
	RecordInvite(ctx context.Context, inv storage.Invite) error
	InviteTop(ctx context.Context, chatID int64, since time.Time, inviter int64, limit int) ([]storage.InviterCount, error)
}

// inviteTableSize is the most members /qoshganlar lists.
const inviteTableSize = 20

// noteAdded notes the members a message says joined, when another added
// them: the message is from the one who added them. One who joined alone, or
// a bot, brings no one.
func (s *Service) noteAdded(msg *telegram.Message) {
	if s.opts.Invites == nil || msg.From == nil || msg.From.IsBot {
		return
	}
	for i := range msg.NewChatMembers {
		member := &msg.NewChatMembers[i]
		if member.IsBot || member.ID == msg.From.ID {
			continue
		}
		s.noteInvite(storage.Invite{ChatID: msg.Chat.ID, UserID: member.ID, InviterID: msg.From.ID,
			InviterName: authorLabel(msg.From.FirstName), Via: "add", At: time.Unix(msg.Date, 0)})
	}
}

// noteLinkJoin notes a member who came in by another member's own invite
// link, which Telegram tells a bot that is an admin.
func (s *Service) noteLinkJoin(change *telegram.ChatMemberUpdated) {
	link, user := change.InviteLink, change.NewChatMember.User
	switch {
	case s.opts.Invites == nil, link == nil, link.Creator == nil, user == nil, user.IsBot, link.Creator.IsBot, link.Creator.ID == user.ID:
		return
	case change.NewChatMember.Status != "member" && change.NewChatMember.Status != "restricted":
		return
	}
	switch change.OldChatMember.Status {
	case "member", "administrator", "creator", "restricted":
		return
	}
	s.noteInvite(storage.Invite{ChatID: change.Chat.ID, UserID: user.ID, InviterID: link.Creator.ID,
		InviterName: authorLabel(link.Creator.FirstName), Via: "link", At: s.opts.Now()})
}

func (s *Service) noteInvite(inv storage.Invite) {
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	if err := s.opts.Invites.RecordInvite(ctx, inv); err != nil {
		s.opts.Logger.Warn("could not record an invite", "chat_id", inv.ChatID, "error", err)
	}
}

// inviteTable answers /qoshganlar [hafta|oy|bugun|hammasi] in a group: who
// brought how many people in, those still there first; replying to a member,
// that member's alone. It asks no model.
func (s *Service) inviteTable(ctx context.Context, in incoming) outcome {
	if !in.group {
		return say(textInvitesGroupOnly.to(in))
	}
	since, period := inviteSince(commandArgument(in.text), s.opts.Now(), s.opts.Location)
	var inviter int64
	if reply := in.msg.ReplyToMessage; reply != nil && reply.From != nil && !reply.From.IsBot {
		inviter = reply.From.ID
	}
	top, err := s.opts.Invites.InviteTop(ctx, in.msg.Chat.ID, since, inviter, inviteTableSize)
	if err != nil {
		s.opts.Logger.Error("could not read the invite table", "chat_id", in.msg.Chat.ID, "error", err)
		return say(textInvitesUnavailable.to(in))
	}
	lines := []string{fmt.Sprintf(textInvitesTitle.to(in), periodNames[period].to(in)), ""}
	if len(top) == 0 {
		lines = append(lines, textInvitesNone.to(in))
	}
	for i, c := range top {
		line := fmt.Sprintf("%d. %s — %d", i+1, cardMarks.Replace(c.Name), c.Stayed)
		if gone := c.Added - c.Stayed; gone > 0 {
			line += fmt.Sprintf(textInvitesGone.to(in), gone)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", textInvitesRule.to(in))
	return say(strings.Join(lines, "\n"))
}

// inviteSince is when the period an argument names starts, in location: this
// week from Monday, by default; today; this month; or all time.
func inviteSince(arg string, now time.Time, location *time.Location) (time.Time, string) {
	today := startOfDay(now, location)
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "bugun", "сегодня", "today":
		return today, "today"
	case "oy", "месяц", "month":
		local := today.In(location)
		return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location), "month"
	case "hammasi", "все", "всё", "all":
		return time.Time{}, "all"
	}
	back := (int(today.In(location).Weekday()) + 6) % 7 // days since Monday
	return today.AddDate(0, 0, -back), "week"
}

var (
	periodNames = map[string]localText{
		"today": {uz: "bugun", ru: "сегодня", en: "today"},
		"week":  {uz: "bu hafta (dushanbadan)", ru: "на этой неделе (с понедельника)", en: "this week (since Monday)"},
		"month": {uz: "bu oy", ru: "в этом месяце", en: "this month"},
		"all":   {uz: "hamma vaqt", ru: "за всё время", en: "all time"},
	}
	textInvitesTitle       = localText{uz: "👥 Kim nechta odam qo'shdi · %s", ru: "👥 Кто сколько людей добавил · %s", en: "👥 Who brought how many in · %s"}
	textInvitesNone        = localText{uz: "Hali hech kim odam qo'shmagan.", ru: "Пока никто никого не добавил.", en: "Nobody has brought anyone in yet."}
	textInvitesGone        = localText{uz: " (yana %d tasi chiqib ketgan)", ru: " (ещё %d ушли)", en: " (%d more left)"}
	textInvitesRule        = localText{uz: "Hisobda: qo'lda qo'shilganlar va shaxsiy havola orqali kirganlar, guruhda qolganlari. Davr: /qoshganlar bugun · oy · hammasi", ru: "Считаются добавленные вручную и пришедшие по личной ссылке, кто остался в группе. Период: /qoshganlar сегодня · месяц · все", en: "Counted: those added by hand or come by one's own invite link who are still here. Period: /qoshganlar today · month · all"}
	textInvitesGroupOnly   = localText{uz: "👥 /qoshganlar guruhda ishlaydi.", ru: "👥 /qoshganlar работает в группе.", en: "👥 /qoshganlar works in a group."}
	textInvitesUnavailable = localText{uz: "Hisobni hozir o'qib bo'lmadi, birozdan keyin urinib ko'ring.", ru: "Сейчас не удалось прочитать счёт, попробуйте позже.", en: "Could not read the count right now; try again in a bit."}
)
