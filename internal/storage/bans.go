package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// BannedMember is a member the bot banned and has not unbanned since, with
// who did and why.
type BannedMember struct {
	UserID         int64
	Name, Username string
	Reason         string
	By             int64
	ByName         string
	At             time.Time
}

// BannedMembers are the chat's members whose latest ban or unban through the
// bot was a ban, since the chat's latest amnesty, the latest first.
func (s *Store) BannedMembers(ctx context.Context, chatID int64, limit int) ([]BannedMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.target_user_id, coalesce(t.first_name, ''), coalesce(t.username, ''), coalesce(b.reason, ''),
			b.requester_id, coalesce(r.first_name, ''), b.created_at
		FROM (
			SELECT DISTINCT ON (target_user_id) target_user_id, action, reason, requester_id, created_at
			FROM moderation_actions
			WHERE chat_id = $1 AND action IN ('ban', 'unban') AND target_user_id > 0
				AND created_at > coalesce((SELECT max(created_at) FROM moderation_actions WHERE chat_id = $1 AND action = 'amnesty'), '-infinity')
			ORDER BY target_user_id, created_at DESC, id DESC
		) b
		LEFT JOIN group_members t ON t.chat_id = $1 AND t.user_id = b.target_user_id
		LEFT JOIN group_members r ON r.chat_id = $1 AND r.user_id = b.requester_id
		WHERE b.action = 'ban'
		ORDER BY b.created_at DESC LIMIT $2`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list banned members: %w", err)
	}
	banned, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (BannedMember, error) {
		var b BannedMember
		err := row.Scan(&b.UserID, &b.Name, &b.Username, &b.Reason, &b.By, &b.ByName, &b.At)
		return b, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list banned members: %w", err)
	}
	return banned, nil
}

// InactiveMembers are the chat's members, not bots, the bot last saw write
// before since, the longest silent first.
func (s *Store) InactiveMembers(ctx context.Context, chatID int64, since time.Time, limit int) ([]GroupMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, coalesce(first_name, ''), coalesce(username, '')
		FROM group_members
		WHERE chat_id = $1 AND NOT is_bot AND coalesce(status, 'member') IN ('member', 'restricted')
			AND last_seen_at < $2
		ORDER BY last_seen_at LIMIT $3`, chatID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list inactive members: %w", err)
	}
	members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupMember, error) {
		m := GroupMember{ChatID: chatID}
		err := row.Scan(&m.UserID, &m.FirstName, &m.Username)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list inactive members: %w", err)
	}
	return members, nil
}
