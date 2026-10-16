package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoPendingModeration means there is no action the person may confirm
// now.
var ErrNoPendingModeration = errors.New("storage: no moderation to confirm")

// ErrOtherModerationPending means the update asked for another action
// before, which still waits for confirmation: one message asked for two,
// as in "delete this and ban him".
var ErrOtherModerationPending = errors.New("storage: the update asked for another moderation already")

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

// UpdateModerated reports whether an update recorded a moderation action:
// when Telegram delivers a confirmation again, whether it was acted on
// already. Polls are not moderation and are left out.
func (s *Store) UpdateModerated(ctx context.Context, updateID int64) (bool, error) {
	var recorded bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM moderation_actions WHERE update_id = $1 AND action <> 'poll')`,
		updateID).Scan(&recorded)
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

// PendingModeration is an action someone asked the bot for, which waits
// until they confirm it by replying to the bot's question about it.
type PendingModeration struct {
	RequesterID, ChatID int64
	Action              string
	// TargetUserID and TargetMessageID are the member and the message acted
	// on, TargetName the member's name and TargetText what the message said.
	TargetUserID, TargetMessageID int64
	TargetName, TargetText        string
	// Minutes is how long a mute lasts; zero for other actions.
	Minutes int
	Reason  string
	// UpdateID is the update that asked.
	UpdateID int64
}

// SavePendingModeration keeps an action until the person who asked confirms
// it, replacing what they asked for before in the same chat, whose question
// then confirms nothing. It reports false, and changes nothing, when the same
// update saved it already: Telegram delivered that update again, and the
// question asked the first time still stands. When the same update saved
// another action, it changes nothing and the error is
// ErrOtherModerationPending.
func (s *Store) SavePendingModeration(ctx context.Context, p PendingModeration) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO pending_moderation
			(requester_id, chat_id, action, target_user_id, target_message_id, target_name, target_text, minutes, reason, update_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (requester_id, chat_id) DO UPDATE
		SET action = EXCLUDED.action, target_user_id = EXCLUDED.target_user_id,
		    target_message_id = EXCLUDED.target_message_id, target_name = EXCLUDED.target_name,
		    target_text = EXCLUDED.target_text, minutes = EXCLUDED.minutes, reason = EXCLUDED.reason,
		    update_id = EXCLUDED.update_id, prompt_message_id = NULL, created_at = now()
		WHERE pending_moderation.update_id <> EXCLUDED.update_id`,
		p.RequesterID, p.ChatID, p.Action, p.TargetUserID, p.TargetMessageID, p.TargetName,
		p.TargetText, p.Minutes, p.Reason, p.UpdateID)
	if err != nil {
		return false, fmt.Errorf("storage: save pending moderation: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	return false, s.checkSamePending(ctx, p)
}

// checkSamePending returns ErrOtherModerationPending when the action p's
// update saved before is not p's. When nothing waits any more, the first
// action was answered meanwhile, and p is taken for a redelivery.
func (s *Store) checkSamePending(ctx context.Context, p PendingModeration) error {
	var action string
	err := s.pool.QueryRow(ctx, `
		SELECT action FROM pending_moderation
		WHERE requester_id = $1 AND chat_id = $2 AND update_id = $3`,
		p.RequesterID, p.ChatID, p.UpdateID).Scan(&action)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("storage: check pending moderation: %w", err)
	case action != p.Action:
		return ErrOtherModerationPending
	}
	return nil
}

// SetModerationPrompt records promptID, the bot's question, on the action
// the update asked for: only a reply to that question confirms it.
func (s *Store) SetModerationPrompt(ctx context.Context, requesterID, chatID, updateID, promptID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE pending_moderation SET prompt_message_id = $4
		WHERE requester_id = $1 AND chat_id = $2 AND update_id = $3`,
		requesterID, chatID, updateID, promptID)
	if err != nil {
		return fmt.Errorf("storage: record moderation question: %w", err)
	}
	return nil
}

// TakePendingModeration returns the action a person asked for in a chat and
// deletes it, so that it is confirmed once. It is returned only when
// promptID is the bot's question about it and it was asked for at most
// maxAge ago; otherwise the error is ErrNoPendingModeration.
func (s *Store) TakePendingModeration(ctx context.Context, requesterID, chatID, promptID int64, maxAge time.Duration) (PendingModeration, error) {
	p := PendingModeration{RequesterID: requesterID, ChatID: chatID}
	err := s.pool.QueryRow(ctx, `
		DELETE FROM pending_moderation
		WHERE requester_id = $1 AND chat_id = $2 AND prompt_message_id = $3
		  AND created_at >= now() - make_interval(secs => $4)
		RETURNING action, target_user_id, target_message_id, target_name, target_text, minutes, reason, update_id`,
		requesterID, chatID, promptID, maxAge.Seconds()).
		Scan(&p.Action, &p.TargetUserID, &p.TargetMessageID, &p.TargetName, &p.TargetText, &p.Minutes, &p.Reason, &p.UpdateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingModeration{}, ErrNoPendingModeration
	}
	if err != nil {
		return PendingModeration{}, fmt.Errorf("storage: take pending moderation: %w", err)
	}
	return p, nil
}

// CancelPendingModeration drops the action a person asked for in a chat, and
// reports whether there was one.
func (s *Store) CancelPendingModeration(ctx context.Context, requesterID, chatID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM pending_moderation WHERE requester_id = $1 AND chat_id = $2`, requesterID, chatID)
	if err != nil {
		return false, fmt.Errorf("storage: cancel pending moderation: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
