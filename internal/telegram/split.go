package telegram

import (
	"strings"
	"unicode/utf16"
)

// MessageLimit is the longest text Telegram accepts in one message. Telegram
// counts UTF-16 code units, so an emoji usually counts as two.
const MessageLimit = 4096

// Split breaks text into chunks of at most limit UTF-16 units. It cuts after
// a line break or a space when one falls in the second half of a chunk, and
// never inside a character. Joining the chunks gives back the original text.
func Split(text string, limit int) []string {
	var chunks []string
	for text != "" {
		end := prefixEnd(text, limit)
		if end < len(text) {
			end = breakPoint(text, end)
		}
		chunks = append(chunks, text[:end])
		text = text[end:]
	}
	return chunks
}

// prefixEnd returns the byte length of the longest prefix that fits in limit
// UTF-16 units. It always includes at least one character so Split advances.
func prefixEnd(text string, limit int) int {
	units := 0
	for i, r := range text {
		units += max(utf16.RuneLen(r), 1)
		if units > limit && i > 0 {
			return i
		}
	}
	return len(text)
}

func breakPoint(text string, end int) int {
	for _, separator := range []byte{'\n', ' '} {
		if i := strings.LastIndexByte(text[:end], separator); i >= end/2 {
			return i + 1
		}
	}
	return end
}
