package storage

import (
	"context"
	"fmt"
)

// NoteBoost keeps a boost given to a group and reports whether it is new:
// Telegram sends a boost again when it changes, and the group should be
// thanked for it once.
func (s *Store) NoteBoost(ctx context.Context, chatID int64, boostID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO group_boosts (chat_id, boost_id) VALUES ($1, $2)
		ON CONFLICT (chat_id, boost_id) DO NOTHING`, chatID, boostID)
	if err != nil {
		return false, fmt.Errorf("storage: note boost: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
