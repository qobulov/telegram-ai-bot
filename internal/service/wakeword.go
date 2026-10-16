package service

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxAuthorNameChars = 32
	unknownAuthor      = "Someone"
)

// word is one word of a message with its position, in bytes.
type word struct {
	text       string
	start, end int
	// The character just before the word, and the two just after it; zero
	// where the text begins or ends. They tell a name from part of a link.
	before, after, afterNext rune
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
		if isWakeWord(words[index].text, wakeWords) && standsAlone(words[index]) {
			return index, true
		}
	}
	return 0, false
}

// standsAlone rules out a wake word that is really part of something longer:
// a username (@mira), a hashtag, a link or domain (mira.uz, site/mira), or a
// hyphenated form (Mira-ga).
func standsAlone(w word) bool {
	if strings.ContainsRune("@#/.-", w.before) {
		return false
	}
	joinsNext := w.after == '.' || w.after == '/' || w.after == '-'
	return !joinsNext || !isWordChar(w.afterNext)
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

// splitWords finds the words in text: runs of letters, digits, combining
// marks, underscores and apostrophes. Apostrophes are kept inside words
// because Uzbek spells with them (o'zing, oʻylab), and so that "Mira's" is
// one word, not a call. Underscores are kept so "mira_bot" is one word.
func splitWords(text string) []word {
	runes := []rune(text)
	var words []word
	offset, start, startOffset := 0, -1, 0
	for i, char := range runes {
		switch {
		case isWordChar(char) && start < 0:
			start, startOffset = i, offset
		case !isWordChar(char) && start >= 0:
			words = append(words, newWord(runes, start, i, startOffset))
			start = -1
		}
		offset += utf8.RuneLen(char)
	}
	if start >= 0 {
		words = append(words, newWord(runes, start, len(runes), startOffset))
	}
	return words
}

// newWord describes runes[from:to], which begins at byte startOffset.
func newWord(runes []rune, from, to, startOffset int) word {
	text := string(runes[from:to])
	return word{
		text:      text,
		start:     startOffset,
		end:       startOffset + len(text),
		before:    runeAt(runes, from-1),
		after:     runeAt(runes, to),
		afterNext: runeAt(runes, to+1),
	}
}

func runeAt(runes []rune, index int) rune {
	if index < 0 || index >= len(runes) {
		return 0
	}
	return runes[index]
}

func isWordChar(char rune) bool {
	return unicode.IsLetter(char) || unicode.IsDigit(char) || unicode.IsMark(char) || char == '_' || isApostrophe(char)
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
// colons, square brackets and invisible characters. A member called "You"
// is told apart from botAuthor, the bot's own lines in the log.
func authorLabel(firstName string) string {
	cleaned := strings.Map(func(char rune) rune {
		switch {
		case char == ':' || char == '：' || char == '[' || char == ']':
			return ' '
		case unicode.IsSpace(char):
			return ' '
		case unicode.IsControl(char) || unicode.Is(unicode.Cf, char):
			return -1 // invisible: direction overrides, zero-width characters
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
	if strings.EqualFold(label, botAuthor) {
		label += " (member)"
	}
	return label
}

// usernameLabel returns "@username", or "" when there is none. Telegram
// usernames are letters, digits and underscores; anything else is dropped
// rather than shown, since the label goes in front of what people say.
func usernameLabel(username string) string {
	if username == "" {
		return ""
	}
	for _, char := range username {
		isLetter := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
		isDigit := char >= '0' && char <= '9'
		if !isLetter && !isDigit && char != '_' {
			return ""
		}
	}
	return "@" + username
}
