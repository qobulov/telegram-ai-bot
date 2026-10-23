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
	return s.reserveDaily(ctx, dailyImages, userID, chatID, since, limit)
}

// ReleaseImage gives back a reservation whose picture could not be drawn.
func (s *Store) ReleaseImage(ctx context.Context, id int64) error {
	return s.releaseDaily(ctx, dailyImages, id)
}

// ReservePresentation takes one of the user's presentations for the day,
// as ReserveImage does a picture.
func (s *Store) ReservePresentation(ctx context.Context, userID, chatID int64, since time.Time, limit int) (int64, bool, error) {
	return s.reserveDaily(ctx, dailyPresentations, userID, chatID, since, limit)
}

// ReleasePresentation gives back a reservation whose presentation could not
// be made.
func (s *Store) ReleasePresentation(ctx context.Context, id int64) error {
	return s.releaseDaily(ctx, dailyPresentations, id)
}

// dailyKind is a table of things made for users and counted per day, and
// the salt that keeps its locks apart from others and from the migration
// lock.
type dailyKind struct {
	table string
	salt  int64
}

var (
	dailyImages        = dailyKind{table: "image_generations", salt: 0x494d4147} // "IMAG"
	dailyPresentations = dailyKind{table: "presentations", salt: 0x50505458}     // "PPTX"
)

func (s *Store) reserveDaily(ctx context.Context, kind dailyKind, userID, chatID int64, since time.Time, limit int) (int64, bool, error) {
	var id int64
	granted := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userID^kind.salt); err != nil {
			return err
		}
		if limit >= 0 {
			var used int
			err := tx.QueryRow(ctx, `SELECT count(*) FROM `+kind.table+` WHERE user_id = $1 AND created_at >= $2`,
				userID, since).Scan(&used)
			if err != nil || used >= limit {
				return err
			}
		}
		granted = true
		return tx.QueryRow(ctx, `INSERT INTO `+kind.table+` (user_id, chat_id) VALUES ($1, $2) RETURNING id`,
			userID, chatID).Scan(&id)
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: reserve from %s: %w", kind.table, err)
	}
	return id, granted, nil
}

func (s *Store) releaseDaily(ctx context.Context, kind dailyKind, id int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM `+kind.table+` WHERE id = $1`, id); err != nil {
		return fmt.Errorf("storage: release from %s: %w", kind.table, err)
	}
	return nil
}
