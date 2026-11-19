package service

import (
	"strings"
	"unicode/utf8"

	"telegram-ai-bot/internal/telegram"
)

// maxDisplayName bounds a member's name where the bot writes it.
const maxDisplayName = 24

// memberName is how the bot names a member in its messages: their first
// name, or username, short, on one line.
func displayName(user *telegram.User) string {
	name := strings.TrimSpace(user.FirstName)
	if name == "" {
		name = user.Username
	}
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > maxDisplayName {
		name = string([]rune(name)[:maxDisplayName-1]) + "…"
	}
	if name == "" {
		return "?"
	}
	return name
}
