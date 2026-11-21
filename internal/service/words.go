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
	WordPlace(ctx context.Context, chatID int64, since time.Time, user int64) (int, storage.WordCount, error)
}

// wordTableSize is the most members /words lists.
const wordTableSize = 10

// wordData starts the data of the /words buttons: "wrd:week" and
// "wrd:month" show that period.
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

// wordTable answers /words [oy] in a group: the top 10 writers of the last
// 7 days, or 30 with "oy", by words, the asker's own place under them when
// they are not among them, with buttons that switch the period
// in place. It asks no model.
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
	text, err := s.wordTableText(ctx, &in.msg.Chat, period, language, in.msg.From)
	if err != nil {
		return say(textWordsUnavailable.to(in))
	}
	if s.opts.Buttons == nil {
		return say(text)
	}
	return outcome{text: text, buttons: wordButtons(period, language)}
}

func (s *Service) wordSince(period string) time.Time {
	return startOfDay(s.opts.Now(), s.opts.Location).AddDate(0, 0, 1-wordDays[period]).In(s.opts.Location)
}

// wordTableText is a group's word table for a period, with viewer's own
// place under it when they are not in the top.
func (s *Service) wordTableText(ctx context.Context, chat *telegram.Chat, period, language string, viewer *telegram.User) (string, error) {
	since := s.wordSince(period)
	top, err := s.opts.Words.WordTop(ctx, chat.ID, since, 0, wordTableSize)
	if err != nil {
		s.opts.Logger.Error("could not read the word table", "chat_id", chat.ID, "error", err)
		return "", err
	}
	lines := []string{fmt.Sprintf(textWordsTitle.inLanguage(language), wordPeriodNames[period].inLanguage(language)), ""}
	if len(top) == 0 {
		lines = append(lines, textWordsNone.inLanguage(language))
	}
	inTop := false
	for i, c := range top {
		lines = append(lines, wordLine(i+1, c, language))
		inTop = inTop || (viewer != nil && c.UserID == viewer.ID)
	}
	if viewer != nil && !viewer.IsBot && !inTop && len(top) > 0 {
		place, own, err := s.opts.Words.WordPlace(ctx, chat.ID, since, viewer.ID)
		if err != nil {
			s.opts.Logger.Warn("could not read a member's word place", "chat_id", chat.ID, "error", err)
		} else if place > 0 {
			lines = append(lines, "---------------------", wordLine(place, own, language))
		}
	}
	return strings.Join(lines, "\n"), nil
}

func wordLine(place int, c storage.WordCount, language string) string {
	return fmt.Sprintf(textWordsLine.inLanguage(language), place, cardMarks.Replace(c.Name), spacedInt(c.Words))
}

var wordButtonNames = map[string]localText{
	"week":  {uz: "7 kun", ru: "7 дней", en: "7 days"},
	"month": {uz: "30 kun", ru: "30 дней", en: "30 days"},
}

// wordButtons are the two periods, the one shown ticked.
func wordButtons(shown, language string) [][]telegram.Button {
	var row []telegram.Button
	for _, period := range []string{"week", "month"} {
		name := wordButtonNames[period].inLanguage(language)
		if period == shown {
			name = "✅ " + name
		}
		row = append(row, telegram.Button{Text: name, Data: wordData + period})
	}
	return [][]telegram.Button{row}
}

// pressedWords switches the word table to the period pressed, with the
// presser's own place under it.
func (s *Service) pressedWords(q *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	language := appLanguage(q.From)
	choice := strings.TrimPrefix(q.Data, wordData)
	chat := &q.Message.Chat
	if _, known := wordDays[choice]; !known {
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false)
		return
	}
	text, err := s.wordTableText(ctx, chat, choice, language, q.From)
	if err != nil {
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, textWordsUnavailable.inLanguage(language), true)
		return
	}
	_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false)
	if err := s.opts.Buttons.EditMessageMenu(ctx, chat.ID, q.Message.MessageID, text, wordButtons(choice, language)); err != nil {
		s.opts.Logger.Warn("could not switch the word table", "chat_id", chat.ID, "error", err)
	}
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
	textWordsTitle       = localText{uz: "🗣 Top-10 so'zamollar · %s", ru: "🗣 Топ-10 болтунов · %s", en: "🗣 Top 10 talkers · %s"}
	textWordsLine        = localText{uz: "%d. %s — %s so'z", ru: "%d. %s — %s слов", en: "%d. %s — %s words"}
	textWordsNone        = localText{uz: "Bu davrda hali hech kim yozmagan.", ru: "За этот период ещё никто не писал.", en: "Nobody has written in this period yet."}
	textWordsGroupOnly   = localText{uz: "✍️ /words guruhda ishlaydi.", ru: "✍️ /words работает в группе.", en: "✍️ /words works in a group."}
	textWordsUnavailable = localText{uz: "Hisobni hozir o'qib bo'lmadi, birozdan keyin urinib ko'ring.", ru: "Сейчас не удалось прочитать счёт, попробуйте позже.", en: "Could not read the count right now; try again in a bit."}
)
