package telegram

import (
	"sort"
	"strings"
	"unicode/utf16"
)

// Entity types the bot looks at.
const (
	EntityMention     = "mention"      // @username
	EntityTextMention = "text_mention" // a user without a username, linked by ID
	EntityBotCommand  = "bot_command"
)

// MessageEntity marks a special part of a message's text, such as a mention.
// Offset and Length are counted in UTF-16 code units, as Telegram defines
// them: an emoji usually counts as two, so they are neither byte nor
// character positions in a Go string.
type MessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	// User is set for text_mention entities.
	User *User `json:"user"`
}

// Content returns the message's text with its entities, or the caption with
// its entities when the message has no text (a photo or document).
func (m *Message) Content() (string, []MessageEntity) {
	if m.Text != "" {
		return m.Text, m.Entities
	}
	return m.Caption, m.CaptionEntities
}

// EntityText returns the part of text an entity covers. It reports false
// when the entity does not fit inside the text.
func EntityText(text string, e MessageEntity) (string, bool) {
	units := utf16.Encode([]rune(text))
	if !fits(e, len(units)) {
		return "", false
	}
	return string(utf16.Decode(units[e.Offset : e.Offset+e.Length])), true
}

// RemoveEntities cuts the given entities out of text and trims the result.
// Entities that do not fit inside the text are left alone.
func RemoveEntities(text string, remove []MessageEntity) string {
	units := utf16.Encode([]rune(text))
	// Cut from the end so earlier offsets stay valid.
	ordered := append([]MessageEntity(nil), remove...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Offset > ordered[j].Offset })
	for _, e := range ordered {
		if fits(e, len(units)) {
			units = cut(units, e.Offset, e.Offset+e.Length)
		}
	}
	return strings.TrimSpace(string(utf16.Decode(units)))
}

// fits reports whether the entity lies inside a text of the given length.
// It is written without adding offset and length, which could overflow.
func fits(e MessageEntity, length int) bool {
	return e.Offset >= 0 && e.Length > 0 && e.Offset <= length && e.Length <= length-e.Offset
}

// cut removes units[start:end]. When that leaves a space on both sides, one
// of them is dropped so "hey @bot what" becomes "hey what".
func cut(units []uint16, start, end int) []uint16 {
	if start > 0 && end < len(units) && units[start-1] == ' ' && units[end] == ' ' {
		end++
	}
	return append(units[:start:start], units[end:]...)
}
