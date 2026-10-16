package webapi

import (
	"html"
	"strings"
	"unicode"
)

// cleanText makes text written by outside sites fit to pass on: scripts
// and HTML tags removed, entities decoded, control and invisible
// formatting characters (such as right-to-left overrides) dropped,
// whitespace folded and the length capped at limit characters.
func cleanText(text string, limit int) string {
	text = html.UnescapeString(stripMarkup(text))
	// Again, for a feed that escapes its HTML twice.
	text = stripMarkup(text)
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, text)
	return truncate(strings.Join(strings.Fields(text), " "), limit)
}

func stripMarkup(text string) string {
	return stripTags(dropBlocks(dropBlocks(text, "script"), "style"))
}

// dropBlocks removes <name ...>...</name> elements with their content.
func dropBlocks(text, name string) string {
	lower := asciiLower(text)
	var b strings.Builder
	for {
		start := indexOpening(lower, name)
		if start < 0 {
			break
		}
		end := strings.Index(lower[start:], "</"+name+">")
		if end < 0 {
			text, lower = text[:start], lower[:start]
			break
		}
		end += start + len("</"+name+">")
		b.WriteString(text[:start])
		text, lower = text[end:], lower[end:]
	}
	b.WriteString(text)
	return b.String()
}

// indexOpening finds the first <name tag in lower: "<script>" or
// "<script src=...>", but not "<scripture>", which is just a word.
func indexOpening(lower, name string) int {
	offset := 0
	for {
		start := strings.Index(lower[offset:], "<"+name)
		if start < 0 {
			return -1
		}
		start += offset
		end := start + len("<"+name)
		if end == len(lower) || strings.IndexByte(" \t\r\n>/", lower[end]) >= 0 {
			return start
		}
		offset = end
	}
}

// asciiLower lowers only A to Z, so byte offsets still match text.
func asciiLower(text string) string {
	b := []byte(text)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// stripTags replaces each tag with a space. A "<" not followed by a
// letter, "/" or "!" is text, as in "x < y".
func stripTags(text string) string {
	var b strings.Builder
	for {
		start := strings.IndexByte(text, '<')
		if start < 0 {
			break
		}
		end := strings.IndexByte(text[start:], '>')
		if end < 0 || !startsTag(text[start+1:]) {
			b.WriteString(text[:start+1])
			text = text[start+1:]
			continue
		}
		b.WriteString(text[:start])
		b.WriteByte(' ')
		text = text[start+end+1:]
	}
	b.WriteString(text)
	return b.String()
}

func startsTag(rest string) bool {
	if rest == "" {
		return false
	}
	c := rest[0]
	return c == '/' || c == '!' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// truncate cuts text to at most limit characters, at a word boundary when
// one is near, and marks the cut.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := runes[:limit-1]
	if !unicode.IsSpace(runes[limit-1]) {
		if space := lastSpace(cut); space > len(cut)-20 {
			cut = cut[:space]
		}
	}
	return strings.TrimSpace(string(cut)) + "…"
}

func lastSpace(runes []rune) int {
	for i := len(runes) - 1; i >= 0; i-- {
		if unicode.IsSpace(runes[i]) {
			return i
		}
	}
	return -1
}
