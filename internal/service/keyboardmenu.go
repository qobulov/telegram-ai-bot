package service

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The keyboard's buttons that need a value ask for it before answering:
// the currency, then an amount to convert; the city; the game of the die;
// the meme's topic. A choice among a few is a row of buttons under the
// question, whose press comes back as menuData; anything else is asked
// with the reply box open on the question, and the answer is recognised
// by the question it replies to, so that nothing is kept between the two.

// menuData starts the data of the menus' buttons: "kb:rate:USD".
const menuData = "kb:"

var (
	rateChoices = []string{"USD", "EUR", "RUB", "KZT", "CNY", "GBP"}
	cityChoices = []string{"Toshkent", "Samarqand", "Buxoro", "Andijon", "Namangan", "Farg'ona", "Nukus", "Qarshi"}
	diceChoices = []string{"🎲", "🎯", "🏀", "⚽", "🎳", "🎰"}
	// rateCode finds the currency of a rate answered before: "1 USD =".
	rateCode = regexp.MustCompile(`\b1 ([A-Z]{3}) =`)
)

// memeRequest asks the model for a meme on the topic the sender wrote.
const memeRequest = "Shu mavzuda mem yasab ber: %s"

// menuQuestion answers a button that needs a value with its question.
func menuQuestion(in incoming, button localText) (outcome, bool) {
	switch button {
	case keyboardRate:
		return outcome{text: textWhichCurrency.to(in), buttons: choiceRows(rateChoices, "rate", 3, nil)}, true
	case keyboardWeather:
		other := telegram.Button{Text: textOtherCity.to(in), Data: menuData + "city:"}
		return outcome{text: textWhichCity.to(in), buttons: choiceRows(cityChoices, "city", 2, &other)}, true
	case keyboardDie:
		return outcome{text: textWhichGame.to(in), buttons: choiceRows(diceChoices, "dice", 3, nil)}, true
	case keyboardMeme:
		return outcome{text: textMemeTopic.to(in), ask: textMemeHint.to(in)}, true
	}
	return outcome{}, false
}

// choiceRows are buttons for choices, perRow to a row, each sending its
// choice under kind, with last, if any, on a row of its own.
func choiceRows(choices []string, kind string, perRow int, last *telegram.Button) [][]telegram.Button {
	var rows [][]telegram.Button
	for i, choice := range choices {
		if i%perRow == 0 {
			rows = append(rows, nil)
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], telegram.Button{Text: choice, Data: menuData + kind + ":" + choice})
	}
	if last != nil {
		rows = append(rows, []telegram.Button{*last})
	}
	return rows
}

// pressedMenu answers a press of a menu's choice, in a private chat.
func (s *Service) pressedMenu(q *telegram.CallbackQuery) {
	if q.Message.Chat.Type != telegram.ChatTypePrivate {
		return
	}
	in := incoming{
		// A press has no update ID of its own here; the roll of a die,
		// recorded once per update, needs one, so the press's ID stands
		// in for it.
		updateID: -int64(hashOf(q.ID) >> 1),
		msg:      &telegram.Message{MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat},
		scope:    storage.Scope{ChatID: q.Message.Chat.ID, UserID: q.From.ID},
		userID:   q.From.ID,
	}
	ctx, cancel := context.WithTimeout(context.Background(), keyboardTimeout)
	defer cancel()
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false); err != nil {
		s.opts.Logger.Debug("could not answer a menu press", "user_id", in.userID, "error", err)
	}
	kind, choice, _ := strings.Cut(strings.TrimPrefix(q.Data, menuData), ":")
	var out outcome
	switch kind {
	case "rate":
		args, _ := json.Marshal(map[string]string{"code": choice})
		out = orFailed(in, textError)(keyboardTool(s, ctx, in, "currency_rate", string(args), rateText))
		if out.text != textError.to(in) {
			out.text += "\n\n" + textAmountPrompt.to(in)
			out.ask = "100"
		}
	case "city":
		if choice == "" {
			out = outcome{text: textCityPrompt.to(in), ask: textCityHint.to(in)}
			break
		}
		args, _ := json.Marshal(map[string]string{"city": choice})
		out = orFailed(in, textError)(keyboardTool(s, ctx, in, "get_weather", string(args), weatherText))
	case "dice":
		args, _ := json.Marshal(map[string]string{"emoji": choice})
		var rolled struct{}
		if !s.runTool(ctx, in, "send_dice", string(args), &rolled) {
			out = say(textDieLater.to(in))
		}
	default:
		return
	}
	if out.text != "" {
		s.send(in, telegram.Outgoing{Text: out.text, Ask: out.ask})
	}
}

