package storage

import (
	"context"
	"fmt"
	"time"
)

// ModerationAction is one thing the bot did in a group for someone, kept as
// an audit: who had it delete, mute, ban or pin what, and when. Polls are
// recorded here too, so that one chat gets few of them.
type ModerationAction struct {
	ChatID      int64
	Action      string
	RequesterID int64
	// TargetUserID and TargetMessageID are zero when the action had none.
	TargetUserID, TargetMessageID int64
	// TargetText is what a deleted message said.
	TargetText string
	// Minutes is how long a mute lasts; zero for other actions.
	Minutes int
	Reason  string
	// UpdateID is the Telegram update that asked for it.
	UpdateID int64
}

// RecordModeration notes an action before the bot takes it, and reports
// whether it is new. The same action in the same update is not: Telegram
// delivered the update again, and the action must not be taken twice.
func (s *Store) RecordModeration(ctx context.Context, a ModerationAction) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO moderation_actions
			(chat_id, action, requester_id, target_user_id, target_message_id, target_text, minutes, reason, update_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (update_id, action) DO NOTHING`,
		a.ChatID, a.Action, a.RequesterID, nonZero(a.TargetUserID), nonZero(a.TargetMessageID),
		a.TargetText, nonZero(int64(a.Minutes)), a.Reason, a.UpdateID)
	if err != nil {
		return false, fmt.Errorf("storage: record moderation: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ForgetModeration drops the record of an action Telegram did not take, so
// that the update may try it again.
func (s *Store) ForgetModeration(ctx context.Context, updateID int64, action string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM moderation_actions WHERE update_id = $1 AND action = $2`, updateID, action)
	if err != nil {
		return fmt.Errorf("storage: forget moderation: %w", err)
	}
	return nil
}

// ModerationBy counts the actions taken for a person since a moment, in
// every group. Polls are not moderation and are left out.
func (s *Store) ModerationBy(ctx context.Context, requesterID int64, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM moderation_actions
		WHERE requester_id = $1 AND created_at >= $2 AND action <> 'poll'`, requesterID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: count moderation: %w", err)
	}
	return count, nil
}

// ActionSwear is a swear the swear guard caught, recorded like any other
// moderation.
const ActionSwear = "swear"

// SwearStrikes counts the swears the swear guard caught from one member in a
// chat since a moment.
func (s *Store) SwearStrikes(ctx context.Context, chatID, userID int64, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM moderation_actions
		WHERE chat_id = $1 AND target_user_id = $2 AND action = $3 AND created_at >= $4`,
		chatID, userID, ActionSwear, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: count swear strikes: %w", err)
	}
	return count, nil
}

// ChatModeration counts a chat's actions of one kind since a moment.
func (s *Store) ChatModeration(ctx context.Context, chatID int64, action string, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM moderation_actions
		WHERE chat_id = $1 AND action = $2 AND created_at >= $3`, chatID, action, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: count chat moderation: %w", err)
	}
	return count, nil
}
