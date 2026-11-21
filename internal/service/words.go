package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// WordBook counts the words each member writes in each group, a day at a time.
type WordBook interface {
	AddMemberWords(ctx context.Context, chatID, userID int64, name string, day time.Time, words int) error
	WordTop(ctx context.Context, chatID int64, since time.Time, user int64, limit int) ([]storage.WordCount, error)
}

// wordTableSize is the most members /words lists.
const wordTableSize = 20

// wordData starts the data of the /words buttons: "wrd:week" and
// "wrd:month" show that period, "wrd:me" the presser's own count.
const wordData = "wrd:"

// wordDays are the periods /words shows, as days back from today, today
// counted.
var wordDays = map[string]int{"week": 7, "month": 30}

// countWords counts a member's message, text or caption, for /words. A
// bot's, and an anonymous admin's, are not counted.
func (s *Service) countWords(msg *telegram.Message) {
	if s.opts.Words == nil || msg.From == nil || msg.From.IsBot {
		return
	}
	content, _ := msg.Content()
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	day := startOfDay(s.opts.Now(), s.opts.Location).In(s.opts.Location)
	if err := s.opts.Words.AddMemberWords(ctx, msg.Chat.ID, msg.From.ID, displayName(msg.From), day, len(strings.Fields(content))); err != nil {
		s.opts.Logger.Warn("could not count a member's words", "chat_id", msg.Chat.ID, "error", err)
	}
}

// wordTable answers /words [oy] in a group: who wrote the most words in the
// last 7 days, or 30 with "oy", with buttons that switch the period in place
// and show the presser their own count. It asks no model.
func (s *Service) wordTable(ctx context.Context, in incoming) outcome {
	if !in.group {
		return say(textWordsGroupOnly.to(in))
	}
	period := "week"
	switch strings.ToLower(strings.TrimSpace(commandArgument(in.text))) {
	case "oy", "месяц", "month", "30":
		period = "month"
	}
	language := languageOf(in)
	text, err := s.wordTableText(ctx, in.msg.Chat.ID, period, language)
	if err != nil {
		return say(textWordsUnavailable.to(in))
	}
	if s.opts.Buttons == nil {
		return say(text)
	}
	return outcome{text: text, buttons: wordButtons(period, language)}
}

func (s *Service) wordSince(period string) time.Time {
	return startOfDay(s.opts.Now(), s.opts.Location).AddDate(0, 0, 1-wordDays[period])
}

// wordTableText is a group's word table for a period.
func (s *Service) wordTableText(ctx context.Context, chatID int64, period, language string) (string, error) {
	top, err := s.opts.Words.WordTop(ctx, chatID, s.wordSince(period).In(s.opts.Location), 0, wordTableSize)
	if err != nil {
		s.opts.Logger.Error("could not read the word table", "chat_id", chatID, "error", err)
		return "", err
	}
	lines := []string{fmt.Sprintf(textWordsTitle.inLanguage(language), wordPeriodNames[period].inLanguage(language)), ""}
	if len(top) == 0 {
		lines = append(lines, textWordsNone.inLanguage(language))
	}
	for i, c := range top {
		lines = append(lines, fmt.Sprintf(textWordsLine.inLanguage(language), i+1, cardMarks.Replace(c.Name), spacedInt(c.Words), spacedInt(c.Messages)))
	}
	return strings.Join(lines, "\n"), nil
}

var wordButtonNames = map[string]localText{
	"week":  {uz: "7 kun", ru: "7 дней", en: "7 days"},
	"month": {uz: "30 kun", ru: "30 дней", en: "30 days"},
	"me":    {uz: "✍️ Meniki", ru: "✍️ Мои", en: "✍️ Mine"},
}

