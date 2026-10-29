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
// the meme's topic; and for Mobile Legends what of it, then the hero or
// the ID it needs. A choice among a few is a row of buttons under the
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

// askedRequest is a question whose answer the model takes, as a request
// written with it: the meme's topic, a hero's name.
type askedRequest struct {
	question, hint localText
	// request takes the answer.
	request localText
}

// askedRequests are the questions whose answers go to the model, by the
// menu choice that asks them.
var askedRequests = map[string]askedRequest{
	"meme": {
		question: localText{uz: "😂 Mem nima haqida bo'lsin? Mavzuni yozing.", ru: "😂 О чём сделать мем? Напишите тему.", en: "😂 What should the meme be about? Write the topic."},
		hint:     localText{uz: "Masalan: dushanba ertalab", ru: "Например: утро понедельника", en: "For example: Monday morning"},
		request:  localText{uz: "Shu mavzuda mem yasab ber: %s", ru: "Сделай мем на тему: %s", en: "Make a meme about: %s"},
	},
	"hero": {
		question: localText{uz: "🦸 Qaysi qahramon? Nomini yozing.", ru: "🦸 Какой герой? Напишите имя.", en: "🦸 Which hero? Write the name."},
		hint:     heroHint,
		request:  localText{uz: "MLBB: %s qahramoni haqida ma'lumot ber", ru: "MLBB: расскажи о герое %s", en: "MLBB: tell me about the hero %s"},
	},
	"build": {
		question: localText{uz: "🛠 Qaysi qahramonga build kerak?", ru: "🛠 Для какого героя сборка?", en: "🛠 A build for which hero?"},
		hint:     heroHint,
		request:  localText{uz: "MLBB: %s uchun eng yaxshi build", ru: "MLBB: лучшая сборка на %s", en: "MLBB: the best build for %s"},
	},
	"counter": {
		question: localText{uz: "⚔️ Qaysi qahramonga qarshi pik kerak?", ru: "⚔️ Против какого героя нужен пик?", en: "⚔️ A pick against which hero?"},
		hint:     heroHint,
		request:  localText{uz: "MLBB: %s ga qarshi kimni olish kerak?", ru: "MLBB: кого брать против %s?", en: "MLBB: who counters %s?"},
	},
	"skins": {
		question: localText{uz: "👕 Qaysi qahramonning skinlari?", ru: "👕 Скины какого героя?", en: "👕 Which hero's skins?"},
		hint:     heroHint,
		request:  localText{uz: "MLBB: %s skinlari", ru: "MLBB: скины %s", en: "MLBB: %s's skins"},
	},
	"draft": {
		question: localText{uz: "🎯 Dushman jamoa kimlarni oldi? Vergul bilan yozing.", ru: "🎯 Кого взяли враги? Через запятую.", en: "🎯 Whom did the enemy pick? Separate with commas."},
		hint:     localText{uz: "Masalan: Ling, Tigreal", ru: "Например: Ling, Tigreal", en: "For example: Ling, Tigreal"},
		request:  localText{uz: "MLBB draft: dushman jamoa oldi: %s. Men kimni olay?", ru: "MLBB драфт: враги взяли: %s. Кого мне взять?", en: "MLBB draft: the enemy picked %s. Whom should I pick?"},
	},
	"winrate": {
		question: localText{uz: "📈 O'yinlar soni, hozirgi win rate va kerakli win rate'ni yozing.", ru: "📈 Напишите число матчей, текущий винрейт и нужный.", en: "📈 Write your matches, your win rate and the one you want."},
		hint:     localText{uz: "Masalan: 500 48 60", ru: "Например: 500 48 60", en: "For example: 500 48 60"},
		request:  localText{uz: "MLBB: o'yinlar soni, hozirgi win rate va kerakli win rate: %s. Ketma-ket nechta yutish kerak?", ru: "MLBB: матчей, текущий и нужный винрейт: %s. Сколько побед подряд нужно?", en: "MLBB: matches, current and wanted win rate: %s. How many wins in a row do I need?"},
	},
	"player": {
		question: localText{uz: "🆔 O'yinchi ID va zonasini yozing.", ru: "🆔 Напишите ID игрока и зону.", en: "🆔 Write the player's ID and zone."},
		hint:     localText{uz: "Masalan: 1407157025 (15832)", ru: "Например: 1407157025 (15832)", en: "For example: 1407157025 (15832)"},
		request:  localText{uz: "MLBB o'yinchi: %s", ru: "MLBB игрок: %s", en: "MLBB player: %s"},
	},
}

