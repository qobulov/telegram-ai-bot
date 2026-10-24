package storage

import (
	"context"
	"fmt"
)

// Skip is a message addressed to the bot that it did not answer, and why,
// as "group_quota" or "overloaded".
type Skip struct {
	ChatID, UserID, MessageID int64
	Reason                    string
}

// NoteSkip records a message the bot did not answer.
func (s *Store) NoteSkip(ctx context.Context, skip Skip) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO skipped_messages (chat_id, user_id, message_id, reason) VALUES ($1, $2, $3, $4)`,
		skip.ChatID, skip.UserID, skip.MessageID, skip.Reason)
	if err != nil {
		return fmt.Errorf("storage: note skipped message: %w", err)
	}
	return nil
}
