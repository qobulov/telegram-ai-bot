package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/reminders"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// A group's admins may have the bot announce who brought the most people
// in: /invites_auto week 3 posts last week's top three each Monday,
// /invites_auto half 5 the last half month's top five on the 1st and the
// 16th, /invites_auto month 3 last month's on the 1st. The post is pinned
// and tags the group's owner, so that whoever rewards the winners sees it.
// The daily cron posts them, at 08:00 in Tashkent.

const (
	// awardTop is how many winners a post names when the command says not.
	awardTop = 3
	// awardLate is how long after a period ends its post may still go, so
	// that a day the cron missed does not lose it while one turned on much
	// later posts nothing old.
	awardLate = 3 * 24 * time.Hour
)

// awardPeriods are the periods an admin may name, in any of the bot's
// languages, by the name stored.
var awardPeriods = map[string]string{
	"week": "week", "hafta": "week", "неделя": "week",
	"half": "half", "yarim": "half", "полмесяца": "half",
	"month": "month", "oy": "month", "месяц": "month",
}

// InviteAwardBook keeps the groups' announcements and reads their tables.
type InviteAwardBook interface {
	InviteAwards(ctx context.Context, chatID int64) ([]storage.InviteAward, error)
	MarkInviteAwardPosted(ctx context.Context, chatID int64, period string) (bool, error)
	InviteTop(ctx context.Context, chatID int64, q storage.InviteQuery) ([]storage.InviterCount, error)
}

// AwardPoster posts and pins an announcement, and finds the group's owner.
type AwardPoster interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
	PinChatMessage(ctx context.Context, chatID, messageID int64) error
	GetChatAdministrators(ctx context.Context, chatID int64) ([]telegram.ChatMember, error)
}

// InviteAwardOptions configures PostInviteAwards.
type InviteAwardOptions struct {
	Book     InviteAwardBook
	Telegram AwardPoster
	Location *time.Location
	Now      time.Time
	Logger   *slog.Logger
}

// inviteAwardCommand answers a group admin's /invites_auto: with a period
// and how many, it turns the announcement on; with "off", off; with
// nothing, it says how it stands and how to set it.
func (s *Service) inviteAwardCommand(ctx context.Context, in incoming) outcome {
	if !in.group {
		return say(textAwardGroupOnly.to(in))
	}
	if refusal := s.refuseUnlessManager(ctx, in); refusal != "" {
		return say(refusal)
	}
	chatID := in.msg.Chat.ID
	args := strings.Fields(strings.ToLower(commandArgument(in.text)))
	if len(args) == 0 {
		return say(s.awardStatus(ctx, in))
	}
	if offWords[args[0]] {
		if err := s.opts.Invites.ClearInviteAward(ctx, chatID); err != nil {
			s.opts.Logger.Error("could not turn an invite award off", "chat_id", chatID, "error", err)
			return say(textAwardUnavailable.to(in))
		}
		return say(textAwardOff.to(in))
	}
	period, top, ok := awardArgs(args)
	if !ok {
		return say(textAwardUsage.to(in))
	}
	award := storage.InviteAward{ChatID: chatID, Period: period, Top: top, Language: appLanguage(in.msg.From), SetBy: in.userID}
	if err := s.opts.Invites.SetInviteAward(ctx, award); err != nil {
		s.opts.Logger.Error("could not turn an invite award on", "chat_id", chatID, "error", err)
		return say(textAwardUnavailable.to(in))
	}
	return say(fmt.Sprintf(textAwardOn.to(in), top, awardWhen[period].to(in)))
}

// awardArgs reads "week 3": the period and how many, three when not said,
// at most inviteTableSize.
func awardArgs(args []string) (period string, top int, ok bool) {
	period, ok = awardPeriods[args[0]]
	if !ok || len(args) > 2 {
		return "", 0, false
	}
	top = awardTop
	if len(args) == 2 {
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 1 || n > inviteTableSize {
			return "", 0, false
		}
		top = n
	}
	return period, top, true
}

// awardStatus says whether the group's announcement is on, and how to set it.
func (s *Service) awardStatus(ctx context.Context, in incoming) string {
	awards, err := s.opts.Invites.InviteAwards(ctx, in.msg.Chat.ID)
	if err != nil {
		s.opts.Logger.Error("could not read an invite award", "chat_id", in.msg.Chat.ID, "error", err)
		return textInvitesUnavailable.to(in)
	}
	if len(awards) == 0 {
		return textAwardNone.to(in) + "\n\n" + textAwardUsage.to(in)
	}
	return fmt.Sprintf(textAwardOn.to(in), awards[0].Top, awardWhen[awards[0].Period].to(in)) + "\n\n" + textAwardUsage.to(in)
}

