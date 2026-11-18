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
	case s.linkOwner(change.InviteLink) != 0:
		// A member's own link, made by the bot: the member brought them.
		inviter, via = &telegram.User{ID: s.linkOwner(change.InviteLink), FirstName: linkMemberName(change.InviteLink.Name)}, "link"
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
// at least one, with buttons that switch the period in place and show the
// presser their own count; replying to a member, that member's alone. It
// asks no model. /invites_week and /invites_month, typed, are its week and
// month.
func (s *Service) inviteTable(ctx context.Context, in incoming) outcome {
	if !in.group {
		return say(textInvitesGroupOnly.to(in))
	}
	arg := commandArgument(in.text)
	if command := commandName(in.text); command != "/invites" {
		arg = strings.TrimPrefix(command, "/invites_")
	}
	_, period := inviteSince(arg, s.opts.Now(), s.opts.Location)
	var inviter int64
	if reply := in.msg.ReplyToMessage; reply != nil && reply.From != nil && !reply.From.IsBot {
		inviter = reply.From.ID
	}
	language := languageOf(in)
	text, err := s.inviteTableText(ctx, in.msg.Chat.ID, period, inviter, language)
	if err != nil {
		return say(textInvitesUnavailable.to(in))
	}
	if inviter != 0 || s.opts.Buttons == nil {
		return say(text)
	}
	return outcome{text: text, buttons: inviteButtons(period, language)}
}

// inviteTableText is the invite table of a group for a period, of one
// inviter's alone when inviter is not 0.
func (s *Service) inviteTableText(ctx context.Context, chatID int64, period string, inviter int64, language string) (string, error) {
	since, _ := inviteSince(period, s.opts.Now(), s.opts.Location)
	top, err := s.opts.Invites.InviteTop(ctx, chatID, storage.InviteQuery{Since: since, Inviter: inviter, Limit: inviteTableSize})
	if err != nil {
		s.opts.Logger.Error("could not read the invite table", "chat_id", chatID, "error", err)
		return "", err
	}
	lines := []string{fmt.Sprintf(textInvitesTitle.inLanguage(language), periodNames[period].inLanguage(language)), ""}
	if len(top) == 0 {
		lines = append(lines, textInvitesNone.inLanguage(language))
	}
	for i, c := range top {
		line := fmt.Sprintf("%d. %s — %d", i+1, cardMarks.Replace(c.Name), c.Stayed)
		if gone := c.Added - c.Stayed; gone > 0 {
			line += fmt.Sprintf(textInvitesGone.inLanguage(language), gone)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", textInvitesRule.inLanguage(language))
	return strings.Join(lines, "\n"), nil
}

// inviteData starts the data of the invite table's buttons: "inv:<period>"
// shows that period, "inv:me" the presser's own count.
const inviteData = "inv:"

// invitePeriodOrder are the periods the table's buttons offer, in order.
var invitePeriodOrder = []string{"today", "week", "month", "all"}

var inviteButtonNames = map[string]localText{
	"today": {uz: "Bugun", ru: "Сегодня", en: "Today"},
	"week":  {uz: "Hafta", ru: "Неделя", en: "Week"},
	"month": {uz: "Oy", ru: "Месяц", en: "Month"},
	"all":   {uz: "Hammasi", ru: "Всё", en: "All"},
	"me":    {uz: "📨 Meniki", ru: "📨 Мои", en: "📨 Mine"},
}

// inviteButtons are the periods, the one shown ticked, and the presser's
// own count under them.
func inviteButtons(shown, language string) [][]telegram.Button {
	var periods []telegram.Button
	for _, period := range invitePeriodOrder {
		name := inviteButtonNames[period].inLanguage(language)
		if period == shown {
			name = "✅ " + name
		}
		periods = append(periods, telegram.Button{Text: name, Data: inviteData + period})
	}
	return [][]telegram.Button{periods, {{Text: inviteButtonNames["me"].inLanguage(language), Data: inviteData + "me"}}}
}

// pressedInvites switches the invite table to the period pressed, or tells
// the presser alone, in a box, how many they brought.
func (s *Service) pressedInvites(q *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	language := appLanguage(q.From)
	choice := strings.TrimPrefix(q.Data, inviteData)
	chatID := q.Message.Chat.ID
	if choice == "me" {
		text, err := s.ownInvites(ctx, chatID, q.From)
		if err != nil {
			text = textInvitesUnavailable.inLanguage(language)
		}
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, text, true)
		return
	}
	if _, known := periodNames[choice]; !known {
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false)
		return
	}
	text, err := s.inviteTableText(ctx, chatID, choice, 0, language)
	if err != nil {
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, textInvitesUnavailable.inLanguage(language), true)
		return
	}
	_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false)
	if err := s.opts.Buttons.EditMessageMenu(ctx, chatID, q.Message.MessageID, text, inviteButtons(choice, language)); err != nil {
		s.opts.Logger.Warn("could not switch the invite table", "chat_id", chatID, "error", err)
	}
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
	textInvitesRule        = localText{uz: "Reytingda guruhda qolgan odam qo'shganlar bor: qo'lda qo'shilgan yoki shaxsiy havola (/mylink) orqali kirgan. Kimdir chiqib ketsa, qo'shganning hisobidan ayriladi. Avto-e'lon (admin): /invites_auto", ru: "В рейтинге те, кто добавил оставшихся в группе: вручную или по личной ссылке (/mylink). Кто ушёл, вычитается у добавившего. Авто (админ): /invites_auto", en: "Listed: those who brought in people still here, added by hand or by one's own invite link (/mylink). One who leaves is taken off whoever brought them. Automatic (admins): /invites_auto"}
	textInvitesGroupOnly   = localText{uz: "👥 /invites guruhda ishlaydi.", ru: "👥 /invites работает в группе.", en: "👥 /invites works in a group."}
	textInvitesUnavailable = localText{uz: "Hisobni hozir o'qib bo'lmadi, birozdan keyin urinib ko'ring.", ru: "Сейчас не удалось прочитать счёт, попробуйте позже.", en: "Could not read the count right now; try again in a bit."}
)