// menuReply answers a reply to one of the menus' questions: an amount to
// convert, or a city. ok is false for any other message, and for an
// amount that is no number, which is then the model's.
func (s *Service) menuReply(ctx context.Context, in incoming) (outcome, bool) {
	asked := in.msg.ReplyToMessage
	if asked == nil || asked.From == nil || asked.From.ID != s.opts.Bot.ID {
		return outcome{}, false
	}
	switch {
	case holdsText(asked.Text, textAmountPrompt):
		code := rateCode.FindStringSubmatch(asked.Text)
		amount, ok := parseAmount(in.text)
		if code == nil || !ok {
			return outcome{}, false
		}
		args, _ := json.Marshal(map[string]any{"code": code[1], "amount": amount})
		return orFailed(in, textError)(keyboardTool(s, ctx, in, "currency_rate", string(args), conversionText(amount))), true
	case holdsText(asked.Text, textCityPrompt):
		args, _ := json.Marshal(map[string]string{"city": strings.TrimSpace(in.text)})
		return orFailed(in, textCityUnknown)(keyboardTool(s, ctx, in, "get_weather", string(args), weatherText)), true
	}
	return outcome{}, false
}

// memeTopic is the topic a reply to the meme's question gives.
func (s *Service) memeTopic(in incoming) (string, bool) {
	asked := in.msg.ReplyToMessage
	if in.group || asked == nil || asked.From == nil || asked.From.ID != s.opts.Bot.ID || !holdsText(asked.Text, textMemeTopic) {
		return "", false
	}
	topic := strings.TrimSpace(in.text)
	return topic, topic != ""
}

// orFailed answers with failed when the tool failed.
func orFailed(in incoming, failed localText) func(outcome, bool) outcome {
	return func(out outcome, ok bool) outcome {
		if !ok {
			return say(failed.to(in))
		}
		return out
	}
}

// holdsText reports text holding question in any of its languages.
func holdsText(text string, question localText) bool {
	return strings.Contains(text, question.uz) || strings.Contains(text, question.ru) || strings.Contains(text, question.en)
}

// parseAmount reads an amount as people write it: "100", "1 500",
// "2,5", "1500 dollar".
func parseAmount(text string) (float64, bool) {
	fields := strings.Fields(strings.ReplaceAll(text, ",", "."))
	digits := ""
	for _, f := range fields {
		if _, err := strconv.ParseFloat(f, 64); err != nil {
			break
		}
		digits += f
	}
	amount, err := strconv.ParseFloat(digits, 64)
	return amount, err == nil && amount > 0 && amount < 1e12
}

// conversionText writes an amount converted to so'm.
func conversionText(amount float64) func(incoming, rateResult) string {
	return func(in incoming, r rateResult) string {
		converted := amount * r.RateUZS
		if r.ConvertedUZS != nil {
			converted = *r.ConvertedUZS
		}
		return fmt.Sprintf(textConverted.to(in), strconv.FormatFloat(amount, 'f', -1, 64), r.Code, groupDigits(converted), r.RateDate)
	}
}

func hashOf(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

var (
	textWhichCurrency = localText{uz: "💵 Qaysi valyuta?", ru: "💵 Какая валюта?", en: "💵 Which currency?"}
	textAmountPrompt  = localText{
		uz: "So'mga aylantirish uchun summani yozing.",
		ru: "Чтобы перевести в сумы, напишите сумму.",
		en: "To convert to so'm, write an amount.",
	}
	// textConverted takes the amount, the currency, the sum and the date.
	textConverted = localText{
		uz: "💵 %s %s = %s so'm (Markaziy bank, %s)",
		ru: "💵 %s %s = %s сум (ЦБ Узбекистана, %s)",
		en: "💵 %s %s = %s so'm (Central Bank of Uzbekistan, %s)",
	}
	textWhichCity   = localText{uz: "🌤 Qaysi shahar?", ru: "🌤 Какой город?", en: "🌤 Which city?"}
	textOtherCity   = localText{uz: "✏️ Boshqa shahar", ru: "✏️ Другой город", en: "✏️ Another city"}
	textCityPrompt  = localText{uz: "Shahar nomini yozing.", ru: "Напишите название города.", en: "Write the city's name."}
	textCityHint    = localText{uz: "Masalan: Termiz", ru: "Например: Термез", en: "For example: Termez"}
	textCityUnknown = localText{
		uz: "Bunday shaharni topa olmadim. Boshqacha yozib ko'ring.",
		ru: "Не нашла такой город. Попробуйте написать иначе.",
		en: "I couldn't find that city. Try writing it another way.",
	}
	textWhichGame = localText{uz: "Qaysi o'yin?", ru: "Какая игра?", en: "Which game?"}
	textMemeTopic = localText{
		uz: "😂 Mem nima haqida bo'lsin? Mavzuni yozing.",
		ru: "😂 О чём сделать мем? Напишите тему.",
		en: "😂 What should the meme be about? Write the topic.",
	}
	textMemeHint = localText{uz: "Masalan: dushanba ertalab", ru: "Например: утро понедельника", en: "For example: Monday morning"}
)
