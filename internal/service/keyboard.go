package service

import (
	"strings"

	"telegram-ai-bot/internal/telegram"
)

// A private chat gets a small keyboard of Maria's common requests with
// /start and /help, in place of the phone's own; Telegram keeps it there.
// A button sends its text as if typed. The die, the dollar, the weather and
// the time are answered in code, without the model and the day's limit
// (see keyboardanswer.go); the meme is answered as typed, by the model,
// whose words a meme needs. The help button is /help. A group never gets
// the keyboard: it would cover every member's own.

// keyboardRequest is one button: what it says in each language.
type keyboardRequest struct {
	text localText
	// meme marks the button that needs the make_meme tool.
	meme bool
}

// The buttons, each answered by what it says in any of its languages.
var (
	keyboardMeme    = localText{uz: "😂 Mem yasab ber", ru: "😂 Сделай мем", en: "😂 Make a meme"}
	keyboardDie     = localText{uz: "🎲 Zar tashla", ru: "🎲 Брось кубик", en: "🎲 Roll a die"}
	keyboardDollar  = localText{uz: "💵 Dollar kursi", ru: "💵 Курс доллара", en: "💵 Dollar rate"}
	keyboardWeather = localText{uz: "🌤 Toshkent ob-havosi", ru: "🌤 Погода в Ташкенте", en: "🌤 Weather in Tashkent"}
	keyboardTime    = localText{uz: "🕐 Soat nechi?", ru: "🕐 Который час?", en: "🕐 What time is it?"}
	// keyboardHelp is answered as /help.
	keyboardHelp = localText{uz: "❓ Yordam", ru: "❓ Помощь", en: "❓ Help"}
)

// keyboardRows are the keyboard's buttons, two to a row.
var keyboardRows = [][]keyboardRequest{
	{{text: keyboardMeme, meme: true}, {text: keyboardDie}},
	{{text: keyboardDollar}, {text: keyboardWeather}},
	{{text: keyboardTime}, {text: keyboardHelp}},
}

// keyboard is the keyboard for whoever wrote in, in their language, without
// the meme button when there is no meme tool: a button that cannot be
// answered is worse than none.
func (s *Service) keyboard(in incoming) [][]string {
	var rows [][]string
	for _, row := range keyboardRows {
		var labels []string
		for _, button := range row {
			if button.meme && !s.opts.MemesEnabled {
				continue
			}
			labels = append(labels, button.text.to(in))
		}
		rows = append(rows, labels)
	}
	return rows
}

// withKeyboard gives out the keyboard when it answers /start or /help in a
// private chat, and has no buttons of its own, which a message cannot carry
// together with a keyboard.
func (s *Service) withKeyboard(in incoming, out outcome) outcome {
	switch commandName(in.text) {
	case "/start", "/help":
		if !in.group && out.text != "" && len(out.buttons) == 0 {
			out.keyboard = s.keyboard(in)
		}
	}
	return out
}

// pressedHelp reports a private message that is the help button, in any of
// its languages.
func pressedHelp(in incoming) bool {
	return !in.group && pressed(in, keyboardHelp)
}

// celebrationWords are what an answer that congratulates someone says, as
// says folds it: on a birthday, a holiday, an exam passed.
var celebrationWords = []string{
	"tabrik", "tugilgan kun", "muborak", "табрик", "туғилган кун", "муборак",
	"поздравл", "с днём рождения", "с днем рождения", "с праздником",
	"congratulations", "congrats", "happy birthday",
}

// effectFor is the effect for an answer in a private chat: 🎉 when it
// congratulates, chosen here from the words rather than by the model,
// which could name an effect Telegram does not have. A group gets none:
// Telegram plays effects only in private chats.
func effectFor(in incoming, answer string) string {
	if in.group || !holdsAny(answer, celebrationWords) {
		return ""
	}
	return telegram.EffectParty
}

var withoutApostrophes = strings.NewReplacer("'", "", "ʻ", "", "ʼ", "", "’", "", "‘", "", "`", "")

// holdsAny reports whether text, in lower case without apostrophes, holds
// one of words.
func holdsAny(text string, words []string) bool {
	folded := withoutApostrophes.Replace(strings.ToLower(text))
	for _, word := range words {
		if strings.Contains(folded, word) {
			return true
		}
	}
	return false
}