// linkMemberName is the member's name a link the bot made for them carries
// after their ID.
func linkMemberName(linkName string) string {
	_, name, _ := strings.Cut(linkName, " ")
	return name
}

// myInvites answers /myinvites: how many the asker brought into the group,
// all time and this week, and how many of them stayed. The table's
// "Meniki" button says the same.
func (s *Service) myInvites(ctx context.Context, in incoming) outcome {
	if !in.group {
		return say(textInvitesGroupOnly.to(in))
	}
	text, err := s.ownInvites(ctx, in.msg.Chat.ID, in.msg.From)
	if err != nil {
		return say(textInvitesUnavailable.to(in))
	}
	if s.opts.MemberLinks != nil {
		text += "\n\nShaxsiy havolangiz: /mylink — u orqali kirganlar ham sizga yoziladi."
	}
	return say(text)
}

// ownInvites is how many user brought into the group, all time and this
// week, and how many of them stayed, short enough for a button's box.
func (s *Service) ownInvites(ctx context.Context, chatID int64, user *telegram.User) (string, error) {
	weekStart, _ := inviteSince("", s.opts.Now(), s.opts.Location)
	count := func(since time.Time) (storage.InviterCount, error) {
		top, err := s.opts.Invites.InviteTop(ctx, chatID, storage.InviteQuery{Since: since, Inviter: user.ID, Limit: inviteTableSize})
		for _, c := range top {
			if c.UserID == user.ID {
				return c, err
			}
		}
		return storage.InviterCount{}, err
	}
	all, err := count(time.Time{})
	if err != nil {
		return "", err
	}
	week, err := count(weekStart)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		"📨 " + cardMarks.Replace(doozName(user)) + " olib kelganlar",
		fmt.Sprintf("Jami: %d, guruhda qolgan: %d", all.Added, all.Stayed),
		fmt.Sprintf("Shu hafta: %d, qolgan: %d", week.Added, week.Stayed),
	}, "\n"), nil
}
