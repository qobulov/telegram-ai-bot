package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReserveBotReply records a reply to another bot in a chat, unless limit
// replies to bots were already given there since since. A per-chat lock
// makes the count and the insert one step.
func (s *Store) ReserveBotReply(ctx context.Context, chatID, botID int64, since time.Time, limit int) (bool, error) {
	granted := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, chatID^0x424f5453); err != nil { // "BOTS"
			return err
		}
		var used int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM bot_replies WHERE chat_id = $1 AND created_at >= $2`,
			chatID, since).Scan(&used)
		if err != nil || used >= limit {
			return err
		}
		granted = true
		_, err = tx.Exec(ctx, `INSERT INTO bot_replies (chat_id, bot_id) VALUES ($1, $2)`, chatID, botID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("storage: reserve bot reply: %w", err)
	}
	return granted, nil
}
