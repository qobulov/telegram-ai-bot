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
	InviteTop(ctx context.Context, chatID int64, q storage.InviteQuery) ([]storage.InviterCount, error)
	SetInviteAward(ctx context.Context, a storage.InviteAward) error
	ClearInviteAward(ctx context.Context, chatID int64) error
	InviteAwards(ctx context.Context, chatID int64) ([]storage.InviteAward, error)
	MarkInvitee(ctx context.Context, chatID, userID int64, in bool, at time.Time) error
}

// inviteTableSize is the most members /invites lists.
const inviteTableSize = 20

// invitePeriods are the commands that answer the invite table: /invites,
// whose argument names the period, this week's by default, and one for
// this week and one for this month alone, to tap.
var invitePeriods = map[string]bool{"/invites": true, "/invites_week": true, "/invites_month": true}

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

// noteJoin notes a member who came in by another, as Telegram tells a bot
// that is an admin: by that member's own invite link, or added by them, the
// change being theirs. A group may post no message that they joined, so this
// is where an addition is seen there. One let in on asking, or who came
// alone, brings no one.
func (s *Service) noteJoin(change *telegram.ChatMemberUpdated) {
	user := change.NewChatMember.User
	switch {
	case s.opts.Invites == nil, user == nil, user.IsBot:
		return
	case change.NewChatMember.Status != "member" && change.NewChatMember.Status != "restricted":
		return
	}
	switch change.OldChatMember.Status {
	case "member", "administrator", "creator", "restricted":
		return
	}
	inviter, via := change.From, "add"
	switch {
	case change.InviteLink != nil:
		inviter, via = change.InviteLink.Creator, "link"
	case change.ViaJoinRequest:
		return
	}
	if inviter == nil || inviter.IsBot || inviter.ID == user.ID {
		return
	}
	s.noteInvite(storage.Invite{ChatID: change.Chat.ID, UserID: user.ID, InviterID: inviter.ID,
		InviterName: authorLabel(inviter.FirstName), Via: via, At: s.opts.Now()})
}

// markInvitee notes a member leaving the group or being removed (in false),
// or coming back (in true), so that whoever brought them counts them only
// while they are there.
func (s *Service) markInvitee(chatID int64, user *telegram.User, in bool) {
	if s.opts.Invites == nil || user == nil || user.IsBot {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	if err := s.opts.Invites.MarkInvitee(ctx, chatID, user.ID, in, s.opts.Now()); err != nil {
		s.opts.Logger.Warn("could not mark an invitee", "chat_id", chatID, "error", err)
	}
}

func (s *Service) noteInvite(inv storage.Invite) {
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	if err := s.opts.Invites.RecordInvite(ctx, inv); err != nil {
		s.opts.Logger.Warn("could not record an invite", "chat_id", inv.ChatID, "error", err)
	}
}

// inviteTable answers /invites [hafta|oy|bugun|hammasi] in a group: who
// brought how many people in who are still there, only those who brought
// at least one; replying to a member, that member's alone. It asks no model.
func (s *Service) inviteTable(ctx context.Context, in incoming) outcome {
	if !in.group {
		return say(textInvitesGroupOnly.to(in))
	}
	arg := commandArgument(in.text)
	if command := commandName(in.text); command != "/invites" {
		arg = strings.TrimPrefix(command, "/invites_")
	}
	since, period := inviteSince(arg, s.opts.Now(), s.opts.Location)
	var inviter int64
	if reply := in.msg.ReplyToMessage; reply != nil && reply.From != nil && !reply.From.IsBot {
		inviter = reply.From.ID
	}
	top, err := s.opts.Invites.InviteTop(ctx, in.msg.Chat.ID, storage.InviteQuery{Since: since, Inviter: inviter, Limit: inviteTableSize})
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
	textInvitesNone        = localText{uz: "Hali hech kim guruhda qolgan odam qo'shmagan.", ru: "Пока никто не добавил тех, кто остался в группе.", en: "Nobody has brought in anyone who is still here yet."}
	textInvitesGone        = localText{uz: " (yana %d tasi chiqib ketgan)", ru: " (ещё %d ушли)", en: " (%d more left)"}
	textInvitesRule        = localText{uz: "Reytingda guruhda qolgan odam qo'shganlar bor: qo'lda qo'shilgan yoki shaxsiy havola orqali kirgan. Kimdir chiqib ketsa, qo'shganning hisobidan ayriladi. Hafta: /invites_week · Oy: /invites_month · Avto-e'lon (admin): /invites_auto", ru: "В рейтинге те, кто добавил оставшихся в группе: вручную или по личной ссылке. Кто ушёл, вычитается у добавившего. Неделя: /invites_week · Месяц: /invites_month · Авто (админ): /invites_auto", en: "Listed: those who brought in people still here, added by hand or by one's own invite link. One who leaves is taken off whoever brought them. Week: /invites_week · Month: /invites_month · Automatic (admins): /invites_auto"}
	textInvitesGroupOnly   = localText{uz: "👥 /invites guruhda ishlaydi.", ru: "👥 /invites работает в группе.", en: "👥 /invites works in a group."}
	textInvitesUnavailable = localText{uz: "Hisobni hozir o'qib bo'lmadi, birozdan keyin urinib ko'ring.", ru: "Сейчас не удалось прочитать счёт, попробуйте позже.", en: "Could not read the count right now; try again in a bit."}
)
