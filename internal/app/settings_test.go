package app

import (
	"context"
	"log/slog"
	"testing"

	"telegram-ai-bot/internal/config"
)

type savedSettings map[string]string

func (s savedSettings) Settings(context.Context, int64) (map[string]string, error) { return s, nil }
func (s savedSettings) GlobalSettingsVersion(context.Context) (string, error)      { return "v1", nil }

// Saved settings go over the environment's; one the configuration refuses
// leaves the environment's whole, and a name /set does not know is ignored.
func TestLoadSettings(t *testing.T) {
	env := map[string]string{"TELEGRAM_BOT_TOKEN": "1:x", "GEMINI_API_KEY": "k", "DB_POSTGRES_URL": "postgres://x@localhost/x"}
	getenv := func(name string) string { return env[name] }
	base, err := config.Load(getenv)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.DiscardHandler)

	cfg, version := LoadSettings(context.Background(), base, getenv, savedSettings{"MAX_OUTPUT_TOKENS": "2048", "TELEGRAM_BOT_TOKEN": "2:stolen"}, logger)
	if cfg.MaxOutputTokens != 2048 || cfg.TelegramToken != "1:x" || version != "v1" {
		t.Errorf("cfg = %d %q %q", cfg.MaxOutputTokens, cfg.TelegramToken, version)
	}
	cfg, _ = LoadSettings(context.Background(), base, getenv, savedSettings{"MAX_OUTPUT_TOKENS": "lots"}, logger)
	if cfg.MaxOutputTokens != base.MaxOutputTokens {
		t.Errorf("a refused value took: %d", cfg.MaxOutputTokens)
	}
}
