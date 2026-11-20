package storage

import (
	"context"
	"fmt"
	"time"
)

// NoteAILimit counts one more reply the chat lost to the AI's credits or
// quota running out, and returns which one it is in this outage: 1 for the
// first. An outage ends when the chat has had no such failure since quiet
// before now; the next one counts from 1 again.
func (s *Store) NoteAILimit(ctx context.Context, chatID int64, now time.Time, quiet time.Duration) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO ai_limit_notices (chat_id, warned, last_at) VALUES ($1, 1, $2)
		ON CONFLICT (chat_id) DO UPDATE SET
			warned = CASE WHEN ai_limit_notices.last_at < $3 THEN 1 ELSE LEAST(ai_limit_notices.warned + 1, 1000) END,
			last_at = $2
		RETURNING warned`, chatID, now, now.Add(-quiet)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("storage: note an AI limit: %w", err)
	}
	return n, nil
}
