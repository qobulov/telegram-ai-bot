package agent

import (
	"strings"
)

// laughs are the emoji the model ends its answers with out of habit: an
// answer loses them at its end, as the persona asks, a fifth of answers
// having ended with one in the week to 2026-11-22.
var laughs = []string{"😂", "🤣", "😉", "😎", "😁", "😄", "😅", "😆"}

// withoutTrailingLaugh is text without the laughs, and the spaces and
// variation selectors around them, that end it; text itself when nothing
// but laughs would be left.
func withoutTrailingLaugh(text string) string {
	rest := text
	for {
		trimmed := strings.TrimRight(rest, " \t\n"+variationSelector)
		cut := false
		for _, laugh := range laughs {
			if before, found := strings.CutSuffix(trimmed, laugh); found {
				trimmed, cut = before, true
			}
		}
		if !cut {
			break
		}
		rest = trimmed
	}
	if rest == text {
		return text
	}
	rest = strings.TrimRight(rest, " \t\n")
	if !hasWords(rest) {
		return text
	}
	return rest
}
