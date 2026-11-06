package app

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"unicode/utf8"

	"telegram-ai-bot/internal/telegram"
)

type fakeProfile struct {
	mu    sync.Mutex
	have  map[string]telegram.BotProfile
	calls []string
}

func (f *fakeProfile) GetBotProfile(_ context.Context, language string) (telegram.BotProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.have[language], nil
}

func (f *fakeProfile) SetBotBio(_ context.Context, _, language string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "bio "+language)
	return nil
}

func (f *fakeProfile) SetBotDescription(_ context.Context, _, language string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "description "+language)
	return nil
}

func TestProfileTextsFitTelegram(t *testing.T) {
	for language, text := range profileTexts {
		if n := utf8.RuneCountInString(text.bio); n == 0 || n > telegram.MaxBotBio {
			t.Errorf("bio %q is %d characters", language, n)
		}
		if n := utf8.RuneCountInString(text.description); n == 0 || n > telegram.MaxBotDescription {
			t.Errorf("description %q is %d characters", language, n)
		}
	}
}

// Only the texts that differ are set: Telegram limits profile changes.
func TestSetProfileTextsSetsOnlyWhatChanged(t *testing.T) {
	uz := profileTexts[""]
	account := &fakeProfile{have: map[string]telegram.BotProfile{
		"":   {Bio: uz.bio, Description: uz.description},
		"ru": {Bio: profileTexts["ru"].bio, Description: "Делаю презентации и PDF"},
	}}
	SetProfileTexts(context.Background(), account, slog.New(slog.NewTextHandler(io.Discard, nil)))
	slices.Sort(account.calls)
	want := []string{"bio en", "description en", "description ru"}
	if !slices.Equal(account.calls, want) {
		t.Errorf("calls = %q, want %q", account.calls, want)
	}
}
