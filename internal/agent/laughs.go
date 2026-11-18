package agent

import (
	"strings"

	"telegram-ai-bot/internal/ai"
)

// laughs are the emoji the model ends its answers with out of habit.
var laughs = []string{"😂", "🤣"}

// Of the bot's last recentAnswers answers in a chat, laughingAnswers with a
// laugh in them make it a habit: the next answer loses its own.
const (
	recentAnswers   = 5
	laughingAnswers = 2
)

// laughsALot reports whether laughingAnswers of the bot's last
// recentAnswers written answers in history laugh.
func laughsALot(history []ai.Message) bool {
	seen, laughing := 0, 0
	for i := len(history) - 1; i >= 0 && seen < recentAnswers; i-- {
		msg := history[i]
		if msg.Role != ai.RoleAssistant || len(msg.ToolCalls) > 0 || msg.Content == "" || strings.HasPrefix(msg.Content, "<") {
			continue
		}
		seen++
		if hasLaugh(msg.Content) {
			laughing++
		}
	}
	return laughing >= laughingAnswers
}

func hasLaugh(text string) bool {
	for _, laugh := range laughs {
		if strings.Contains(text, laugh) {
			return true
		}
	}
	return false
}

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
