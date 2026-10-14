package app

import (
	"errors"
	"testing"

	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

func TestMemberCountErrorsMeanTheBotLeft(t *testing.T) {
	for _, err := range []error{
		&telegram.APIError{Code: 403, Description: "Forbidden: bot was kicked from the supergroup chat"},
		&telegram.APIError{Code: 400, Description: "Bad Request: chat not found"},
	} {
		if !errors.Is(memberCountError(err), tools.ErrNotInGroup) {
			t.Errorf("%v is not reported as the bot having left", err)
		}
	}
	other := &telegram.APIError{Code: 429, Description: "Too Many Requests"}
	if errors.Is(memberCountError(other), tools.ErrNotInGroup) {
		t.Errorf("%v is reported as the bot having left", other)
	}
}
