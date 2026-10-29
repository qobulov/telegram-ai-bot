package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// DailyQuota asks for one of a user's answers in private chats on the day
// that began at Since. Limit answers are allowed in a day, or PremiumLimit
// while the user's premium lasts; 0 means premium changes nothing.
type DailyQuota struct {
	UserID       int64
	Since        time.Time
	Limit        int
	PremiumLimit int
}

// ReservePrivateMessage takes one of the user's answers for the day, and
// returns its ID for ReleasePrivateMessage, when fewer than Limit were taken.
// Otherwise notify is true for the day's first refusal only, which is
// recorded so that the user is told once. A per-user lock makes the count
// and the insert one step, so simultaneous messages cannot slip past the
// limit or be told twice.
func (s *Store) ReservePrivateMessage(ctx context.Context, q DailyQuota) (id int64, allowed, notify bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, q.UserID^0x50524956); err != nil { // "PRIV"
			return err
		}
		limit, err := dailyLimit(ctx, tx, q)
		if err != nil {
			return err
		}
		var used, refused int
		err = tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE NOT denied), count(*) FILTER (WHERE denied)
			FROM private_messages
			WHERE user_id = $1 AND created_at >= $2`,
			q.UserID, q.Since).Scan(&used, &refused)
		switch {
		case err != nil:
			return err
		case used < limit:
			allowed = true
			return tx.QueryRow(ctx, `INSERT INTO private_messages (user_id) VALUES ($1) RETURNING id`, q.UserID).Scan(&id)
		case refused > 0:
			return nil
		}
		notify = true
		_, err = tx.Exec(ctx, `INSERT INTO private_messages (user_id, denied) VALUES ($1, true)`, q.UserID)
		return err
	})
	if err != nil {
		return 0, false, false, fmt.Errorf("storage: reserve private message: %w", err)
	}
	return id, allowed, notify, nil
}

// ReleasePrivateMessage gives back an answer whose turn failed.
func (s *Store) ReleasePrivateMessage(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM private_messages WHERE id = $1 AND NOT denied`, id); err != nil {
		return fmt.Errorf("storage: release private message: %w", err)
	}
	return nil
}

// dailyLimit is the quota's limit for the user: PremiumLimit while their
// premium lasts, Limit otherwise. Asked in the reservation's transaction, a
// premium bought a moment ago counts at once.
func dailyLimit(ctx context.Context, tx pgx.Tx, q DailyQuota) (int, error) {
	if q.PremiumLimit <= 0 {
		return q.Limit, nil
	}
	var premium bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM premium_users WHERE user_id = $1 AND until > now())`, q.UserID).Scan(&premium)
	if err != nil || !premium {
		return q.Limit, err
	}
	return q.PremiumLimit, nil
}
