package service

import (
	"regexp"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/telegram"
)

// Applause is a reply to the bot that needs no words: a sticker, a GIF,
// laughter, an emoji or a nod. Answering each one with a model turn made the
// bot flood its groups, so applause gets an emoji reaction instead: the bot
// is visibly there without taking over the conversation. A short reply can
// still be an answer, though ("ha" is yes in Uzbek, "?" asks what she
// meant), and such a reply is left to the usual answer.

// laughReaction answers laughter. Anything else is answered by one of
// applauseReactions, picked by the message's ID so that the choice varies
// without randomness and stays the same if Telegram redelivers the update.
const laughReaction = "🤣"

var applauseReactions = []string{"👍", "🔥", "😁", "👌"}

// laughWords are laughing words that are not spelled with laughing sounds.
var laughWords = map[string]bool{"lol": true, "kek": true, "rofl": true, "lmao": true, "лол": true, "кек": true}

// nods are words that only acknowledge.
var nods = map[string]bool{"ok": true, "ок": true, "okay": true, "окей": true}

// laughEmoji count as laughter when choosing the reaction, and make a single
// laughing sound laughter: "xa 😂".
const laughEmoji = "😂🤣"

// sadEmoji mark someone who may be down, who needs words rather than a grin.
const sadEmoji = "😢😭💔🥺😔😞☹🙁😿😥😓😩😫"

// arithmetic is a sum someone wants worked out: "2+2", "15*3".
var arithmetic = regexp.MustCompile(`\d\s*[-+*/×÷^=]\s*\d`)

// reactedToApplause answers applause from a member with a reaction, logs
// the message in the group log, and reports whether it did. Only a reply
// to the bot can be applause: a mention or the bot's name is a call and is
// always answered, and so is an owner, whatever they send.
func (s *Service) reactedToApplause(in incoming) bool {
	if !in.group || in.fromBot || in.trigger != triggerReply || s.isOwner(in.msg.From) || !isApplause(in.msg, in.text) {
		return false
	}
	s.react(in, applauseReaction(in.msg, in.text))
	s.logMessage(in.msg)
	return true
}

// isApplause reports whether a message, whose text is text, says nothing
// that needs an answer. Photos, voice messages, videos, files and polls are
// content even without a caption, and a message with nothing the bot can
// read is left to the usual reply.
func isApplause(msg *telegram.Message, text string) bool {
	// Checked first: Telegram also sets Document on a GIF.
	if msg.Sticker != nil || msg.Animation != nil {
		if msg.Sticker != nil && isSad(msg.Sticker.Emoji) {
			return false
		}
		return text == "" || isApplauseText(text, false)
	}
	if carriesContent(msg) || text == "" {
		return false
	}
	return isApplauseText(text, askedSomething(msg.ReplyToMessage))
}

func carriesContent(msg *telegram.Message) bool {
	return len(msg.Photo) > 0 || msg.Voice != nil || msg.Video != nil || msg.VideoNote != nil ||
		msg.Document != nil || msg.Audio != nil || msg.Poll != nil
}

// askedSomething reports a message of the bot's with a question in it.
func askedSomething(replied *telegram.Message) bool {
	if replied == nil {
		return false
	}
	text, _ := replied.Content()
	return strings.ContainsAny(text, "?？")
}

// isApplauseText reports text that only laughs, nods or shows emoji,
// punctuation or digits: "xaxa", "ха-ха)))", "xa 😂", "😂😂😂", "ok", "+1",
// "0". A question, a sum and a sad face are not applause, and neither is a
// number when the bot asked something: then it is the answer.
func isApplauseText(text string, asked bool) bool {
	switch {
	case strings.ContainsAny(text, "?？") || arithmetic.MatchString(text) || isSad(text):
		return false
	case !hasLetters(text):
		return !(asked && strings.IndexFunc(text, unicode.IsDigit) >= 0)
	}
	return isLaughter(text) || nods[strings.ToLower(strings.TrimSpace(text))]
}

func isSad(text string) bool {
	return strings.ContainsAny(text, sadEmoji)
}

// applauseReaction picks the reaction to applause. A sticker laughs when the
// emoji Telegram gives it does.
func applauseReaction(msg *telegram.Message, text string) string {
	if laughs(text) || (msg.Sticker != nil && laughs(msg.Sticker.Emoji)) {
		return laughReaction
	}
	count := int64(len(applauseReactions))
	index := (msg.MessageID%count + count) % count // never negative
	return applauseReactions[index]
}

// laughs reports text with a laughing emoji in it, or text that is laughter.
func laughs(text string) bool {
	return strings.ContainsAny(text, laughEmoji) || isLaughter(text)
}

// isLaughter reports text whose letters, read together, only laugh. Emoji,
// punctuation, digits and spaces are dropped first, so "ха-ха", "xa xa xa"
// and "ха)))" laugh as much as "xaxa" does, and laughing words such as
// "lol" may stand among them. A single sound laughs only with a laughing
// emoji or smiles beside it: alone, "ha" and "xa" mean yes in Uzbek. Text
// without letters is not laughter.
func isLaughter(text string) bool {
	words := letterWords(text)
	var sounds strings.Builder
	for _, word := range words {
		if !laughWords[word] {
			sounds.WriteString(word)
		}
	}
	if len(words) == 0 {
		return false
	}
	if sounds.Len() == 0 {
		return true
	}
	count := laughSounds(sounds.String())
	return count >= 2 || (count == 1 && (strings.ContainsAny(text, laughEmoji) || strings.Contains(text, "))")))
}

// laughSounds counts the sounds of laughing that letters spell, and is 0
// when they spell anything else: "Хахаха" is 3, "ахах" and "хх" are 2, "xa"
// is 1. A doubled vowel is not laughter: "haa" means "yes" in Uzbek.
func laughSounds(letters string) int {
	consonants := 0
	afterVowel := false
	for _, char := range strings.ReplaceAll(letters, "пх", "х") {
		switch char {
		case 'x', 'h', 'х', 'ҳ':
			consonants++
			afterVowel = false
		case 'a', 'e', 'а', 'е':
			if afterVowel {
				return 0
			}
			afterVowel = true
		default:
			return 0
		}
	}
	return consonants
}

func hasLetters(text string) bool {
	return strings.IndexFunc(text, unicode.IsLetter) >= 0
}

// letterWords returns the runs of letters in text, in lower case. Emoji,
// punctuation, digits and spaces only separate them.
func letterWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(char rune) bool { return !unicode.IsLetter(char) })
}
