package agent

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// oldCommands are commands the bot no longer has, or another bot's, that
// the model still teaches from memory, and the ones that do their work now.
var oldCommands = []struct{ old, current string }{
	{"/cards", "/card"}, {"/mycards", "/card"}, {"/cardtop", "/card"}, {"/market", "/card"}, {"/duel", "/card"},
	{"/myinvites", "/invites"}, {"/invites_week", "/invites"}, {"/invites_month", "/invites"},
	{"/profile", "/rights"},
	// Iris's command list; here /access shows the rank each command needs.
	{".дк", "/access"}, {".ДК", "/access"},
}

// currentCommands is answer with every old command written as the one
// that does its work now: only a whole command, "/cards" or
// "/cards@bot", never "/cardsx" or a link's "https://x/cards".
func currentCommands(answer string) string {
	for _, c := range oldCommands {
		answer = replaceCommand(answer, c.old, c.current)
	}
	return answer
}

// replaceCommand writes each whole old in text as current.
func replaceCommand(text, old, current string) string {
	var b strings.Builder
	for {
		i := strings.Index(text, old)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		before, _ := utf8.DecodeLastRuneInString(text[:i])
		after, _ := utf8.DecodeRuneInString(text[i+len(old):])
		whole := (i == 0 || !partOfCommand(before) && before != '/' && before != ':' && before != '.') &&
			(i+len(old) == len(text) || !partOfCommand(after))
		b.WriteString(text[:i])
		if whole {
			b.WriteString(current)
		} else {
			b.WriteString(old)
		}
		text = text[i+len(old):]
	}
}

// partOfCommand reports a character a command's name may go on with.
func partOfCommand(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
