package app

import (
	"context"
	"log/slog"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
)

// SettingsStore reads the bot's settings saved over the environment's.
type SettingsStore interface {
	Settings(ctx context.Context, chatID int64) (map[string]string, error)
	GlobalSettingsVersion(ctx context.Context) (string, error)
}

// LoadSettings is the configuration with the owners' saved settings over
// the environment's, and the version they were read at. A saved value the
// configuration refuses, or settings that cannot be read, leave the
// environment's: a bad setting must not stop the bot.
func LoadSettings(ctx context.Context, env config.Config, getenv func(string) string, store SettingsStore, logger *slog.Logger) (config.Config, string) {
	version, err := store.GlobalSettingsVersion(ctx)
	if err != nil {
		logger.Warn("could not read the saved settings; using the environment's", "error", err)
		return env, ""
	}
	saved, err := store.Settings(ctx, storage.GlobalSettings)
	if err != nil {
		logger.Warn("could not read the saved settings; using the environment's", "error", err)
		return env, ""
	}
	overrides := map[string]string{}
	for name, value := range saved {
		if _, ok := config.FindSetting(name); ok {
			overrides[name] = value
		}
	}
	cfg, err := config.Load(config.Overlay(getenv, overrides))
	if err != nil {
		logger.Error("a saved setting is refused; using the environment's", "error", err)
		return env, version
	}
	return cfg, version
}
