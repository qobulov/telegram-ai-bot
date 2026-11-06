package service

import (
	"strings"

	"telegram-ai-bot/internal/telegram"
)

// A private chat gets a small keyboard of Maria's common requests with
// /start and /help, in place of the phone's own; Telegram keeps it there.
// A button sends its text as if typed. The time is answered at once, and
// the rest first ask for what they need, the currency, the city, the game
// or what of Mobile Legends (see keyboardmenu.go); all but Mobile Legends
// are then answered in code, without the model and the day's limit (see
// keyboardanswer.go). The help button is /help. A group never gets the
// keyboard: it would cover every member's own.

// The buttons, each answered by what it says in any of its languages.
var (
	keyboardDie     = localText{uz: "🎲 Zar tashla", ru: "🎲 Брось кубик", en: "🎲 Roll a die"}
	keyboardRate    = localText{uz: "💵 Valyuta kursi", ru: "💵 Курс валют", en: "💵 Exchange rate"}
	keyboardWeather = localText{uz: "🌤 Ob-havo", ru: "🌤 Погода", en: "🌤 Weather"}
	keyboardTime    = localText{uz: "🕐 Soat nechi?", ru: "🕐 Который час?", en: "🕐 What time is it?"}
	keyboardMLBB    = localText{uz: "🎮 MLBB", ru: "🎮 MLBB", en: "🎮 MLBB"}
	keyboardMore    = localText{uz: "📋 Ko'proq", ru: "📋 Ещё", en: "📋 More"}
	// keyboardHelp is answered as /help.
	keyboardHelp = localText{uz: "❓ Yordam", ru: "❓ Помощь", en: "❓ Help"}
	// keyboardAd is the owners' only button: it asks for an ad to post.
	keyboardAd = localText{uz: "📢 Reklama berish", ru: "📢 Дать рекламу", en: "📢 Post an ad"}
	// The buttons as an earlier keyboard named them, which a chat keeps
	// until the keyboard is sent again.
	keyboardOldDollar  = localText{uz: "💵 Dollar kursi", ru: "💵 Курс доллара", en: "💵 Dollar rate"}
	keyboardOldWeather = localText{uz: "🌤 Toshkent ob-havosi", ru: "🌤 Погода в Ташкенте", en: "🌤 Weather in Tashkent"}
)

// keyboardRows are the keyboard's buttons, two to a row. A meme is asked
// for in words: the owners took its button off.
var keyboardRows = [][]localText{
	{keyboardRate, keyboardWeather},
	{keyboardMLBB, keyboardDie},
	{keyboardTime, keyboardMore},
	{keyboardHelp},
}

// keyboard is the keyboard for whoever wrote in, in their language; the
// owners', who post ads and ask the rest in words, has the ad button only.
func (s *Service) keyboard(in incoming) [][]string {
	if s.isOwner(in.msg.From) && s.opts.Tools != nil && s.opts.Buttons != nil {
		return [][]string{{keyboardAd.to(in)}}
	}
	var rows [][]string
	for _, row := range keyboardRows {
		var labels []string
		for _, button := range row {
			labels = append(labels, button.to(in))
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
