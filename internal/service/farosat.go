package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
)

// Farosat is a daily game in groups, as the owners asked on 2026-11-26, in
// place of another bot's: once a day /farosat grows a member's farosat
// ("sense") by some grams, or now and then shrinks it a little, the day
// turning at midnight in the bot's time zone; /topfarosat ranks the group,
// its top 10, or its top 30 with "30". It is answered in code, the model never asked.

// FarosatStore keeps the groups' farosat.
type FarosatStore interface {
	TakeFarosat(ctx context.Context, chatID, userID int64, name string, day time.Time, gain int) (grams, place int, taken bool, err error)
	FarosatTop(ctx context.Context, chatID int64, limit int) ([]storage.FarosatRow, error)
}

const (
	// A day's gain: farosatLoss of the takes lose 1 to farosatMostLoss
	// grams, the others gain 1 to farosatMostGain.
	farosatLoss     = 0.15
	farosatMostLoss = 5
	farosatMostGain = 12
)

var (
	textFarosatUp = localText{
		uz: "🧠 %s, bugun farosatingiz **+%d gram** o'sdi!\nEndi: **%d gram** · guruhda %d-o'rin\n🕛 Keyingisi yarim tundan keyin",
		ru: "🧠 %s, сегодня ваша фаросат выросла на **+%d г**!\nТеперь: **%d г** · %d-е место в группе\n🕛 Следующая после полуночи",
		en: "🧠 %s, your farosat grew **+%d grams** today!\nNow: **%d grams** · place %d in the group\n🕛 The next one after midnight",
	}
	textFarosatDown = localText{
		uz: "🧠 %s, voy, bugun farosatingiz **%d gram** kamaydi 😅\nEndi: **%d gram** · guruhda %d-o'rin\n🕛 Keyingisi yarim tundan keyin",
		ru: "🧠 %s, ой, сегодня ваша фаросат уменьшилась на **%d г** 😅\nТеперь: **%d г** · %d-е место в группе\n🕛 Следующая после полуночи",
		en: "🧠 %s, oops, your farosat shrank **%d grams** today 😅\nNow: **%d grams** · place %d in the group\n🕛 The next one after midnight",
	}
	textFarosatTaken = localText{
		uz: "🧠 %s, bugungi farosatingizni allaqachon oldingiz.\nFarosatingiz: **%d gram** · guruhda %d-o'rin\n🕛 Keyingisi yarim tundan keyin",
		ru: "🧠 %s, сегодняшнюю фаросат вы уже получили.\nВаша фаросат: **%d г** · %d-е место в группе\n🕛 Следующая после полуночи",
		en: "🧠 %s, you already took today's farosat.\nYour farosat: **%d grams** · place %d in the group\n🕛 The next one after midnight",
	}
	textFarosatTop  = localText{uz: "🏆 **Farosat reytingi — TOP %d**", ru: "🏆 **Рейтинг фаросат — ТОП %d**", en: "🏆 **Farosat ranking — TOP %d**"}
	textFarosatNone = localText{
		uz: "🧠 Hali hech kim farosat olmagan. /farosat yozing!",
		ru: "🧠 Пока никто не получил фаросат. Напишите /farosat!",
		en: "🧠 Nobody has taken their farosat yet. Write /farosat!",
	}
	textFarosatGrams = localText{uz: "gram", ru: "г", en: "grams"}
)

// farosatGain is a day's gain, at random.
func farosatGain() int {
	if rand.Float64() < farosatLoss {
		return -1 - rand.IntN(farosatMostLoss)
	}
	return 1 + rand.IntN(farosatMostGain)
}

// farosat answers /farosat: the day's take, or the total when it is taken.
func (s *Service) farosat(ctx context.Context, in incoming) outcome {
	location := s.opts.Location
	if location == nil {
		location = time.UTC
	}
	name := cardMarks.Replace(displayName(in.msg.From))
	gain := farosatGain()
	grams, place, taken, err := s.opts.Farosat.TakeFarosat(ctx, in.scope.ChatID, in.userID, name, startOfDay(s.opts.Now(), location), gain)
	if err != nil {
		s.opts.Logger.Warn("could not take farosat", "chat_id", in.scope.ChatID, "error", err)
		return say(textError.to(in))
	}
	switch {
	case !taken:
		return say(fmt.Sprintf(textFarosatTaken.to(in), name, grams, place))
	case gain < 0:
		return say(fmt.Sprintf(textFarosatDown.to(in), name, -gain, grams, place))
	default:
		return say(fmt.Sprintf(textFarosatUp.to(in), name, gain, grams, place))
	}
}

var farosatMedals = []string{"🥇", "🥈", "🥉"}

// farosatTop answers /topfarosat with the group's ranking.
func (s *Service) farosatTop(ctx context.Context, in incoming, limit int) outcome {
	top, err := s.opts.Farosat.FarosatTop(ctx, in.scope.ChatID, limit)
	if err != nil {
		s.opts.Logger.Warn("could not read the farosat ranking", "chat_id", in.scope.ChatID, "error", err)
		return say(textError.to(in))
	}
	if len(top) == 0 {
		return say(textFarosatNone.to(in))
	}
	lines := []string{fmt.Sprintf(textFarosatTop.to(in), limit)}
	for i, r := range top {
		place := fmt.Sprintf("%d.", i+1)
		if i < len(farosatMedals) {
			place = farosatMedals[i]
		}
		lines = append(lines, fmt.Sprintf("%s %s — %d %s", place, r.Name, r.Grams, textFarosatGrams.to(in)))
	}
	return say(strings.Join(lines, "\n"))
}
