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
	textFarosatHead  = localText{uz: "🧠 **FAROSAT** · %s", ru: "🧠 **ФАРОСАТ** · %s", en: "🧠 **FAROSAT** · %s"}
	textFarosatUp    = localText{uz: "📈 Bugun: **+%d gram**", ru: "📈 Сегодня: **+%d г**", en: "📈 Today: **+%d grams**"}
	textFarosatDown  = localText{uz: "📉 Bugun: **−%d gram**", ru: "📉 Сегодня: **−%d г**", en: "📉 Today: **−%d grams**"}
	textFarosatTaken = localText{
		uz: "✅ Bugungi farosatingizni allaqachon oldingiz",
		ru: "✅ Сегодняшнюю фаросат вы уже получили",
		en: "✅ You already took today's farosat",
	}
	textFarosatTotal = localText{uz: "⚖️ Jami: **%d gram**", ru: "⚖️ Всего: **%d г**", en: "⚖️ Total: **%d grams**"}
	textFarosatPlace = localText{uz: "🏆 Guruhda: **%d-o'rin**", ru: "🏆 В группе: **%d-е место**", en: "🏆 In the group: **place %d**"}
	textFarosatNext  = localText{uz: "🕛 Keyingisi yarim tundan keyin", ru: "🕛 Следующая после полуночи", en: "🕛 The next one after midnight"}
	// A day's take says how it went: a big gain, a small one, a loss.
	textFarosatBig   = localText{uz: "🔥 Bugun aql charaqlayapti!", ru: "🔥 Сегодня ум так и сияет!", en: "🔥 Your mind is sparkling today!"}
	textFarosatSmall = localText{uz: "🌱 Oz-ozdan, lekin o'syapti", ru: "🌱 Понемногу, но растёт", en: "🌱 Little by little, it grows"}
	textFarosatLost  = localText{uz: "😅 Voy, bugun biroz kamaydi", ru: "😅 Ой, сегодня немного убавилось", en: "😅 Oops, a little less today"}
	textFarosatTop   = localText{uz: "🏆 **FAROSAT REYTINGI · TOP %d**", ru: "🏆 **РЕЙТИНГ ФАРОСАТ · ТОП %d**", en: "🏆 **FAROSAT RANKING · TOP %d**"}
	textFarosatTake  = localText{uz: "🧠 /farosat — bugungi farosatingizni oling", ru: "🧠 /farosat — получите фаросат на сегодня", en: "🧠 /farosat — take today's farosat"}
	textFarosatNone  = localText{
		uz: "🧠 Hali hech kim farosat olmagan. /farosat yozing!",
		ru: "🧠 Пока никто не получил фаросат. Напишите /farosat!",
		en: "🧠 Nobody has taken their farosat yet. Write /farosat!",
	}
	textFarosatGrams = localText{uz: "gram", ru: "г", en: "grams"}
)

// farosatRule sets a card's head off from its body and its foot.
const farosatRule = "━━━━━━━━━━━━━━"

// farosatBigGain is a gain told as a big one.
const farosatBigGain = 9

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
	return say(farosatCard(in, name, gain, taken, grams, place))
}

// farosatCard is a take's answer: whose it is, the day's change, the total,
// the place, and when the next one comes.
func farosatCard(in incoming, name string, gain int, taken bool, grams, place int) string {
	var today, mood string
	switch {
	case !taken:
		today = textFarosatTaken.to(in)
	case gain < 0:
		today, mood = fmt.Sprintf(textFarosatDown.to(in), -gain), textFarosatLost.to(in)
	case gain >= farosatBigGain:
		today, mood = fmt.Sprintf(textFarosatUp.to(in), gain), textFarosatBig.to(in)
	default:
		today, mood = fmt.Sprintf(textFarosatUp.to(in), gain), textFarosatSmall.to(in)
	}
	lines := []string{fmt.Sprintf(textFarosatHead.to(in), name), farosatRule, today}
	if mood != "" {
		lines = append(lines, mood)
	}
	lines = append(lines, "", fmt.Sprintf(textFarosatTotal.to(in), grams), fmt.Sprintf(textFarosatPlace.to(in), place),
		farosatRule, textFarosatNext.to(in))
	return strings.Join(lines, "\n")
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
	lines := []string{fmt.Sprintf(textFarosatTop.to(in), limit), farosatRule}
	for i, r := range top {
		place := fmt.Sprintf("%d.", i+1)
		if i < len(farosatMedals) {
			place = farosatMedals[i]
		}
		lines = append(lines, fmt.Sprintf("%s %s — **%d** %s", place, r.Name, r.Grams, textFarosatGrams.to(in)))
		if i == len(farosatMedals)-1 && len(top) > len(farosatMedals) {
			lines = append(lines, "")
		}
	}
	lines = append(lines, farosatRule, textFarosatTake.to(in))
	return say(strings.Join(lines, "\n"))
}
