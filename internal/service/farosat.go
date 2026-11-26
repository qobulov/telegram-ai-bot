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

// The answers are word for word the other bot's, in Uzbek alone, as the
// owners asked: its players know them.
const (
	textFarosatUp    = "🧠 %s, farosatingiz +%d gram oshdi.\n🧠 Hozir: %d gram"
	textFarosatDown  = "🧠 %s, farosatingiz %d gram kamaydi.\n🧠 Hozir: %d gram"
	textFarosatTaken = "%s, bugungi ulushingizni allaqachon oldingiz.\n🧠 Farosatingiz: %d gram\n🕛 Keyingisi yarim tundan keyin"
	textFarosatTop   = "🏆 Chat reytingi — TOP %d"
	textFarosatNone  = "🧠 Hali hech kim farosat olmagan. /farosat yozing!"
	// farosatNameShown is how much of a name the ranking shows, the rest
	// cut to "...".
	farosatNameShown = 17
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
	grams, _, taken, err := s.opts.Farosat.TakeFarosat(ctx, in.scope.ChatID, in.userID, name, startOfDay(s.opts.Now(), location), gain)
	if err != nil {
		s.opts.Logger.Warn("could not take farosat", "chat_id", in.scope.ChatID, "error", err)
		return say(textError.to(in))
	}
	switch {
	case !taken:
		return say(fmt.Sprintf(textFarosatTaken, name, grams))
	case gain < 0:
		return say(fmt.Sprintf(textFarosatDown, name, -gain, grams))
	default:
		return say(fmt.Sprintf(textFarosatUp, name, gain, grams))
	}
}

var farosatMedals = []string{"🥇", "🥈", "🥉"}

// farosatName is a name as the ranking shows it, a long one cut short.
func farosatName(name string) string {
	runes := []rune(name)
	if len(runes) <= farosatNameShown {
		return name
	}
	return string(runes[:farosatNameShown]) + "..."
}

// farosatTop answers /topfarosat with the group's ranking.
func (s *Service) farosatTop(ctx context.Context, in incoming, limit int) outcome {
	top, err := s.opts.Farosat.FarosatTop(ctx, in.scope.ChatID, limit)
	if err != nil {
		s.opts.Logger.Warn("could not read the farosat ranking", "chat_id", in.scope.ChatID, "error", err)
		return say(textError.to(in))
	}
	if len(top) == 0 {
		return say(textFarosatNone)
	}
	lines := []string{fmt.Sprintf(textFarosatTop, limit)}
	for i, r := range top {
		place := fmt.Sprintf("%d.", i+1)
		if i < len(farosatMedals) {
			place = farosatMedals[i]
		}
		lines = append(lines, fmt.Sprintf("%s %s — %d gram", place, farosatName(r.Name), r.Grams))
	}
	return say(strings.Join(lines, "\n"))
}
