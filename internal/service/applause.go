package service

import (
	"strings"
	"unicode"

	"telegram-ai-bot/internal/telegram"
)

// Applause is a reply to the bot that needs no words: a sticker, a GIF,
// laughter, or a couple of characters. Answering each one with a model turn
// made the bot flood its groups, so applause gets an emoji reaction instead:
// the bot is visibly there without taking over the conversation.

// maxApplauseChars is the most visible characters a reply can have and
// still need no answer whatever they say ("ok", "+1", "0").
const maxApplauseChars = 2

// laughReaction answers laughter. Anything else is answered by one of
// applauseReactions, picked by the message's ID so that the choice varies
// without randomness and stays the same if Telegram redelivers the update.
const laughReaction = "🤣"

var applauseReactions = []string{"👍", "🔥", "😁", "👌"}

// laughWords are laughing words that are not spelled with laughing sounds.
var laughWords = map[string]bool{"lol": true, "kek": true, "rofl": true, "lmao": true, "лол": true, "кек": true}

// laughEmoji count as laughter when choosing the reaction.
const laughEmoji = "😂🤣"

// reactedToApplause answers applause from a member with a reaction, logs
// the message for the day's log, and reports whether it did. Only a reply
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
		return text == "" || isApplauseText(text)
	}
	if carriesContent(msg) || text == "" {
		return false
	}
	return isApplauseText(text)
}

func carriesContent(msg *telegram.Message) bool {
	return len(msg.Photo) > 0 || msg.Voice != nil || msg.Video != nil || msg.VideoNote != nil ||
		msg.Document != nil || msg.Audio != nil || msg.Poll != nil
}

// isApplauseText reports text of at most two visible characters, or made
// only of emoji, punctuation, digits and laughter: "xaxa", "ахахах)))",
// "😂😂😂".
func isApplauseText(text string) bool {
	if visibleChars(text) <= maxApplauseChars {
		return true
	}
	for _, word := range letterWords(text) {
		if !isLaughter(word) {
			return false
		}
	}
	return true
}

// applauseReaction picks the reaction to applause.
func applauseReaction(msg *telegram.Message, text string) string {
	if laughs(text) {
		return laughReaction
	}
	count := int64(len(applauseReactions))
	index := (msg.MessageID%count + count) % count // never negative
	return applauseReactions[index]
}

// laughs reports text with a laughing word or emoji in it.
func laughs(text string) bool {
	if strings.ContainsAny(text, laughEmoji) {
		return true
	}
	for _, word := range letterWords(text) {
		if isLaughter(word) {
			return true
		}
	}
	return false
}

// isLaughter reports a laughing word: one of laughWords, or a word spelled
// only with the sounds of laughing, at least two of them: "xaxa", "Хахаха",
// "ахахах", "hehe", "пхпх", "ҳаҳа". One sound is not laughter: "ha", "xa"
// and "haa" mean "yes" in Uzbek.
func isLaughter(word string) bool {
	if laughWords[word] {
		return true
	}
	sounds := 0
	for _, char := range strings.ReplaceAll(word, "пх", "х") {
		switch char {
		case 'x', 'h', 'х', 'ҳ':
			sounds++
		case 'a', 'e', 'а', 'е':
		default:
			return false
		}
	}
	return sounds >= 2
}

// letterWords returns the runs of letters in text, in lower case. Emoji,
// punctuation, digits and spaces only separate them.
func letterWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(char rune) bool { return !unicode.IsLetter(char) })
}

// visibleChars counts the characters of text that show, leaving out spaces
// and the invisible ones that only join or style the others, such as the
// variation selector in "❤️".
func visibleChars(text string) int {
	count := 0
	for _, char := range text {
		if !unicode.IsSpace(char) && !unicode.IsMark(char) && !unicode.Is(unicode.Cf, char) {
			count++
		}
	}
	return count
}
