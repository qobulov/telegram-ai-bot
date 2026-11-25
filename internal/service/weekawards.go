package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// Each Monday morning Maria names last week's nominees in the groups the
// owners turned AI on for, as the owners asked on 2026-11-25: who wrote the
// most, at night, laughed the most, was muted the most and got the best new
// card. A group's admins stop it with /weekawards off.

const (
	// Least counts that earn a nomination, so that a quiet week names no
	// one for a single night message.
	weekTalkerLeast  = 20
	weekOwlLeast     = 5
	weekLaugherLeast = 3
	weekMutedLeast   = 2
	// weekLuckyLeast is the rarity a card must have, Legendary.
	weekLuckyLeast = 3
	// weekNominationsLeast are how many make a post.
	weekNominationsLeast = 2
)

// WeekAwardBook reads the groups and their weeks.
type WeekAwardBook interface {
	GroupsWithSetting(ctx context.Context, key, value string) ([]storage.GroupWithSetting, error)
	GroupSetting(ctx context.Context, chatID int64, key string) (string, error)
	WeekAwards(ctx context.Context, chatID int64, since, until time.Time, location *time.Location) (storage.WeekAwards, error)
	MarkWeekAwardsPosted(ctx context.Context, chatID int64, week string) (bool, error)
}

// WeekAwardPoster posts the nominations.
type WeekAwardPoster interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
}

// WeekAwardOptions configures PostWeekAwards.
type WeekAwardOptions struct {
	Book     WeekAwardBook
	Telegram WeekAwardPoster
	Location *time.Location
	Now      time.Time
	// NotLucky are the owners, whose cards are often given, not drawn.
	NotLucky map[int64]bool
	Logger   *slog.Logger
}

// PostWeekAwards posts last week's nominations in each group with AI on
// that has not had them and did not turn them off, and reports how many it
// posted. The week is marked before the post goes, so two runs never post
// it twice; a post that fails is logged and not tried again.
func PostWeekAwards(ctx context.Context, opts WeekAwardOptions) (int, error) {
	since, until, key := awardPeriod("week", opts.Now, opts.Location)
	if opts.Now.Sub(until) > awardLate {
		return 0, nil
	}
	groups, err := opts.Book.GroupsWithSetting(ctx, storage.SettingAI, "on")
	if err != nil {
		return 0, err
	}
	posted := 0
	for _, g := range groups {
		if off, err := opts.Book.GroupSetting(ctx, g.ChatID, storage.SettingWeekAwardsOff); err != nil || off == "on" {
			continue
		}
		w, err := opts.Book.WeekAwards(ctx, g.ChatID, since, until, opts.Location)
		if err != nil {
			opts.Logger.Warn("could not read the week's nominations", "chat_id", g.ChatID, "error", err)
			continue
		}
		text, ok := weekAwardText(w, since, until.Add(-time.Second), opts.NotLucky)
		if !ok {
			continue
		}
		if fresh, err := opts.Book.MarkWeekAwardsPosted(ctx, g.ChatID, key); err != nil || !fresh {
			if err != nil {
				opts.Logger.Warn("could not mark the week's awards posted", "chat_id", g.ChatID, "error", err)
			}
			continue
		}
		if _, err := opts.Telegram.SendMessage(ctx, telegram.Outgoing{ChatID: g.ChatID, Text: text}); err != nil {
			opts.Logger.Warn("could not post the week's awards", "chat_id", g.ChatID, "error", err)
			continue
		}
		posted++
	}
	return posted, nil
}

// weekAwardText is the post, and false when the week has too few
// nominations for one.
func weekAwardText(w storage.WeekAwards, since, last time.Time, notLucky map[int64]bool) (string, bool) {
	var lines []string
	add := func(n storage.WeekNominee, least int, captions []string) {
		if n.UserID != 0 && n.Count >= least {
			lines = append(lines, fmt.Sprintf(pickCaption(captions, n.UserID, since), mentionMember(n.UserID, n.Name), n.Count))
		}
	}
	add(w.Talker, weekTalkerLeast, talkerCaptions)
	add(w.Owl, weekOwlLeast, owlCaptions)
	add(w.Laugher, weekLaugherLeast, laugherCaptions)
	add(w.Muted, weekMutedLeast, mutedCaptions)
	if l := w.Lucky; l.UserID != 0 && l.Rarity >= weekLuckyLeast && !notLucky[l.UserID] {
		lines = append(lines, fmt.Sprintf(pickCaption(luckyCaptions, l.UserID, since), mentionMember(l.UserID, l.Name), cardLabel(l)))
	}
	if len(lines) < weekNominationsLeast {
		return "", false
	}
	dates := fmt.Sprintf("📅 %s – %s", since.Format("02.01"), last.In(since.Location()).Format("02.01"))
	return strings.Join([]string{"🏆 Haftaning nominatsiyalari", dates, "", strings.Join(lines, "\n\n"), "", textWeekAwardsEnd}, "\n"), true
}

