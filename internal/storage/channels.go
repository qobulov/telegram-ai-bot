package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ChannelSummary is a channel the bot was made an admin of.
type ChannelSummary struct {
	ChatID   int64
	Title    string
	Username string
	// BotStatus is what Telegram last said about the bot there:
	// "administrator", "left" or "kicked".
	BotStatus string
	// Rights are the bot's admin rights there, as Telegram last reported
	// them.
	Rights []string
}

// RecordChannel notes the bot being made an admin of, changed in or removed
// from a channel, as RecordGroup does for groups.
func (s *Store) RecordChannel(ctx context.Context, c GroupStatus) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channels (chat_id, title, username, bot_status, bot_rights) VALUES ($1, $2, nullif($3, ''), $4, $5)
		ON CONFLICT (chat_id) DO UPDATE
		SET title = coalesce(nullif(EXCLUDED.title, ''), channels.title),
		    username = EXCLUDED.username,
		    bot_status = EXCLUDED.bot_status,
		    bot_rights = EXCLUDED.bot_rights,
		    last_seen_at = now()`,
		c.ChatID, c.Title, c.Username, c.BotStatus, rightsArray(c.Rights))
	if err != nil {
		return fmt.Errorf("storage: record channel: %w", err)
	}
	return nil
}

// Channels returns up to limit channels the bot has been an admin of, most
// recently changed first.
func (s *Store) Channels(ctx context.Context, limit int) ([]ChannelSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT chat_id, title, coalesce(username, ''), bot_status, bot_rights
		FROM channels
		ORDER BY last_seen_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read channels: %w", err)
	}
	channels, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChannelSummary, error) {
		var c ChannelSummary
		err := row.Scan(&c.ChatID, &c.Title, &c.Username, &c.BotStatus, &c.Rights)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read channels: %w", err)
	}
	return channels, nil
}
