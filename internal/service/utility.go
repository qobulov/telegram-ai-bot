package service

import (
	"fmt"
	"strings"
	"time"
)

// Everyday commands other group bots have: /ping and /id.

// utilityCommand answers /ping and /id in code; ok is false for any other
// message.
func (s *Service) utilityCommand(in incoming) (outcome, bool) {
	switch commandName(in.text) {
	case "/ping":
		took := time.Since(time.Unix(in.msg.Date, 0))
		if in.msg.Date == 0 || took < 0 {
			took = 0
		}
		return say(fmt.Sprintf("🏓 Pong · %d ms", took.Milliseconds())), true
	case "/id":
		return say(idText(in)), true
	}
	return outcome{}, false
}

// idText is the asker's ID and the chat's, or the ID of the member replied
// to.
func idText(in incoming) string {
	if r := in.msg.ReplyToMessage; r != nil && r.From != nil {
		return fmt.Sprintf("🆔 %s: `%d`", cardMarks.Replace(displayName(r.From)), r.From.ID)
	}
	lines := []string{fmt.Sprintf("🆔 Sizning ID: `%d`", in.userID)}
	if in.group {
		lines = append(lines, fmt.Sprintf("💬 Chat ID: `%d`", in.msg.Chat.ID))
	}
	return strings.Join(lines, "\n")
}
