package storage

import (
	"context"
	"fmt"
	"time"
)

// MemberActivity is what the bot saw of one member in a group: how much
// they wrote, when it last saw them, who brought them in, and what was done
// to them.
type MemberActivity struct {
	MessagesToday, MessagesWeek int
	LastSeen                    time.Time
	// JoinedAt and BroughtBy are set when the bot saw them brought in.
	JoinedAt              time.Time
	BroughtBy             string
	Muted, Banned, Warned int
}

// MemberActivityOf reads a member's activity in a chat, today counted from
// today.
func (s *Store) MemberActivityOf(ctx context.Context, chatID, userID int64, today time.Time) (MemberActivity, error) {
	var a MemberActivity
	var lastSeen, joined *time.Time
	var broughtBy *string
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM group_messages WHERE chat_id = $1 AND author_id = $2 AND created_at >= $3)::int,
			(SELECT count(*) FROM group_messages WHERE chat_id = $1 AND author_id = $2 AND created_at >= now() - interval '7 days')::int,
			(SELECT last_seen_at FROM group_members WHERE chat_id = $1 AND user_id = $2),
			(SELECT joined_at FROM group_invites WHERE chat_id = $1 AND user_id = $2 ORDER BY joined_at DESC LIMIT 1),
			(SELECT inviter_name FROM group_invites WHERE chat_id = $1 AND user_id = $2 ORDER BY joined_at DESC LIMIT 1),
			(SELECT count(*) FROM moderation_actions WHERE chat_id = $1 AND target_user_id = $2 AND action = 'mute')::int,
			(SELECT count(*) FROM moderation_actions WHERE chat_id = $1 AND target_user_id = $2 AND action = 'ban')::int,
			(SELECT count(*) FROM moderation_actions WHERE chat_id = $1 AND target_user_id = $2 AND action = 'warn')::int`,
		chatID, userID, today).Scan(&a.MessagesToday, &a.MessagesWeek, &lastSeen, &joined, &broughtBy, &a.Muted, &a.Banned, &a.Warned)
	if err != nil {
		return MemberActivity{}, fmt.Errorf("storage: read a member's activity: %w", err)
	}
	if lastSeen != nil {
		a.LastSeen = *lastSeen
	}
	if joined != nil {
		a.JoinedAt = *joined
	}
	if broughtBy != nil {
		a.BroughtBy = *broughtBy
	}
	return a, nil
}
