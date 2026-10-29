package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ModerationAction is one thing the bot did in a group for someone, kept as
// an audit: who had it delete, mute, ban or pin what, and when. Polls are
// recorded here too, and dice rolls, so that one chat gets few of them.
type ModerationAction struct {
	// ID is set on an action read back, such as the one to undo.
	ID          int64
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
	// Undo is what puts the action back, as the moderation tool wrote it;
	// nil when it cannot be undone.
	Undo json.RawMessage
}

// RecordModeration notes an action before the bot takes it, and reports
// whether it is new. The same action in the same update is not: Telegram
// delivered the update again, and the action must not be taken twice.
func (s *Store) RecordModeration(ctx context.Context, a ModerationAction) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO moderation_actions
			(chat_id, action, requester_id, target_user_id, target_message_id, target_text, minutes, reason, update_id, undo)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (update_id, action) DO NOTHING`,
		a.ChatID, a.Action, a.RequesterID, nonZero(a.TargetUserID), nonZero(a.TargetMessageID),
		a.TargetText, nonZero(int64(a.Minutes)), a.Reason, a.UpdateID, nullJSON(a.Undo))
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

// SetModerationUndo keeps what undoes an action recorded earlier, for an
// action whose undo is known only once it is done, such as a new invite
// link.
func (s *Store) SetModerationUndo(ctx context.Context, updateID int64, action string, undo json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `UPDATE moderation_actions SET undo = $3 WHERE update_id = $1 AND action = $2`, updateID, action, nullJSON(undo))
	if err != nil {
		return fmt.Errorf("storage: set moderation undo: %w", err)
	}
	return nil
}

// SetModerationMessage keeps the message an action recorded earlier made,
// such as a poll's, known only once Telegram has sent it.
func (s *Store) SetModerationMessage(ctx context.Context, updateID int64, action string, messageID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE moderation_actions SET target_message_id = $3 WHERE update_id = $1 AND action = $2`, updateID, action, messageID)
	if err != nil {
		return fmt.Errorf("storage: set moderation message: %w", err)
	}
	return nil
}

// Polls are recorded as ActionPoll, with the poll's message once sent, and
// closing one as ActionPollStop, with the same message.
const (
	ActionPoll     = "poll"
	ActionPollStop = "poll_stop"
)

// OpenPoll is the record of a poll the bot made in a chat and has not
// closed: the one in message messageID, or the latest when it is 0. found
// is false when there is none.
func (s *Store) OpenPoll(ctx context.Context, chatID, messageID int64) (a ModerationAction, found bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT p.id, p.chat_id, p.action, p.requester_id, p.target_message_id, p.update_id
		FROM moderation_actions p
		WHERE p.chat_id = $1 AND p.action = 'poll' AND p.target_message_id IS NOT NULL
		  AND ($2 = 0 OR p.target_message_id = $2)
		  AND NOT EXISTS (
			SELECT 1 FROM moderation_actions s
			WHERE s.chat_id = p.chat_id AND s.action = 'poll_stop' AND s.target_message_id = p.target_message_id)
		ORDER BY p.id DESC LIMIT 1`, chatID, messageID).
		Scan(&a.ID, &a.ChatID, &a.Action, &a.RequesterID, &a.TargetMessageID, &a.UpdateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationAction{}, false, nil
	}
	if err != nil {
		return ModerationAction{}, false, fmt.Errorf("storage: read open poll: %w", err)
	}
	return a, true, nil
}

// LastUndoable is the latest action in a chat that can still be undone,
// and whether there is one.
func (s *Store) LastUndoable(ctx context.Context, chatID int64) (ModerationAction, bool, error) {
	var a ModerationAction
	var targetUser, targetMessage *int64
	err := s.pool.QueryRow(ctx, `
		SELECT id, chat_id, action, requester_id, target_user_id, target_message_id, update_id, undo
		FROM moderation_actions
		WHERE chat_id = $1 AND undo IS NOT NULL AND undone_at IS NULL
		ORDER BY id DESC LIMIT 1`, chatID).
		Scan(&a.ID, &a.ChatID, &a.Action, &a.RequesterID, &targetUser, &targetMessage, &a.UpdateID, &a.Undo)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationAction{}, false, nil
	}
	if err != nil {
		return ModerationAction{}, false, fmt.Errorf("storage: last undoable: %w", err)
	}
	if targetUser != nil {
		a.TargetUserID = *targetUser
	}
	if targetMessage != nil {
		a.TargetMessageID = *targetMessage
	}
	return a, true, nil
}

// InviteLink is the record of an invite link the bot made in a chat and has
// not revoked, with the link in its undo: the one given, or the latest when
// link is "". found is false when there is none.
func (s *Store) InviteLink(ctx context.Context, chatID int64, link string) (a ModerationAction, found bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT id, chat_id, action, requester_id, update_id, undo
		FROM moderation_actions
		WHERE chat_id = $1 AND action = 'invite_link' AND undo ? 'link' AND undone_at IS NULL
		  AND ($2 = '' OR undo->>'link' = $2)
		ORDER BY id DESC LIMIT 1`, chatID, link).
		Scan(&a.ID, &a.ChatID, &a.Action, &a.RequesterID, &a.UpdateID, &a.Undo)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationAction{}, false, nil
	}
	if err != nil {
		return ModerationAction{}, false, fmt.Errorf("storage: read invite link: %w", err)
	}
	return a, true, nil
}

