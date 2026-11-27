package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
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
	farosatLoss     = 0.05
	farosatMostLoss = 10
	farosatMostGain = 10
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

// farosatRule sets the ranking's title and its medals off from the rest.
const farosatRule = "━━━━━━━━━━━━━━━"

// farosatPlace is a place's mark: a medal for the first three, its
// number after.
func farosatPlace(i int) string {
	if i < len(farosatMedals) {
		return farosatMedals[i]
	}
	return fmt.Sprintf("%d.", i+1)
}

// farosatName is a name as the ranking shows it, a long one cut short.
func farosatName(name string) string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) <= farosatNameShown {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:farosatNameShown])) + "..."
}

// farosatData starts a ranking button's data, followed by its size.
const farosatData = "frs:"

// farosatSizes are the rankings' sizes, a button each under the ranking.
var farosatSizes = []int{10, 30, 50}

// farosatTop answers /topfarosat with the group's ranking and the buttons
// to show it at another size.
func (s *Service) farosatTop(ctx context.Context, in incoming, limit int) outcome {
	text, err := s.farosatTopText(ctx, in.scope.ChatID, limit)
	if err != nil {
		s.opts.Logger.Warn("could not read the farosat ranking", "chat_id", in.scope.ChatID, "error", err)
		return say(textError.to(in))
	}
	return outcome{text: text, buttons: farosatButtons(limit)}
}

// farosatTopText is the group's ranking at its size.
func (s *Service) farosatTopText(ctx context.Context, chatID int64, limit int) (string, error) {
	top, err := s.opts.Farosat.FarosatTop(ctx, chatID, limit)
	if err != nil {
		return "", err
	}
	if len(top) == 0 {
		return textFarosatNone, nil
	}
	lines := []string{"**" + fmt.Sprintf(textFarosatTop, limit) + "**", farosatRule}
	for i, r := range top {
		if i == len(farosatMedals) {
			lines = append(lines, farosatRule)
		}
		lines = append(lines, fmt.Sprintf("%s %s — **%d** gram", farosatPlace(i), farosatName(r.Name), r.Grams))
	}
	return strings.Join(lines, "\n"), nil
}

// farosatButtons are the ranking's sizes, the one shown ticked.
func farosatButtons(shown int) [][]telegram.Button {
	row := make([]telegram.Button, 0, len(farosatSizes))
	for _, size := range farosatSizes {
		name := fmt.Sprintf("TOP %d", size)
		if size == shown {
			name = "✅ " + name
		}
		row = append(row, telegram.Button{Text: name, Data: fmt.Sprintf("%s%d", farosatData, size)})
	}
	return [][]telegram.Button{row}
}

// farosatSize is a ranking size asked for, by a button or after
// /topfarosat; 10 for any other.
func farosatSize(asked string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(asked)); err == nil && slices.Contains(farosatSizes, n) {
		return n
	}
	return farosatSizes[0]
}

// pressedFarosat shows the ranking at the size pressed, in place.
func (s *Service) pressedFarosat(q *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	limit := farosatSize(strings.TrimPrefix(q.Data, farosatData))
	text, err := s.farosatTopText(ctx, q.Message.Chat.ID, limit)
	if err != nil {
		s.opts.Logger.Warn("could not read the farosat ranking", "chat_id", q.Message.Chat.ID, "error", err)
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, textError.inLanguage("uz"), true)
		return
	}
	_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false)
	if err := s.opts.Buttons.EditMessageMenu(ctx, q.Message.Chat.ID, q.Message.MessageID, text, farosatButtons(limit)); err != nil {
		s.opts.Logger.Debug("could not switch the farosat ranking", "chat_id", q.Message.Chat.ID, "error", err)
	}
}
