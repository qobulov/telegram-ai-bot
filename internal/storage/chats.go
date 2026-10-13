package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SetMuted records whether the bot is muted in a chat, and who decided.
func (s *Store) SetMuted(ctx context.Context, chatID int64, muted bool, by int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO chats (chat_id, muted, updated_by) VALUES ($1, $2, $3)
		ON CONFLICT (chat_id) DO UPDATE
		SET muted = EXCLUDED.muted, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		chatID, muted, nonZero(by))
	if err != nil {
		return fmt.Errorf("storage: set muted: %w", err)
	}
	return nil
}

// IsMuted reports whether the bot is muted in a chat. A chat with no stored
// setting is not muted.
func (s *Store) IsMuted(ctx context.Context, chatID int64) (bool, error) {
	var muted bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chats WHERE chat_id = $1 AND muted)`, chatID).Scan(&muted)
	if err != nil {
		return false, fmt.Errorf("storage: read muted: %w", err)
	}
	return muted, nil
}

// MutedChats returns the IDs of all chats where the bot is muted. It is read
// once at startup; the running bot keeps the answer in memory.
func (s *Store) MutedChats(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT chat_id FROM chats WHERE muted ORDER BY chat_id`)
	if err != nil {
		return nil, fmt.Errorf("storage: load muted chats: %w", err)
	}
	chats, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("storage: load muted chats: %w", err)
	}
	return chats, nil
}