// PostInviteAwards posts each group's announcement whose period has just
// ended and is not posted yet, and reports how many it posted. A period is
// noted as posted before its post goes, so that two runs never post it
// twice; one whose post fails is logged and not tried again.
func PostInviteAwards(ctx context.Context, opts InviteAwardOptions) (int, error) {
	awards, err := opts.Book.InviteAwards(ctx, 0)
	if err != nil {
		return 0, err
	}
	posted := 0
	for _, a := range awards {
		since, until, key := awardPeriod(a.Period, opts.Now, opts.Location)
		if key == a.Posted || !until.After(a.SetAt) || opts.Now.Sub(until) > awardLate {
			continue
		}
		if fresh, err := opts.Book.MarkInviteAwardPosted(ctx, a.ChatID, key); err != nil || !fresh {
			if err != nil {
				opts.Logger.Warn("could not mark an invite award posted", "chat_id", a.ChatID, "error", err)
			}
			continue
		}
		sent, err := postAward(ctx, opts, a, since, until)
		if err != nil {
			opts.Logger.Warn("could not post an invite award", "chat_id", a.ChatID, "error", err)
			continue
		}
		if sent {
			posted++
		}
	}
	return posted, nil
}

// postAward posts and pins one group's winners from since until until,
// tagging its owner; a period nobody brought anyone in posts nothing.
func postAward(ctx context.Context, opts InviteAwardOptions, a storage.InviteAward, since, until time.Time) (bool, error) {
	top, err := opts.Book.InviteTop(ctx, a.ChatID, storage.InviteQuery{Since: since, Until: until, Limit: a.Top})
	if err != nil || len(top) == 0 {
		return false, err
	}
	owner := groupOwner(ctx, opts, a.ChatID)
	text := awardText(a, since, until.Add(-time.Second), top, owner)
	id, err := opts.Telegram.SendMessage(ctx, telegram.Outgoing{ChatID: a.ChatID, Text: text})
	if err != nil {
		return false, err
	}
	if err := opts.Telegram.PinChatMessage(ctx, a.ChatID, id); err != nil {
		opts.Logger.Info("could not pin an invite award", "chat_id", a.ChatID, "error", err)
	}
	return true, nil
}

// groupOwner is the group's creator, nil when Telegram does not say.
func groupOwner(ctx context.Context, opts InviteAwardOptions, chatID int64) *telegram.User {
	admins, err := opts.Telegram.GetChatAdministrators(ctx, chatID)
	if err != nil {
		opts.Logger.Info("could not find a group's owner", "chat_id", chatID, "error", err)
		return nil
	}
	for _, admin := range admins {
		if admin.Status == "creator" && admin.User != nil && !admin.User.IsBot {
			return admin.User
		}
	}
	return nil
}

// awardText is the post: the period's title and dates, the winners with
// medals and how many they brought who stayed, and the owner tagged.
func awardText(a storage.InviteAward, since, last time.Time, top []storage.InviterCount, owner *telegram.User) string {
	lang := a.Language
	lines := []string{awardTitles[a.Period].inLanguage(lang),
		fmt.Sprintf("📅 %s – %s", since.Format("02.01"), last.In(since.Location()).Format("02.01")), ""}
	for i, c := range top {
		place := strconv.Itoa(i+1) + "."
		if i < len(awardMedals) {
			place = awardMedals[i]
		}
		lines = append(lines, fmt.Sprintf(textAwardLine.inLanguage(lang), place, reminders.Mention(c.UserID, c.Name), c.Stayed))
	}
	if owner != nil {
		lines = append(lines, "", fmt.Sprintf(textAwardOwner.inLanguage(lang), reminders.Mention(owner.ID, owner.FirstName)))
	}
	return strings.Join(lines, "\n")
}

var awardMedals = []string{"🥇", "🥈", "🥉"}

