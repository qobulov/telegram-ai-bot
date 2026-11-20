package service

import (
	"strings"
	"testing"
)

// In every language the private help keeps the bot's own commands and drops
// the group's, which work in groups alone.
func TestPrivateHelpHasNoGroupCommands(t *testing.T) {
	for _, lang := range []string{"uz", "ru", "en"} {
		help := PrivateHelpTextIn(lang, Abilities{})
		for _, group := range []string{"/mute", "/ban", "/rank", "/report", "/voteban", "/admins", "/mylink", "/invites", "/rights"} {
			if strings.Contains(help, group) {
				t.Errorf("%s private help names %s", lang, group)
			}
		}
		for _, own := range []string{"/start", "/help", "/about", "/card", "/games", "/id", "/help@username"} {
			if !strings.Contains(help, own) {
				t.Errorf("%s private help lacks %s", lang, own)
			}
		}
	}
}
