package service

import (
	"math/rand/v2"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/stickers"
)

// "Мари гиф", "Maria gif pubg", "гиф кулги ташла": a GIF asked for in a word
// or two is sent without the model, which only picked one and cost a turn
// of the group's quota. A topic is searched in English, as GIPHY knows it:
// a mood word becomes the bot's sticker category, a common Uzbek or Russian
// word its English one, and a name in Latin letters ("pubg", "naruto") goes
// as it is; any other topic is still the model's, which translates it.

// gifWords are the words that ask for a GIF.
var gifWords = map[string]bool{"gif": true, "gifka": true, "гиф": true, "гифка": true, "гифку": true, "гифки": true}

// gifFillers are the words such a request may have besides a topic.
var gifFillers = map[string]bool{
	"tashla": true, "ташла": true, "tashlab": true, "ber": true, "бер": true, "yubor": true, "юбор": true,
	"bitta": true, "битта": true, "iltimos": true, "илтимос": true, "qani": true, "қани": true,
	"кинь": true, "скинь": true, "дай": true, "отправь": true, "пожалуйста": true, "одну": true,
	"send": true, "a": true, "one": true, "please": true, "me": true,
	"haqida": true, "ҳақида": true, "хакида": true, "tasha": true, "про": true, "about": true, "of": true,
}

// gifTopics are the moods and common topics in Uzbek and Russian, and what
// GIPHY is asked: a sticker category, which it knows by its own words, or
// an English word.
var gifTopics = map[string]string{
	"kulgi": "funny", "kulgili": "funny", "кулги": "funny", "кулгили": "funny", "смешно": "funny", "смешная": "funny", "ржака": "funny", "funny": "funny", "lol": "funny",
	"xafa": "sad", "хафа": "sad", "грустно": "sad", "грустная": "sad", "sad": "sad",
	"yig'la": "sad crying", "йиғла": "sad crying", "плачь": "sad crying", "плач": "sad crying",
	"tabrik": "celebration", "табрик": "celebration", "bayram": "celebration", "байрам": "celebration", "поздравляю": "celebration", "праздник": "celebration",
	"zo'r": "cool", "зўр": "cool", "круто": "cool", "cool": "cool",
	"jahl": "angry", "жаҳл": "angry", "злой": "angry", "злость": "angry", "angry": "angry",
	"shok": "shock", "шок": "shock", "shock": "shock",
	"facepalm": "facepalm", "фейспалм": "facepalm",
	"sevgi": "love", "севги": "love", "любовь": "love", "love": "love", "quchoq": "comfort", "қучоқ": "comfort", "обнимашки": "comfort",
	"mushuk": "cat", "мушук": "cat", "кот": "cat", "котик": "cat", "кошка": "cat",
	"kuchuk": "dog", "кучук": "dog", "собака": "dog", "пёс": "dog",
	"raqs": "dance", "рақс": "dance", "танец": "dance", "танцы": "dance",
	"salom": "hello", "салом": "hello", "привет": "hello",
	"rahmat": "thank you", "раҳмат": "thank you", "спасибо": "thank you",
	"xayr": "bye", "хайр": "bye", "пока": "bye",
	"uyqu": "sleepy", "уйқу": "sleepy", "спать": "sleepy",
	"ovqat": "food", "овқат": "food", "еда": "food",
	"futbol": "football", "футбол": "football",
	"g'alaba": "winner", "ғалаба": "winner", "победа": "winner",
	"anime": "anime", "аниме": "anime",
}

// gifRequest reads a request for a GIF in a word or two, the bot's name
// aside, and the topic to search, a random mood when none was named. ok is
// false for any other message, or a topic only the model can put in
// English.
func (s *Service) gifRequest(in incoming) (topic string, ok bool) {
	if s.opts.GIFs == nil || s.opts.AnimationSender == nil || s.opts.MediaSlots == nil {
		return "", false
	}
	written, _ := in.msg.Content()
	asked := false
	var rest []string
	for _, w := range strings.Fields(strings.ToLower(written)) {
		w = strings.Trim(w, ".,!?:;«»\"`")
		switch {
		case w == "", isWakeWord(w, s.opts.WakeWords), strings.EqualFold(w, "@"+s.opts.Bot.Username), gifFillers[w]:
		case gifWords[w]:
			asked = true
		default:
			rest = append(rest, w)
		}
	}
	if !asked || len(rest) > 3 {
		return "", false
	}
	return gifTopic(rest)
}

// asksForGIF reports a request gifRequest answers, which asks no model and
// so takes nothing from the group's quota of answers.
func (s *Service) asksForGIF(in incoming) bool {
	_, ok := s.gifRequest(in)
	return ok
}

// gifTopic is what GIPHY is asked for the words of a request.
func gifTopic(words []string) (string, bool) {
	if len(words) == 0 {
		return stickers.Categories[rand.IntN(len(stickers.Categories))], true
	}
	if len(words) == 1 {
		if topic, ok := gifTopics[words[0]]; ok {
			return topic, true
		}
	}
	topic := strings.Join(words, " ")
	for _, r := range topic {
		if r > unicode.MaxASCII {
			return "", false
		}
	}
	return topic, true
}

// gifAnswer sends the GIF asked for, or says there is none of it.
func (s *Service) gifAnswer(in incoming, topic string) outcome {
	id, claimed := s.sendGIF(in, topic, 0)
	if id != 0 {
		if in.group {
			// The log shows the bot's GIF, as it does one the model chose.
			s.ambient.add(in.scope.ChatID, 0, s.opts.Bot.ID, botAuthor, "<gif "+topic+">")
		}
		return outcome{}
	}
	if !claimed {
		return say(textError.to(in))
	}
	if in.group {
		s.ambient.add(in.scope.ChatID, 0, s.opts.Bot.ID, botAuthor, textNoGIF.to(in))
	}
	return say(textNoGIF.to(in))
}
