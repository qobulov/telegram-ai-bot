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

// ModerationTargets are the members the bot acted on in the chat since a
// time, the latest first, with the names the group log last saw them by,
// also once they have left or were banned: whom a reply to the bot's notice,
// or a name, means after the member is gone.
func (s *Store) ModerationTargets(ctx context.Context, chatID int64, since time.Time, limit int) ([]GroupMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.target_user_id, coalesce(m.first_name, ''), coalesce(m.username, ''), coalesce(m.is_bot, false), coalesce(m.status, '')
		FROM (
			SELECT target_user_id, max(created_at) AS at
			FROM moderation_actions
			WHERE chat_id = $1 AND target_user_id > 0 AND created_at >= $2
			GROUP BY target_user_id
		) a
		LEFT JOIN group_members m ON m.chat_id = $1 AND m.user_id = a.target_user_id
		ORDER BY a.at DESC, a.target_user_id
		LIMIT $3`, chatID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: moderation targets: %w", err)
	}
	targets, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupMember, error) {
		m := GroupMember{ChatID: chatID}
		err := row.Scan(&m.UserID, &m.FirstName, &m.Username, &m.IsBot, &m.Status)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: moderation targets: %w", err)
	}
	return targets, nil
}