func cardLabel(l storage.WeekLucky) string {
	label := l.Card
	if l.Anime != "" {
		label += ", " + l.Anime
	}
	if l.Rarity >= weekLuckyLeast+1 {
		return label + " (Mythic)"
	}
	return label + " (Legendary)"
}

// pickCaption picks one caption for a member and a week, the same at each
// run.
func pickCaption(captions []string, userID int64, week time.Time) string {
	h := fnv.New32a()
	fmt.Fprintf(h, "%d:%s", userID, week.Format("2006-01-02"))
	return captions[h.Sum32()%uint32(len(captions))]
}

// setWeekAwards answers /weekawards: whether the group's weekly
// nominations are posted. Alone it switches them; "on" and "off" set them.
func (s *Service) setWeekAwards(ctx context.Context, in incoming) outcome {
	if allowed, err := s.mayModerate(ctx, in, "group"); err != nil || !allowed {
		return say("⛔ Buni guruh egasi yoki 4-darajali moderator sozlaydi.")
	}
	current, err := s.opts.Extras.GroupSetting(ctx, in.scope.ChatID, storage.SettingWeekAwardsOff)
	if err != nil {
		return say(textError.to(in))
	}
	off := current != "on"
	switch strings.ToLower(strings.TrimSpace(commandArgument(in.text))) {
	case "on", "yoq", "ha":
		off = false
	case "off", "o'chir", "ochir", "yo'q":
		off = true
	}
	value := ""
	if off {
		value = "on"
	}
	if err := s.opts.Extras.SetGroupSetting(ctx, in.scope.ChatID, storage.SettingWeekAwardsOff, value, in.userID); err != nil {
		return say(textError.to(in))
	}
	if off {
		return say("🏆 Haftalik nominatsiyalar o'chirildi. Yoqish: /weekawards on")
	}
	return say("🏆 Haftalik nominatsiyalar yoqildi: har dushanba ertalab o'tgan hafta gapdoni, tungi boyqushi, kulgichisi, mute chempioni va omadlisini e'lon qilaman. O'chirish: /weekawards off")
}

const textWeekAwardsEnd = "Tabriklaymiz! Keyingi hafta kim bo'larkin? 😏"

var (
	talkerCaptions = []string{
		"🗣 Haftaning gapdoni — %s: %d ta xabar. Klaviaturasi dam olish so'rayapti 😂",
		"🗣 Haftaning gapdoni — %s: %d ta xabar. Guruh radiosi deyishsa bo'ladi 📻",
		"🗣 Haftaning gapdoni — %s: %d ta xabar. Barmoqlarga rahmat, ular charchagan 😅",
	}
	owlCaptions = []string{
		"🦉 Tungi boyqush — %s: tunda %d ta xabar. Uyqu degan narsani eshitganmisiz? 😴",
		"🦉 Tungi boyqush — %s: 00:00 dan 05:00 gacha %d ta xabar. Guruhni tunda kim qo'riqlashini bilib oldik 🌙",
		"🦉 Tungi boyqush — %s: tunda %d ta xabar. Ertalab dars yoki ish bormidi o'zi? ☕",
	}
	laugherCaptions = []string{
		"😂 Kulgichi — %s: %d marta kuldi. Guruhning eng minnatdor tomoshabini 👏",
		"😂 Kulgichi — %s: %d marta 😂. Hazilchilarning eng sodiq muxlisi 🎭",
		"😂 Kulgichi — %s: %d marta kuldi. Kayfiyati doim zo'r, havas qilsa arziydi ✨",
	}
	mutedCaptions = []string{
		"🔇 Mute chempioni — %s: %d marta. Adminlar bilan do'stlik mustahkam 😅",
		"🔇 Mute chempioni — %s: %d marta jim qoldi. Sukut ham oltin, deyishadi 🤐",
		"🔇 Mute chempioni — %s: %d marta. Keyingi hafta rekordni yangilamaylik 🙏",
	}
	luckyCaptions = []string{
		"🎴 Omadli — %s: %s tushirdi. Omadni bizga ham ulashing! 🍀",
		"🎴 Omadli — %s: %s ochdi. Kartalar uni yaxshi ko'radi 💫",
		"🎴 Omadli — %s: %s qo'lga kiritdi. Qolganlar havas bilan qarab turibdi 👀",
	}
)
