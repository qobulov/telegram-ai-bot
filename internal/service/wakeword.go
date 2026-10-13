package service

import (
	"strings"
	"unicode"
)

const (
	maxAuthorNameChars = 32
	unknownAuthor      = "Someone"
)

// word is one word of a message with its position, in bytes.
type word struct {
	text       string
	start, end int
}

// stripWakeWord reports whether a message calls the bot by one of its wake
// words, and returns the message without the call.
//
// A wake word counts only as a whole word, compared without regard to case,
// and only where a name is used to address someone: as the first or second
// word ("Mira, look", "Hey Mira, look") or as the last word ("look, Mira").
// A name in the middle of a sentence ("I think Mira is wrong") or inside a
// longer word ("Miranda", "Miraga") is not a call.
//
// When the name opens the message, everything up to and including it is
// removed, so "Hey Mira, roast me" becomes "roast me". If nothing would be
// left to say ("Hey Mira", "Mira!!!"), or the name comes last, the message
// is returned whole so the bot can simply answer the call.
func stripWakeWord(text string, wakeWords []string) (string, bool) {
	words := splitWords(text)
	index, found := wakeWordIndex(words, wakeWords)
	if !found {
		return "", false
	}
	whole := strings.TrimSpace(text)
	if index > 1 {
		return whole, true
	}
	rest := strings.TrimLeftFunc(text[words[index].end:], isSeparator)
	if len(splitWords(rest)) == 0 {
		return whole, true
	}
	return strings.TrimSpace(rest), true
}

// wakeWordIndex looks for a wake word in the first, second and last position.
func wakeWordIndex(words []word, wakeWords []string) (int, bool) {
	for _, index := range []int{0, 1, len(words) - 1} {
		if index < 0 || index >= len(words) {
			continue
		}
		if isWakeWord(words[index].text, wakeWords) {
			return index, true
		}
	}
	return 0, false
}

func isWakeWord(candidate string, wakeWords []string) bool {
	// Quotes around a name ('Mira') are not part of it.
	candidate = strings.TrimFunc(candidate, isApostrophe)
	for _, wakeWord := range wakeWords {
		if strings.EqualFold(candidate, wakeWord) {
			return true
		}
	}
	return false
}

// splitWords finds the words in text: runs of letters, digits and
// apostrophes. Apostrophes are kept inside words because Uzbek spells with
// them (o'zing, oʻylab), and so that "Mira's" is one word, not a call.
func splitWords(text string) []word {
	var words []word
	start := -1
	for i, char := range text {
		if isWordChar(char) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			words = append(words, word{text: text[start:i], start: start, end: i})
			start = -1
		}
	}
	if start >= 0 {
		words = append(words, word{text: text[start:], start: start, end: len(text)})
	}
	return words
}

func isWordChar(char rune) bool {
	return unicode.IsLetter(char) || unicode.IsDigit(char) || isApostrophe(char)
}

func isApostrophe(char rune) bool {
	switch char {
	case '\'', '’', '‘', 'ʻ', 'ʼ', '`':
		return true
	}
	return false
}

func isSeparator(char rune) bool {
	return unicode.IsSpace(char) || unicode.IsPunct(char)
}

// authorLabel turns a Telegram first name into the label put in front of a
// group message ("Bek: ..."). The name is chosen by its owner, so anything
// that could fake another speaker or a marker line is removed: line breaks,
// colons and square brackets.
func authorLabel(firstName string) string {
	cleaned := strings.Map(func(char rune) rune {
		switch char {
		case '\n', '\r', ':', '[', ']':
			return ' '
		}
		return char
	}, firstName)
	label := strings.Join(strings.Fields(cleaned), " ")
	if label == "" {
		return unknownAuthor
	}
	if runes := []rune(label); len(runes) > maxAuthorNameChars {
		label = strings.TrimSpace(string(runes[:maxAuthorNameChars]))
	}
	return label
}
