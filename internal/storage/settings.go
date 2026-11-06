package storage

import (
	"context"
	"fmt"
	"time"
)

// GlobalSettings is the chat ID of the settings that hold for the whole bot.
const GlobalSettings = 0

// Settings are a chat's settings over the environment's, by name; chat
// GlobalSettings has the bot's.
func (s *Store) Settings(ctx context.Context, chatID int64) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, value FROM settings WHERE chat_id = $1`, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: settings: %w", err)
	}
	defer rows.Close()
	settings := map[string]string{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, fmt.Errorf("storage: settings: %w", err)
		}
		settings[name] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: settings: %w", err)
	}
	return settings, nil
}

// SetSetting saves a chat's setting, replacing the one before it.
func (s *Store) SetSetting(ctx context.Context, chatID int64, name, value string, setBy int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO settings (chat_id, name, value, set_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (chat_id, name) DO UPDATE SET value = EXCLUDED.value, set_by = EXCLUDED.set_by, updated_at = now()`,
		chatID, name, value, setBy)
	if err != nil {
		return fmt.Errorf("storage: set setting: %w", err)
	}
	return nil
}

// ClearSetting removes a chat's setting, so that the next one down holds:
// the bot's for a group, the environment's for the bot.
func (s *Store) ClearSetting(ctx context.Context, chatID int64, name string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM settings WHERE chat_id = $1 AND name = $2`, chatID, name); err != nil {
		return fmt.Errorf("storage: clear setting: %w", err)
	}
	return nil
}

// GlobalSettingsVersion changes whenever the bot's settings do: the latest
// change and how many there are, so that a removal shows too.
func (s *Store) GlobalSettingsVersion(ctx context.Context) (string, error) {
	var latest *time.Time
	var count int
	err := s.pool.QueryRow(ctx, `SELECT max(updated_at), count(*) FROM settings WHERE chat_id = 0`).Scan(&latest, &count)
	if err != nil {
		return "", fmt.Errorf("storage: settings version: %w", err)
	}
	if latest == nil {
		return "none", nil
	}
	return fmt.Sprintf("%d:%d", latest.UnixNano(), count), nil
}
