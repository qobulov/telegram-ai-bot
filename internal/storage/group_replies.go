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
// quota's limits allow it, and reports whether they did. A per-chat lock
// makes the count and the insert one step. The windows are measured by the
// database clock, so every instance counts the same answers.
func (s *Store) ReserveGroupReply(ctx context.Context, q ReplyQuota) (bool, error) {
	granted := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
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
		_, err = tx.Exec(ctx, `INSERT INTO group_replies (chat_id, user_id) VALUES ($1, $2)`, q.ChatID, q.UserID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("storage: reserve group reply: %w", err)
	}
	return granted, nil
}
