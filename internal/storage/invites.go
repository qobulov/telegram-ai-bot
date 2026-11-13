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
// member before is not noted again, so leaving and coming back adds nothing.
func (s *Store) RecordInvite(ctx context.Context, inv Invite) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO group_invites (chat_id, user_id, inviter_id, inviter_name, via, joined_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chat_id, user_id, inviter_id) DO NOTHING`,
		inv.ChatID, inv.UserID, inv.InviterID, inv.InviterName, inv.Via, inv.At)
	if err != nil {
		return fmt.Errorf("storage: record an invite: %w", err)
	}
	return nil
}

// InviteTop is the members who brought the most people into the group since
// a time, at most limit; with inviter not 0, that member alone. A person who
// left or was removed since is counted in Added and not in Stayed.
func (s *Store) InviteTop(ctx context.Context, chatID int64, since time.Time, inviter int64, limit int) ([]InviterCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.inviter_id, (array_agg(i.inviter_name ORDER BY i.joined_at DESC))[1],
			count(*)::int,
			count(*) FILTER (WHERE coalesce(m.status, 'member') NOT IN ('left', 'kicked'))::int
		FROM group_invites i
		LEFT JOIN group_members m ON m.chat_id = i.chat_id AND m.user_id = i.user_id
		WHERE i.chat_id = $1 AND i.joined_at >= $2 AND ($3 = 0 OR i.inviter_id = $3)
		GROUP BY i.inviter_id
		ORDER BY 4 DESC, 3 DESC, i.inviter_id
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
