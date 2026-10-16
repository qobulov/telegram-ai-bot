package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ReplyQuota asks for one answer to a member of a group. The group may get
// at most PerMinute answers in a minute, across all its members, and the
// member at most PerMember in 10 minutes.
type ReplyQuota struct {
	ChatID, UserID       int64
	PerMinute, PerMember int
}

// ReserveGroupReply records an answer to a member of a group if both of the
// quota's limits allow it, and reports whether they did, with the record's
// ID for ReleaseGroupReply. A per-chat lock makes the count and the insert
// one step. The windows are measured by the database clock, so every
// instance counts the same answers.
func (s *Store) ReserveGroupReply(ctx context.Context, q ReplyQuota) (id int64, granted bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, q.ChatID^0x47525059); err != nil { // "GRPY"
			return err
		}
		var inChat, byMember int
		err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE created_at >= now() - interval '1 minute'),
			       count(*) FILTER (WHERE user_id = $2)
			FROM group_replies
			WHERE chat_id = $1 AND created_at >= now() - interval '10 minutes'`,
			q.ChatID, q.UserID).Scan(&inChat, &byMember)
		if err != nil || inChat >= q.PerMinute || byMember >= q.PerMember {
			return err
		}
		granted = true
		return tx.QueryRow(ctx, `INSERT INTO group_replies (chat_id, user_id) VALUES ($1, $2) RETURNING id`, q.ChatID, q.UserID).Scan(&id)
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: reserve group reply: %w", err)
	}
	return id, granted, nil
}

// ReleaseGroupReply gives back an answer whose turn answered nothing.
func (s *Store) ReleaseGroupReply(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM group_replies WHERE id = $1`, id); err != nil {
		return fmt.Errorf("storage: release group reply: %w", err)
	}
	return nil
}