// wordButtons are the two periods, the one shown ticked, and the presser's
// own count.
func wordButtons(shown, language string) [][]telegram.Button {
	var row []telegram.Button
	for _, period := range []string{"week", "month"} {
		name := wordButtonNames[period].inLanguage(language)
		if period == shown {
			name = "✅ " + name
		}
		row = append(row, telegram.Button{Text: name, Data: wordData + period})
	}
	row = append(row, telegram.Button{Text: wordButtonNames["me"].inLanguage(language), Data: wordData + "me"})
	return [][]telegram.Button{row}
}

// pressedWords switches the word table to the period pressed, or tells the
// presser alone, in a box, how much they wrote.
func (s *Service) pressedWords(q *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	language := appLanguage(q.From)
	choice := strings.TrimPrefix(q.Data, wordData)
	chatID := q.Message.Chat.ID
	if choice == "me" {
		text, err := s.ownWords(ctx, chatID, q.From, language)
		if err != nil {
			text = textWordsUnavailable.inLanguage(language)
		}
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, text, true)
		return
	}
	if _, known := wordDays[choice]; !known {
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false)
		return
	}
	text, err := s.wordTableText(ctx, chatID, choice, language)
	if err != nil {
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, textWordsUnavailable.inLanguage(language), true)
		return
	}
	_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false)
	if err := s.opts.Buttons.EditMessageMenu(ctx, chatID, q.Message.MessageID, text, wordButtons(choice, language)); err != nil {
		s.opts.Logger.Warn("could not switch the word table", "chat_id", chatID, "error", err)
	}
}

// ownWords is how many words user wrote in the group in the last 7 and 30
// days, short enough for a button's box.
func (s *Service) ownWords(ctx context.Context, chatID int64, user *telegram.User, language string) (string, error) {
	count := func(period string) (storage.WordCount, error) {
		top, err := s.opts.Words.WordTop(ctx, chatID, s.wordSince(period).In(s.opts.Location), user.ID, 1)
		if err != nil || len(top) == 0 {
			return storage.WordCount{}, err
		}
		return top[0], nil
	}
	week, err := count("week")
	if err != nil {
		return "", err
	}
	month, err := count("month")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(textWordsOwn.inLanguage(language), cardMarks.Replace(displayName(user)),
		spacedInt(week.Words), spacedInt(week.Messages), spacedInt(month.Words), spacedInt(month.Messages)), nil
}

// spacedInt writes n with a space between thousands: 12 345.
func spacedInt(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + " " + s[i:]
	}
	return s
}

var (
	wordPeriodNames = map[string]localText{
		"week":  {uz: "oxirgi 7 kun", ru: "за 7 дней", en: "the last 7 days"},
		"month": {uz: "oxirgi 30 kun", ru: "за 30 дней", en: "the last 30 days"},
	}
	textWordsTitle       = localText{uz: "✍️ Kim nechta so'z yozdi · %s", ru: "✍️ Кто сколько слов написал · %s", en: "✍️ Who wrote how many words · %s"}
	textWordsLine        = localText{uz: "%d. %s — %s so'z (%s xabar)", ru: "%d. %s — %s слов (%s сообщ.)", en: "%d. %s — %s words (%s messages)"}
	textWordsNone        = localText{uz: "Bu davrda hali hech kim yozmagan.", ru: "За этот период ещё никто не писал.", en: "Nobody has written in this period yet."}
	textWordsOwn         = localText{uz: "✍️ %s\n7 kun: %s so'z, %s xabar\n30 kun: %s so'z, %s xabar", ru: "✍️ %s\n7 дней: %s слов, %s сообщ.\n30 дней: %s слов, %s сообщ.", en: "✍️ %s\n7 days: %s words, %s messages\n30 days: %s words, %s messages"}
	textWordsGroupOnly   = localText{uz: "✍️ /words guruhda ishlaydi.", ru: "✍️ /words работает в группе.", en: "✍️ /words works in a group."}
	textWordsUnavailable = localText{uz: "Hisobni hozir o'qib bo'lmadi, birozdan keyin urinib ko'ring.", ru: "Сейчас не удалось прочитать счёт, попробуйте позже.", en: "Could not read the count right now; try again in a bit."}
)
