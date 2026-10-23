package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/agent"
)

// A private chat's small talk and most common questions are answered here,
// without the model: each model call carries the whole persona, thousands of
// tokens, to say "Salom!" back. Such answers take nothing from the day's
// limit. Only a message that is wholly one of these phrases counts, so
// "salom, ob-havo qanaqa?" still goes to the model.

// faqKind is what a short private message asks for.
type faqKind int

const (
	faqGreeting faqKind = iota + 1
	faqHowAreYou
	faqThanks
	faqOkay
	faqName
	faqCreator
	faqOwner
	faqAbilities
)

// faqPhrase is a known message: what it asks for, and its language.
type faqPhrase struct {
	kind faqKind
	lang string
}

// faqPhrases are the known messages, folded as foldPhrase does.
var faqPhrases = map[string]faqPhrase{}

func init() {
	add := func(kind faqKind, lang string, phrases ...string) {
		for _, p := range phrases {
			faqPhrases[foldPhrase(p)] = faqPhrase{kind, lang}
		}
	}
	add(faqGreeting, "uz", "salom", "assalomu alaykum", "assalom", "assalomu aleykum", "salom maria", "salom mari", "xayrli kun", "салом", "ассалому алайкум")
	add(faqGreeting, "ru", "привет", "здравствуйте", "здравствуй", "приветик", "привет мария", "добрый день")
	add(faqGreeting, "en", "hi", "hello", "hey", "hi maria", "hello maria")
	add(faqHowAreYou, "uz", "qalaysan", "qalaysiz", "yaxshimisan", "yaxshimisiz", "nima gap", "nima gaplar", "ishlar qalay", "калайсан", "қалайсиз", "нима гап")
	add(faqHowAreYou, "ru", "как дела", "как ты", "как вы", "как жизнь")
	add(faqHowAreYou, "en", "how are you", "how are you doing", "whats up", "sup")
	add(faqThanks, "uz", "rahmat", "raxmat", "katta rahmat", "rahmat maria", "рахмат", "раҳмат")
	add(faqThanks, "ru", "спасибо", "спс", "благодарю", "спасибо большое")
	add(faqThanks, "en", "thanks", "thank you", "thx", "ty")
	// "ha", "да" and "yes" are left out: they often answer the bot's own
	// question, which the model must see.
	add(faqOkay, "uz", "ok", "okey", "xo'p", "mayli", "tushundim", "ок", "хоп", "майли")
	add(faqOkay, "ru", "окей", "понял", "поняла", "ясно", "ладно")
	add(faqOkay, "en", "okay", "got it", "cool", "nice")
	add(faqName, "uz", "isming nima", "ismingiz nima", "sen kimsan", "siz kimsiz", "исминг нима", "сен кимсан")
	add(faqName, "ru", "как тебя зовут", "как вас зовут", "ты кто", "кто ты")
	add(faqName, "en", "whats your name", "what is your name", "who are you")
	add(faqCreator, "uz", "seni kim yaratgan", "sizni kim yaratgan", "kim yaratgan", "seni kim yasagan", "seni kim dasturlagan", "сени ким яратган")
	add(faqCreator, "ru", "кто тебя создал", "кто вас создал", "кто твой создатель", "кто тебя сделал")
	add(faqCreator, "en", "who made you", "who created you", "who built you")
	add(faqOwner, "uz", "egang kim", "egangiz kim", "xo'jayining kim", "xo'jayiningiz kim", "эганг ким")
	add(faqOwner, "ru", "кто твой хозяин", "кто твой владелец", "чей ты бот")
	add(faqOwner, "en", "who is your owner", "who owns you")
	add(faqAbilities, "uz", "nima qila olasan", "nimalar qila olasan", "nima qila olasiz", "nimalar qila olasiz", "нима кила оласан", "нималар кила оласан")
	add(faqAbilities, "ru", "что ты умеешь", "что умеешь", "что вы умеете", "что ты можешь")
	add(faqAbilities, "en", "what can you do", "what do you do")
}

// foldPhrase is text in lower case, its letters and digits only, with
// apostrophes and punctuation dropped and one space between words, so
// "Salom!!" and "salom" are one phrase.
func foldPhrase(text string) string {
	var words []string
	for word := range strings.FieldsSeq(strings.ToLower(text)) {
		word = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, word)
		if word != "" {
			words = append(words, word)
		}
	}
	return strings.Join(words, " ")
}

// emojiOnly reports text of emoji and spaces alone, such as "😂😂" or "👍".
func emojiOnly(text string) bool {
	seen := false
	for _, r := range text {
		switch {
		case unicode.IsSpace(r), r == '️', r == '‍':
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsPunct(r):
			return false
		default:
			seen = true
		}
	}
	return seen
}

// quickAnswer answers a private message that is only small talk or a common
// question, and reports whether it did. Owners always get the model.
func (s *Service) quickAnswer(ctx context.Context, in incoming) (outcome, bool) {
	if !s.opts.QuickAnswers || in.group || in.fromBot || s.isOwner(in.msg.From) || s.hasMedia(in) {
		return outcome{}, false
	}
	answer, ok := s.quickReply(in.text)
	if !ok {
		return outcome{}, false
	}
	// Saved through the agent, so that the update is answered once and the
	// model sees the exchange in later turns.
	saved, err := s.opts.Replier.Reply(ctx, agent.Request{
		Scope: in.scope, UserID: in.userID, UpdateID: in.updateID, MessageID: in.msg.MessageID,
		Text: in.text, Fixed: &answer,
	})
	switch {
	case errors.Is(err, agent.ErrDuplicate):
		return outcome{duplicate: true}, true
	case err != nil:
		s.opts.Logger.Warn("could not save a quick answer; sending it anyway", "user_id", in.userID, "error", err)
		saved = answer
	}
	return outcome{text: saved.Text, reaction: saved.Reaction, answerID: saved.ID}, true
}

