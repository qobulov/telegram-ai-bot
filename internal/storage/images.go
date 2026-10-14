package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReserveImage takes one of the user's pictures for the day, counted from
// since. It reports false when limit pictures were already drawn; a
// negative limit means no limit. A per-user lock makes the count and the
// insert one step, so simultaneous requests cannot slip past the limit.
// The returned ID is for ReleaseImage when drawing then fails.
func (s *Store) ReserveImage(ctx context.Context, userID, chatID int64, since time.Time, limit int) (int64, bool, error) {
	var id int64
	granted := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, imageLockKey(userID)); err != nil {
			return err
		}
		if limit >= 0 {
			var used int
			err := tx.QueryRow(ctx, `SELECT count(*) FROM image_generations WHERE user_id = $1 AND created_at >= $2`,
				userID, since).Scan(&used)
			if err != nil || used >= limit {
				return err
			}
		}
		granted = true
		return tx.QueryRow(ctx, `INSERT INTO image_generations (user_id, chat_id) VALUES ($1, $2) RETURNING id`,
			userID, chatID).Scan(&id)
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: reserve image: %w", err)
	}
	return id, granted, nil
}

// ReleaseImage gives back a reservation whose picture could not be drawn.
func (s *Store) ReleaseImage(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM image_generations WHERE id = $1`, id); err != nil {
		return fmt.Errorf("storage: release image: %w", err)
	}
	return nil
}

// imageLockKey keeps image locks apart from the migration lock.
func imageLockKey(userID int64) int64 {
	return userID ^ 0x494d4147 // "IMAG"
}
