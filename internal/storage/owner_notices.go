package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReserveOwnerNotice takes the group's one message to its owners for the
// day that began at since, and returns its ID for ReleaseOwnerNotice; ok is
// false when the group already sent one that day. A per-group lock makes
// the count and the insert one step, so two requests at once cannot both
// pass.
func (s *Store) ReserveOwnerNotice(ctx context.Context, chatID, requesterID int64, since time.Time) (id int64, ok bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, chatID^0x4f574e52); err != nil { // "OWNR"
			return err
		}
		var sent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM owner_notices WHERE chat_id = $1 AND created_at >= $2`, chatID, since).Scan(&sent); err != nil {
			return err
		}
		if sent > 0 {
			return nil
		}
		ok = true
		return tx.QueryRow(ctx, `INSERT INTO owner_notices (chat_id, requester_id) VALUES ($1, $2) RETURNING id`, chatID, requesterID).Scan(&id)
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: reserve owner notice: %w", err)
	}
	return id, ok, nil
}

// ReleaseOwnerNotice gives back a reservation whose message was not sent.
func (s *Store) ReleaseOwnerNotice(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM owner_notices WHERE id = $1`, id); err != nil {
		return fmt.Errorf("storage: release owner notice: %w", err)
	}
	return nil
}
