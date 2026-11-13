package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Invite is one person brought into a group: by being added (Via "add") or
// by the inviter's own invite link ("link").
type Invite struct {
	ChatID      int64
	UserID      int64
	InviterID   int64
	InviterName string
	Via         string
	At          time.Time
}

// InviterCount is how many people a member brought into a group since a
// time, and how many of them are still there.
type InviterCount struct {
	UserID int64
	Name   string
	Added  int
	Stayed int
}

// RecordInvite notes a person brought into a group. One brought by the same
// member before is not counted again, so leaving and coming back adds
// nothing, but is in the group again.
func (s *Store) RecordInvite(ctx context.Context, inv Invite) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO group_invites (chat_id, user_id, inviter_id, inviter_name, via, joined_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chat_id, user_id, inviter_id) DO UPDATE SET left_at = NULL`,
		inv.ChatID, inv.UserID, inv.InviterID, inv.InviterName, inv.Via, inv.At)
	if err != nil {
		return fmt.Errorf("storage: record an invite: %w", err)
	}
	return nil
}

// MarkInvitee notes that a person brought into a group left it or was
// removed (in false), or came back (in true), whoever brought them: the
// table counts only those still there.
func (s *Store) MarkInvitee(ctx context.Context, chatID, userID int64, in bool, at time.Time) error {
	query := `UPDATE group_invites SET left_at = $3 WHERE chat_id = $1 AND user_id = $2 AND left_at IS NULL`
	args := []any{chatID, userID, at}
	if in {
		query = `UPDATE group_invites SET left_at = NULL WHERE chat_id = $1 AND user_id = $2 AND left_at IS NOT NULL`
		args = args[:2]
	}
	if _, err := s.pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("storage: mark an invitee: %w", err)
	}
	return nil
}

// InviteTop is the members who brought the most people still in the group
// since a time, at most limit, each having brought at least one who stayed;
// with inviter not 0, that member alone, whatever they have. A person who
// left or was removed since is counted in Added and not in Stayed; of two
// with as many stayed, the one fewer left goes first.
func (s *Store) InviteTop(ctx context.Context, chatID int64, since time.Time, inviter int64, limit int) ([]InviterCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT inviter_id, (array_agg(inviter_name ORDER BY joined_at DESC))[1],
			count(*)::int, count(*) FILTER (WHERE left_at IS NULL)::int
		FROM group_invites
		WHERE chat_id = $1 AND joined_at >= $2 AND ($3 = 0 OR inviter_id = $3)
		GROUP BY inviter_id
		HAVING $3 <> 0 OR count(*) FILTER (WHERE left_at IS NULL) > 0
		ORDER BY 4 DESC, 3, inviter_id
		LIMIT $4`, chatID, since, inviter, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read the invite table: %w", err)
	}
	top, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (InviterCount, error) {
		var c InviterCount
		err := row.Scan(&c.UserID, &c.Name, &c.Added, &c.Stayed)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the invite table: %w", err)
	}
	return top, nil
}
