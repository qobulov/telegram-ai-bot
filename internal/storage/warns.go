package storage

import (
	"context"
	"fmt"
)

// AddWarn gives the member one more warning in the chat and returns how many
// they have.
func (s *Store) AddWarn(ctx context.Context, chatID, userID int64) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO member_warns (chat_id, user_id, count) VALUES ($1, $2, 1)
		ON CONFLICT (chat_id, user_id) DO UPDATE SET count = member_warns.count + 1, updated_at = now()
		RETURNING count`, chatID, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: warn a member: %w", err)
	}
	return count, nil
}

// RemoveWarn takes one of the member's warnings away and returns how many
// are left.
func (s *Store) RemoveWarn(ctx context.Context, chatID, userID int64) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO member_warns (chat_id, user_id, count) VALUES ($1, $2, 0)
		ON CONFLICT (chat_id, user_id) DO UPDATE SET count = greatest(member_warns.count - 1, 0), updated_at = now()
		RETURNING count`, chatID, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: take a warning away: %w", err)
	}
	return count, nil
}

// ResetWarns clears the member's warnings in the chat.
func (s *Store) ResetWarns(ctx context.Context, chatID, userID int64) error {
	if _, err := s.pool.Exec(ctx, `UPDATE member_warns SET count = 0, updated_at = now() WHERE chat_id = $1 AND user_id = $2`, chatID, userID); err != nil {
		return fmt.Errorf("storage: clear a member's warnings: %w", err)
	}
	return nil
}
