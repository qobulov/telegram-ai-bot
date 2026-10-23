package storage

import (
	"context"
	"fmt"
	"time"
)

// GroupMember is a member the bot has seen in a group.
type GroupMember struct {
	ChatID    int64
	UserID    int64
	FirstName string
	Username  string
	IsBot     bool
	// Status is Telegram's: creator, administrator, member, restricted, left
	// or kicked. Empty when a message only shows the member is there.
	Status   string
	LastSeen time.Time
}

// OtherBots counts the bots besides botID seen in a group and not seen to
// leave it. Telegram's member count includes them, and a group padded with
// bots is no bigger for it.
func (s *Store) OtherBots(ctx context.Context, chatID, botID int64) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM group_members
		WHERE chat_id = $1 AND is_bot AND user_id <> $2 AND status NOT IN ('left', 'kicked')`,
		chatID, botID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: count a group's bots: %w", err)
	}
	return count, nil
}

// forgetLeftAfter is how long a member who left or was banned is kept.
const forgetLeftAfter = 90 * 24 * time.Hour

// SeeMember notes a member in a group, with their current name. A status
// replaces the one known; without one, a member known to have left or been
// banned is back as a member, and any other status is kept.
func (s *Store) SeeMember(ctx context.Context, m GroupMember) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO group_members (chat_id, user_id, first_name, username, is_bot, status)
		VALUES ($1, $2, $3, $4, $5, coalesce(nullif($6, ''), 'member'))
		ON CONFLICT (chat_id, user_id) DO UPDATE
		SET first_name = EXCLUDED.first_name,
		    username = EXCLUDED.username,
		    is_bot = EXCLUDED.is_bot,
		    status = CASE
		        WHEN $6 <> '' THEN $6
		        WHEN group_members.status IN ('left', 'kicked') THEN 'member'
		        ELSE group_members.status END,
		    last_seen_at = now()`,
		m.ChatID, m.UserID, m.FirstName, m.Username, m.IsBot, m.Status)
	if err != nil {
		return fmt.Errorf("storage: see group member: %w", err)
	}
	return nil
}

// ActiveMembers counts the people, not bots, seen writing in a group since
// then who have not left it or been banned.
func (s *Store) ActiveMembers(ctx context.Context, chatID int64, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM group_members
		WHERE chat_id = $1 AND NOT is_bot AND status NOT IN ('left', 'kicked') AND last_seen_at >= $2`,
		chatID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: count active members: %w", err)
	}
	return count, nil
}

// GroupMembers returns up to limit of the members seen in a group who have
// not left or been banned, the most recently seen first, and how many such
// members are known in all.
func (s *Store) GroupMembers(ctx context.Context, chatID int64, limit int) ([]GroupMember, int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT chat_id, user_id, first_name, username, is_bot, status, last_seen_at, count(*) OVER ()
		FROM group_members
		WHERE chat_id = $1 AND status NOT IN ('left', 'kicked')
		ORDER BY last_seen_at DESC, user_id
		LIMIT $2`, chatID, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("storage: group members: %w", err)
	}
	defer rows.Close()
	var members []GroupMember
	total := 0
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.ChatID, &m.UserID, &m.FirstName, &m.Username, &m.IsBot, &m.Status, &m.LastSeen, &total); err != nil {
			return nil, 0, fmt.Errorf("storage: group members: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("storage: group members: %w", err)
	}
	return members, total, nil
}