// awardPeriod is the last period of its kind to have ended by now, in
// location: last week from Monday, the last half month (the 1st to the
// 15th, or the 16th to the month's end) or last month; and its key.
func awardPeriod(period string, now time.Time, location *time.Location) (since, until time.Time, key string) {
	today := startOfDay(now, location)
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)
	switch period {
	case "week":
		until = today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
		since = until.AddDate(0, 0, -7)
	case "half":
		if today.Day() >= 16 {
			since, until = monthStart, monthStart.AddDate(0, 0, 15)
		} else {
			since, until = monthStart.AddDate(0, -1, 15), monthStart
		}
	default:
		since, until = monthStart.AddDate(0, -1, 0), monthStart
	}
	return since, until, period + ":" + since.Format("2006-01-02")
}

var (
	awardTitles = map[string]localText{
		"week":  {uz: "🏆 Haftaning eng ko'p odam qo'shganlari", ru: "🏆 Кто добавил больше всех за неделю", en: "🏆 Who brought the most in this week"},
		"half":  {uz: "🏆 Yarim oyning eng ko'p odam qo'shganlari", ru: "🏆 Кто добавил больше всех за полмесяца", en: "🏆 Who brought the most in this half month"},
		"month": {uz: "🏆 Oyning eng ko'p odam qo'shganlari", ru: "🏆 Кто добавил больше всех за месяц", en: "🏆 Who brought the most in this month"},
	}
	awardWhen = map[string]localText{
		"week":  {uz: "har dushanba ertalab, o'tgan hafta bo'yicha", ru: "каждый понедельник утром, за прошлую неделю", en: "every Monday morning, for the week before"},
		"half":  {uz: "har oyning 1 va 16-sanasi ertalab, o'tgan yarim oy bo'yicha", ru: "1-го и 16-го числа утром, за прошедшие полмесяца", en: "on the 1st and the 16th in the morning, for the half month before"},
		"month": {uz: "har oyning 1-sanasi ertalab, o'tgan oy bo'yicha", ru: "1-го числа утром, за прошлый месяц", en: "on the 1st in the morning, for the month before"},
	}
	textAwardLine        = localText{uz: "%s %s — %d ta", ru: "%s %s — %d", en: "%s %s — %d"}
	textAwardOwner       = localText{uz: "👑 Guruh egasi: %s", ru: "👑 Владелец группы: %s", en: "👑 Group owner: %s"}
	textAwardOn          = localText{uz: "✅ Top %d odam qo'shganni %s e'lon qilaman, xabarni pin qilaman va guruh egasini belgilayman. Pin uchun menga «Xabarlarni qadash» huquqi kerak.", ru: "✅ Буду объявлять топ-%d добавивших %s, закреплять сообщение и отмечать владельца группы. Для закрепления мне нужно право «Закреплять сообщения».", en: "✅ I will post the top %d who brought people in %s, pin it and tag the group's owner. To pin I need the «Pin messages» right."}
	textAwardOff         = localText{uz: "🏆 Avto-reyting o'chirildi.", ru: "🏆 Авто-рейтинг выключен.", en: "🏆 The automatic ranking is off."}
	textAwardNone        = localText{uz: "🏆 Avto-reyting o'chiq.", ru: "🏆 Авто-рейтинг выключен.", en: "🏆 The automatic ranking is off."}
	textAwardUsage       = localText{uz: "Yoqish: /invites_auto week 3 — har dushanba, o'tgan hafta top 3\n/invites_auto half 5 — 1 va 16-sanada, o'tgan yarim oy top 5\n/invites_auto month 3 — har oy\nO'chirish: /invites_auto off", ru: "Включить: /invites_auto week 3 — каждый понедельник, топ-3 прошлой недели\n/invites_auto half 5 — 1-го и 16-го, топ-5 прошедших полмесяца\n/invites_auto month 3 — каждый месяц\nВыключить: /invites_auto off", en: "Turn on: /invites_auto week 3 — every Monday, last week's top 3\n/invites_auto half 5 — on the 1st and 16th, the last half month's top 5\n/invites_auto month 3 — every month\nTurn off: /invites_auto off"}
	textAwardGroupOnly   = localText{uz: "🏆 /invites_auto guruhda ishlaydi.", ru: "🏆 /invites_auto работает в группе.", en: "🏆 /invites_auto works in a group."}
	textAwardUnavailable = localText{uz: "Hozir saqlab bo'lmadi, birozdan keyin urinib ko'ring.", ru: "Сейчас не удалось сохранить, попробуйте позже.", en: "Could not save it right now; try again in a bit."}
)
