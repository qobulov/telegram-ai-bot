package telegram

import (
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// messageEntity marks a span of a message's text, in UTF-16 code units as
// Telegram counts them.
type messageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	// URL is a text_link's, such as tg://user?id=, which tags that member.
	URL string `json:"url,omitempty"`
}

// RichLimit is the most characters a rich message holds.
const RichLimit = 32768

// richBlocks are the lines that make text structured: headings, list items,
// table rows and quotations. Bold alone is not enough: entities show it.
var richBlocks = regexp.MustCompile(`(?m)^[ \t]*(#{1,6}[ \t]|[-*+][ \t]|[0-9]+\.[ \t]|\|.*\||>)`)

// isRich reports text with structure to send as a rich message.
func isRich(text string) bool {
	return utf8.RuneCountInString(text) <= RichLimit && richBlocks.MatchString(text)
}

var (
	// codeBlocks are ```fenced``` blocks, with or without a language,
	// which become pre entities with their text as it is.
	codeBlocks = regexp.MustCompile("(?s)```[A-Za-z0-9_+-]*[ \t]*\n?(.*?)\n?```")
	// bulletMarks are "* ", "- " and "+ " starting a line, which become "• ".
	bulletMarks = regexp.MustCompile(`(?m)^[ \t]*[*+-][ \t]+`)
	// headingMarks are "## Title" lines, which become bold lines.
	headingMarks = regexp.MustCompile(`(?m)^[ \t]*#{1,6}[ \t]+(.+?)[ \t]*$`)
	// memberTag is [name](tg://user?id=123), the tag of a member without a
	// @username, which becomes a link to them.
	memberTag = regexp.MustCompile(`^\[([^\]\n]{1,64})\]\(tg://user\?id=([0-9]{1,15})\)`)
	// webLink is [text](https://...): it becomes "text (https://...)", so
	// that no address is ever hidden behind words.
	webLink = regexp.MustCompile(`^\[([^\]\n]{1,200})\]\((https?://[^\s()]+)\)`)
	// bareURL is an address written out, whose underscores and stars are
	// part of it, never marks.
	bareURL = regexp.MustCompile(`^(?:https?://|www\.)[^\s]+`)
)

// formatted turns the Markdown a model writes into plain text and the
// entities that show it: **bold** and __bold__, *italic* and _italic_,
// `code`, ```code blocks```, a [name](tg://user?id=) tag, [text](url) as the
// text and its address, headings as bold lines and "* " bullets as "• ".
// Entities are computed, not parsed by Telegram, so nothing needs escaping
// and a stray or unmatched mark stays as text and can never make a send
// fail. An underscore inside a word, as in @some_user or a_b, is no mark.
func formatted(text string) (string, []messageEntity) {
	var f formatter
	last := 0
	for _, m := range codeBlocks.FindAllStringSubmatchIndex(text, -1) {
		f.prose(text[last:m[0]])
		f.block(text[m[2]:m[3]])
		last = m[1]
	}
	f.prose(text[last:])
	return f.b.String(), f.entities
}

// formatter builds formatted's text, counting its length in UTF-16 units
// for the entities' offsets.
type formatter struct {
	b        strings.Builder
	units    int
	entities []messageEntity
}

func (f *formatter) write(s string) {
	f.b.WriteString(s)
	f.units += utf16Len(s)
}

// block writes a code block as a pre entity.
func (f *formatter) block(code string) {
	if code == "" {
		return
	}
	f.entities = append(f.entities, messageEntity{Type: "pre", Offset: f.units, Length: utf16Len(code)})
	f.write(code)
}

// prose writes text outside code blocks: its bullets and headings first,
// then its inline marks.
func (f *formatter) prose(text string) {
	text = bulletMarks.ReplaceAllString(text, "• ")
	text = headingMarks.ReplaceAllStringFunc(text, func(line string) string {
		title := headingMarks.FindStringSubmatch(line)[1]
		return "**" + strings.ReplaceAll(title, "**", "") + "**"
	})
	f.inline(text, false)
}

// inline writes text with its inline marks made entities. Within bold or
// italic, emphasized, code is plain text: Telegram lets neither hold code.
func (f *formatter) inline(text string, emphasized bool) {
	for i := 0; i < len(text); {
		rest := text[i:]
		if i == 0 || !isWordByte(text[i-1]) {
			if m := bareURL.FindString(rest); m != "" {
				f.write(m)
				i += len(m)
				continue
			}
		}
		switch rest[0] {
		case '`':
			if end := strings.IndexAny(rest[1:], "`\n"); end > 0 && rest[1+end] == '`' {
				code := rest[1 : 1+end]
				if !emphasized {
					f.entities = append(f.entities, messageEntity{Type: "code", Offset: f.units, Length: utf16Len(code)})
				}
				f.write(code)
				i += end + 2
				continue
			}
		case '[':
			if m := memberTag.FindStringSubmatch(rest); m != nil {
				f.entities = append(f.entities, messageEntity{Type: "text_link", Offset: f.units, Length: utf16Len(m[1]), URL: "tg://user?id=" + m[2]})
				f.write(m[1])
				i += len(m[0])
				continue
			}
			if m := webLink.FindStringSubmatch(rest); m != nil {
				if m[1] == m[2] {
					f.write(m[2])
				} else {
					f.write(m[1] + " (" + m[2] + ")")
				}
				i += len(m[0])
				continue
			}
		case '*', '_':
			if n := f.emphasis(text, i); n > 0 {
				i += n
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(rest)
		f.write(rest[:size])
		i += size
	}
}

// emphasis writes the bold or italic span that opens at text[i], and says
// how many bytes it took; 0 when no span opens there. A span stays within
// a line, neither begins nor ends with a space, and an underscore's span
// is no part of a word: "2 * 3 * 4", "@some_user" and "a__b" have none.
func (f *formatter) emphasis(text string, i int) int {
	mark := text[i : i+1]
	kind := "italic"
	if strings.HasPrefix(text[i:], mark+mark) {
		mark, kind = mark+mark, "bold"
	}
	underscore := mark[0] == '_'
	if underscore && i > 0 && isWordByte(text[i-1]) {
		return 0
	}
	start := i + len(mark)
	if start >= len(text) || text[start] == ' ' {
		return 0
	}
	for from := start; from < len(text); {
		j := strings.Index(text[from:], mark)
		if j < 0 {
			return 0
		}
		end := from + j
		// "***both***" closes at the last two stars, around "*both*".
		for kind == "bold" && end+len(mark) < len(text) && text[end+len(mark)] == mark[0] {
			end++
		}
		inner := text[start:end]
		after := end + len(mark)
		switch {
		case inner == "" || strings.Contains(inner, "\n"):
			return 0
		case strings.HasSuffix(inner, " "),
			underscore && after < len(text) && isWordByte(text[after]),
			kind == "italic" && after < len(text) && text[after] == mark[0]:
			// Not a closing mark: "a * b", "x_y" or a "**" after an italic.
			from = after
			if kind == "italic" && after < len(text) && text[after] == mark[0] {
				from++
			}
			continue
		}
		at := len(f.entities)
		f.entities = append(f.entities, messageEntity{Type: kind, Offset: f.units})
		before := f.units
		f.inline(inner, true)
		f.entities[at].Length = f.units - before
		return after - i
	}
	return 0
}

// isWordByte reports a letter, digit or underscore, or a byte of a
// character beyond ASCII, which is taken for a letter.
func isWordByte(c byte) bool {
	return c == '_' || c >= 0x80 || ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}
