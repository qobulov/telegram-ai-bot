package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoPendingBan means there is no ban the person may confirm now.
var ErrNoPendingBan = errors.New("storage: no ban to confirm")

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

// ModerationRecorded reports whether an update has recorded an action: when
// Telegram delivers the update again, whether it was acted on already.
func (s *Store) ModerationRecorded(ctx context.Context, updateID int64, action string) (bool, error) {
	var recorded bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM moderation_actions WHERE update_id = $1 AND action = $2)`,
		updateID, action).Scan(&recorded)
	if err != nil {
		return false, fmt.Errorf("storage: check moderation: %w", err)
	}
	return recorded, nil
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

// PendingBan is a ban someone asked for and has not confirmed yet.
type PendingBan struct {
	RequesterID, ChatID, TargetUserID int64
	TargetName                        string
	// UpdateID is the update that asked; only a later one may confirm.
	UpdateID int64
}

// SavePendingBan keeps a ban until the person who asked confirms it. It
// replaces the ban they asked for before in the same chat.
func (s *Store) SavePendingBan(ctx context.Context, b PendingBan) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pending_bans (requester_id, chat_id, target_user_id, target_name, update_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (requester_id, chat_id) DO UPDATE
		SET target_user_id = EXCLUDED.target_user_id, target_name = EXCLUDED.target_name,
		    update_id = EXCLUDED.update_id, created_at = now()`,
		b.RequesterID, b.ChatID, b.TargetUserID, b.TargetName, b.UpdateID)
	if err != nil {
		return fmt.Errorf("storage: save pending ban: %w", err)
	}
	return nil
}

// TakePendingBan returns the ban a person asked for in a chat and deletes
// it, so that it is confirmed once. Only a ban asked for in an update before
// updateID, at most maxAge ago, is returned: the message that asked cannot
// confirm itself. Otherwise it returns ErrNoPendingBan.
func (s *Store) TakePendingBan(ctx context.Context, requesterID, chatID, updateID int64, maxAge time.Duration) (PendingBan, error) {
	b := PendingBan{RequesterID: requesterID, ChatID: chatID}
	err := s.pool.QueryRow(ctx, `
		DELETE FROM pending_bans
		WHERE requester_id = $1 AND chat_id = $2 AND update_id < $3
		  AND created_at >= now() - make_interval(secs => $4)
		RETURNING target_user_id, target_name, update_id`, requesterID, chatID, updateID, maxAge.Seconds()).
		Scan(&b.TargetUserID, &b.TargetName, &b.UpdateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingBan{}, ErrNoPendingBan
	}
	if err != nil {
		return PendingBan{}, fmt.Errorf("storage: take pending ban: %w", err)
	}
	return b, nil
}

// CancelPendingBan drops the ban a person asked for in a chat, and reports
// whether there was one.
func (s *Store) CancelPendingBan(ctx context.Context, requesterID, chatID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM pending_bans WHERE requester_id = $1 AND chat_id = $2`, requesterID, chatID)
	if err != nil {
		return false, fmt.Errorf("storage: cancel pending ban: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
