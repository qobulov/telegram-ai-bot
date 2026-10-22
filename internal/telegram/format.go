package telegram

import (
	"regexp"
	"strings"
	"unicode/utf16"
)

// messageEntity marks a span of a message's text, in UTF-16 code units as
// Telegram counts them.
type messageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

var (
	// inlineMarks are **bold** and `code` within one line.
	inlineMarks = regexp.MustCompile("\\*\\*([^*\\n]+?)\\*\\*|`([^`\\n]+)`")
	// bulletMarks are "* " and "- " starting a line, which become "• ".
	bulletMarks = regexp.MustCompile(`(?m)^[ \t]*[*-][ \t]+`)
	// headingMarks are "## Title" lines, which become bold lines.
	headingMarks = regexp.MustCompile(`(?m)^[ \t]*#{1,6}[ \t]+(.+?)[ \t]*$`)
)

// formatted turns the little Markdown a model writes into plain text and
// the entities that show it: **bold**, `code`, headings as bold lines and
// "* " bullets as "• ". Entities are computed, not parsed by Telegram, so
// a stray or unmatched mark stays as text and can never make a send fail.
func formatted(text string) (string, []messageEntity) {
	text = bulletMarks.ReplaceAllString(text, "• ")
	text = headingMarks.ReplaceAllString(text, "**$1**")
	var b strings.Builder
	var entities []messageEntity
	offset, last := 0, 0
	for _, m := range inlineMarks.FindAllStringSubmatchIndex(text, -1) {
		before := text[last:m[0]]
		b.WriteString(before)
		offset += utf16Len(before)
		kind, start, end := "bold", m[2], m[3]
		if start < 0 {
			kind, start, end = "code", m[4], m[5]
		}
		inner := text[start:end]
		entities = append(entities, messageEntity{Type: kind, Offset: offset, Length: utf16Len(inner)})
		b.WriteString(inner)
		offset += utf16Len(inner)
		last = m[1]
	}
	b.WriteString(text[last:])
	return b.String(), entities
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}