var heroHint = localText{uz: "Masalan: Ling", ru: "Например: Ling", en: "For example: Ling"}

// mlbbChoices are the Mobile Legends menu's buttons, by the choice each
// sends. meta asks nothing: it is asked of the model at once.
var mlbbChoices = []struct {
	choice string
	label  localText
}{
	{"meta", localText{uz: "🔥 Meta", ru: "🔥 Мета", en: "🔥 Meta"}},
	{"hero", localText{uz: "🦸 Qahramon", ru: "🦸 Герой", en: "🦸 Hero"}},
	{"build", localText{uz: "🛠 Build", ru: "🛠 Сборка", en: "🛠 Build"}},
	{"counter", localText{uz: "⚔️ Kontr-pik", ru: "⚔️ Контрпик", en: "⚔️ Counter"}},
	{"skins", localText{uz: "👕 Skinlar", ru: "👕 Скины", en: "👕 Skins"}},
	{"draft", localText{uz: "🎯 Draft", ru: "🎯 Драфт", en: "🎯 Draft"}},
	{"winrate", localText{uz: "📈 Win rate", ru: "📈 Винрейт", en: "📈 Win rate"}},
	{"player", localText{uz: "🆔 O'yinchi", ru: "🆔 Игрок", en: "🆔 Player"}},
}

var textMLBBMeta = localText{uz: "MLBB: hozirgi meta, eng kuchli qahramonlar", ru: "MLBB: текущая мета, самые сильные герои", en: "MLBB: the current meta, the strongest heroes"}

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
		return askFor(in, askedRequests["meme"]), true
	case keyboardMLBB:
		var rows [][]telegram.Button
		for i, c := range mlbbChoices {
			if i%2 == 0 {
				rows = append(rows, nil)
			}
			rows[len(rows)-1] = append(rows[len(rows)-1], telegram.Button{Text: c.label.to(in), Data: menuData + "ml:" + c.choice})
		}
		return outcome{text: textMLBBMenu.to(in), buttons: rows}, true
	}
	return outcome{}, false
}

// askFor asks request's question with the reply box open.
func askFor(in incoming, request askedRequest) outcome {
	return outcome{text: request.question.to(in), ask: request.hint.to(in)}
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
	case "ml":
		if choice == "meta" {
			s.askAsTyped(q, textMLBBMeta.to(in))
			return
		}
		request, ok := askedRequests[choice]
		if !ok {
			return
		}
		out = askFor(in, request)
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

// askedRequestOf is the request a reply to one of askedRequests' questions
// makes, for the model.
func (s *Service) askedRequestOf(in incoming) (string, bool) {
	asked := in.msg.ReplyToMessage
	value := strings.TrimSpace(in.text)
	if in.group || value == "" || asked == nil || asked.From == nil || asked.From.ID != s.opts.Bot.ID {
		return "", false
	}
	for _, request := range askedRequests {
		if holdsText(asked.Text, request.question) {
			return fmt.Sprintf(request.request.to(in), value), true
		}
	}
	return "", false
}

// askAsTyped handles text as if the one who pressed a menu's button had
// written it in reply to the menu: a choice that needs the model and no
// value, such as the meta. It is its own update, the press standing in for
// its ID, handled as the press was: at once on serverless hosting, else
// by the workers.
func (s *Service) askAsTyped(q *telegram.CallbackQuery, text string) {
	update := telegram.Update{UpdateID: -int64(hashOf("typed:"+q.ID) >> 1), Message: &telegram.Message{
		MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat, Text: text,
	}}
	if len(s.queues) == 0 {
		s.HandleNow(update)
		return
	}
	s.Handle(update)
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
	textMLBBMenu  = localText{uz: "🎮 Mobile Legends: nima kerak?", ru: "🎮 Mobile Legends: что нужно?", en: "🎮 Mobile Legends: what do you need?"}
)