// quickReply is the answer to small talk or a common question, if text is
// one.
func (s *Service) quickReply(text string) (agent.Answer, bool) {
	if emojiOnly(text) {
		return agent.Answer{Reaction: "👍"}, true
	}
	phrase, ok := faqPhrases[foldPhrase(text)]
	if !ok {
		return agent.Answer{}, false
	}
	switch phrase.kind {
	case faqThanks:
		return agent.Answer{Reaction: "❤"}, true
	case faqOkay:
		return agent.Answer{Reaction: "👍"}, true
	}
	return agent.Answer{Text: s.faqText(phrase)}, true
}

// faqText is the answer to a known question, in its language.
func (s *Service) faqText(phrase faqPhrase) string {
	pick := func(t localText) string {
		switch phrase.lang {
		case "ru":
			return t.ru
		case "en":
			return t.en
		}
		return t.uz
	}
	switch phrase.kind {
	case faqGreeting:
		return pick(faqGreetings[rand.IntN(len(faqGreetings))])
	case faqHowAreYou:
		return pick(faqMoods[rand.IntN(len(faqMoods))])
	case faqName:
		return fmt.Sprintf(pick(textMyName), cmp.Or(s.opts.DisplayName, "Maria"))
	case faqCreator, faqOwner:
		return s.makerText(phrase.kind, pick)
	}
	return pick(textAbilities)
}

// makerText says who programmed the bot and whose it is: the developer and
// the other owners when the developer is told apart, the owners otherwise.
func (s *Service) makerText(kind faqKind, pick func(localText) string) string {
	developer := s.opts.Developer
	owners := s.ownerHandles(developer)
	switch {
	case developer != "" && owners == "":
		return fmt.Sprintf(pick(textProgrammedAndOwned), developer)
	case developer != "" && kind == faqOwner:
		return fmt.Sprintf(pick(textOwnedBy), owners, developer)
	case developer != "":
		return fmt.Sprintf(pick(textProgrammedBy), developer, owners)
	case owners != "":
		return fmt.Sprintf(pick(textMadeBy), owners)
	}
	return pick(textMadeByOwners)
}

// ownerHandles are the owners' usernames but except's, joined for a
// sentence, without "@": a private chat is no place to tag them.
func (s *Service) ownerHandles(except string) string {
	var handles []string
	for _, u := range s.opts.OwnerUsernames {
		if u != except {
			handles = append(handles, u)
		}
	}
	return strings.Join(handles, ", ")
}

var (
	faqGreetings = []localText{
		{uz: "Salom! Nima gaplar? 😊", ru: "Привет! Как дела? 😊", en: "Hi! What's up? 😊"},
		{uz: "Va alaykum assalom! Eshitaman.", ru: "Привет-привет! Слушаю.", en: "Hey! I'm listening."},
		{uz: "Salom! Qanday yordam kerak?", ru: "Привет! Чем помочь?", en: "Hello! How can I help?"},
	}
	faqMoods = []localText{
		{uz: "Yaxshi, rahmat! O'zingizda nima gaplar?", ru: "Хорошо, спасибо! А у вас как?", en: "Good, thanks! How about you?"},
		{uz: "Zo'r! Sizdachi?", ru: "Отлично! А у вас?", en: "Great! And you?"},
	}
	textMyName             = localText{uz: "Mening ismim %s. 😊", ru: "Меня зовут %s. 😊", en: "My name is %s. 😊"}
	textMadeBy             = localText{uz: "Meni %s yaratgan.", ru: "Меня создали %s.", en: "I was made by %s."}
	textProgrammedBy       = localText{uz: "Meni %s dasturlagan, egam esa %s.", ru: "Меня запрограммировал %s, а мой владелец — %s.", en: "I was programmed by %s, and my owner is %s."}
	textProgrammedAndOwned = localText{uz: "Meni %s dasturlagan, egam ham o'sha.", ru: "Меня запрограммировал %s, он же мой владелец.", en: "I was programmed by %s, who is also my owner."}
	textOwnedBy            = localText{uz: "Egam — %s. Meni esa %s dasturlagan.", ru: "Мой владелец — %s. А запрограммировал меня %s.", en: "My owner is %s. I was programmed by %s."}
	textMadeByOwners       = localText{uz: "Meni egalarim yaratgan.", ru: "Меня создали мои владельцы.", en: "My owners made me."}
	textAbilities          = localText{
		uz: "Gaplashaman, savollarga javob beraman, valyuta kursi, ob-havo, namoz vaqti, yangiliklar, MLBB qahramonlari, kino va musiqa topaman, rasm, stiker va ovozli xabarlarni tushunaman. Shunchaki yozing!",
		ru: "Болтаю, отвечаю на вопросы, ищу курсы валют, погоду, время намаза, новости, героев MLBB, фильмы и музыку, понимаю фото, стикеры и голосовые. Просто пишите!",
		en: "I chat, answer questions, look up exchange rates, weather, prayer times, news, MLBB heroes, films and music, and understand photos, stickers and voice messages. Just write!",
	}
)