// MarkUndone marks an action put back, and reports whether this call did:
// an undo asked twice puts it back once.
func (s *Store) MarkUndone(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE moderation_actions SET undone_at = now() WHERE id = $1 AND undone_at IS NULL`, id)
	if err != nil {
		return false, fmt.Errorf("storage: mark undone: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// nullJSON is NULL for no JSON, so that "undo IS NOT NULL" finds only
// actions that can be undone.
func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}

// ModerationBy counts the actions taken for a person since a moment, in
// every group. Polls, closing them, and dice rolls are not moderation and
// are left out.
func (s *Store) ModerationBy(ctx context.Context, requesterID int64, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM moderation_actions
		WHERE requester_id = $1 AND created_at >= $2 AND action NOT IN ('poll', 'poll_stop', 'dice')`, requesterID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: count moderation: %w", err)
	}
	return count, nil
}

// The swears the swear guard caught, recorded like any other moderation:
// ActionSwear insults a person, ActionSwearAtBot the bot itself and
// ActionSwearWord is an exclamation aimed at nobody.
const (
	ActionSwear      = "swear"
	ActionSwearAtBot = "swear_bot"
	ActionSwearWord  = "swear_word"
)

// What the guards an owner may turn on did: banned a known spammer, deleted
// a link to a malicious host or an unsafe picture.
const (
	ActionSpamBan     = "spam_ban"
	ActionLinkDelete  = "link_delete"
	ActionImageDelete = "image_delete"
)

// SwearStrikes counts the swears of one kind the swear guard caught from one
// member in a chat since a moment.
func (s *Store) SwearStrikes(ctx context.Context, chatID, userID int64, action string, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM moderation_actions
		WHERE chat_id = $1 AND target_user_id = $2 AND action = $3 AND created_at >= $4`,
		chatID, userID, action, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: count swear strikes: %w", err)
	}
	return count, nil
}

// SwornIn reports whether the swear guard already caught one message, in
// any of its versions: an edit of a message it warned about is not a second
// swear.
func (s *Store) SwornIn(ctx context.Context, chatID, messageID int64) (bool, error) {
	var caught bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM moderation_actions
			WHERE chat_id = $1 AND target_message_id = $2 AND action IN ($3, $4, $5))`,
		chatID, messageID, ActionSwear, ActionSwearAtBot, ActionSwearWord).Scan(&caught)
	if err != nil {
		return false, fmt.Errorf("storage: find a caught swear: %w", err)
	}
	return caught, nil
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
