package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The kinds of reminder: words the member asked to be reminded of, or that
// their next Kartalar card may be opened.
const (
	ReminderText = "text"
	ReminderCard = "card"
)

// The outcomes of a reminder once the call that claimed it is done with it.
const (
	ReminderSent    = "sent"
	ReminderSkipped = "skipped"
	ReminderFailed  = "failed"
)

// Reminder is one reminder, waiting or claimed.
type Reminder struct {
	ID int64
	// ChatID is where it is sent: the group it was asked in, or the user's
	// private chat.
	ChatID int64
	UserID int64
	// UserName is the asker's name, for the mention in a group.
	UserName string
	// MessageID is the message it answers, 0 for none.
	MessageID int64
	Kind      string
	Text      string
	// Language is "uz", "ru" or "en", for the words the bot adds itself.
	Language string
	DueAt    time.Time
	Attempts int
	// LastDraw is, as claimed, when the user last drew a card; zero for
	// one who never did. A card reminder is not sent when it is too soon.
	LastDraw time.Time
}

// ErrReminderLimit is returned by AddReminder when the user, or the chat,
// already has as many reminders waiting as allowed.
var ErrReminderLimit = errors.New("storage: too many reminders waiting")

// AddReminder saves a text reminder unless its user already has perUser
// reminders waiting, or its chat perChat, when it returns
// ErrReminderLimit. The user's reminders are counted under a lock, so two
// at once cannot both pass the limit.
func (s *Store) AddReminder(ctx context.Context, r Reminder, perUser, perChat int) (Reminder, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('reminders:' || $1::bigint, 0))`, r.UserID); err != nil {
			return err
		}
		var mine, chat int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE user_id = $1), count(*) FILTER (WHERE chat_id = $2)
			FROM reminders WHERE claimed_at IS NULL AND kind = 'text' AND (user_id = $1 OR chat_id = $2)`,
			r.UserID, r.ChatID).Scan(&mine, &chat); err != nil {
			return err
		}
		if mine >= perUser || chat >= perChat {
			return ErrReminderLimit
		}
		return tx.QueryRow(ctx, `
			INSERT INTO reminders (chat_id, user_id, user_name, message_id, kind, text, language, due_at)
			VALUES ($1, $2, $3, $4, 'text', $5, $6, $7) RETURNING id`,
			r.ChatID, r.UserID, r.UserName, r.MessageID, r.Text, r.Language, r.DueAt).Scan(&r.ID)
	})
	if errors.Is(err, ErrReminderLimit) {
		return Reminder{}, err
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("storage: add a reminder: %w", err)
	}
	r.Kind = ReminderText
	return r, nil
}

// SetCardReminder saves the user's card reminder, in place of the one
// waiting if there is one: a player has one at most.
func (s *Store) SetCardReminder(ctx context.Context, r Reminder) (Reminder, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO reminders (chat_id, user_id, user_name, message_id, kind, language, due_at)
		VALUES ($1, $2, $3, $4, 'card', $5, $6)
		ON CONFLICT (user_id) WHERE kind = 'card' AND claimed_at IS NULL DO UPDATE SET
			chat_id = EXCLUDED.chat_id, user_name = EXCLUDED.user_name, message_id = EXCLUDED.message_id,
			language = EXCLUDED.language, due_at = EXCLUDED.due_at
		RETURNING id`,
		r.ChatID, r.UserID, r.UserName, r.MessageID, r.Language, r.DueAt).Scan(&r.ID)
	if err != nil {
		return Reminder{}, fmt.Errorf("storage: set a card reminder: %w", err)
	}
	r.Kind = ReminderCard
	return r, nil
}

// SetCardAutoRemind turns on, for chatID, or off, for 0, the card
// reminder each draw of the user's schedules. Turning it off also drops
// the card reminder waiting.
func (s *Store) SetCardAutoRemind(ctx context.Context, userID int64, name string, chatID int64, language string) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var remindChat *int64
		if chatID != 0 {
			remindChat = &chatID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO card_players (user_id, name, remind_chat_id, remind_language) VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id) DO UPDATE SET remind_chat_id = EXCLUDED.remind_chat_id, remind_language = EXCLUDED.remind_language`,
			userID, name, remindChat, language); err != nil {
			return err
		}
		if chatID != 0 {
			return nil
		}
		_, err := tx.Exec(ctx, `DELETE FROM reminders WHERE user_id = $1 AND kind = 'card' AND claimed_at IS NULL`, userID)
		return err
	})
	if err != nil {
		return fmt.Errorf("storage: set the card reminders: %w", err)
	}
	return nil
}

// CardAutoRemind is the chat the user's draws schedule card reminders to,
// 0 when that is off.
func (s *Store) CardAutoRemind(ctx context.Context, userID int64) (int64, error) {
	var chatID *int64
	err := s.pool.QueryRow(ctx, `SELECT remind_chat_id FROM card_players WHERE user_id = $1`, userID).Scan(&chatID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && chatID == nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("storage: read the card reminders: %w", err)
	}
	return *chatID, nil
}

// WaitingReminders are the user's reminders waiting in a chat, the soonest
// first.
func (s *Store) WaitingReminders(ctx context.Context, userID, chatID int64) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, chat_id, user_id, user_name, message_id, kind, text, language, due_at, attempts
		FROM reminders WHERE user_id = $1 AND chat_id = $2 AND claimed_at IS NULL
		ORDER BY due_at, id`, userID, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: list reminders: %w", err)
	}
	waiting, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Reminder, error) {
		var r Reminder
		err := row.Scan(&r.ID, &r.ChatID, &r.UserID, &r.UserName, &r.MessageID, &r.Kind, &r.Text, &r.Language, &r.DueAt, &r.Attempts)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list reminders: %w", err)
	}
	return waiting, nil
}

// CancelReminder deletes a reminder still waiting, only the user's own in
// that chat; false when there is no such reminder.
func (s *Store) CancelReminder(ctx context.Context, id, userID, chatID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM reminders WHERE id = $1 AND user_id = $2 AND chat_id = $3 AND claimed_at IS NULL`, id, userID, chatID)
	if err != nil {
		return false, fmt.Errorf("storage: cancel a reminder: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ClaimDueReminders claims at most limit reminders due by now, the
// longest due first, and returns them. A row claimed is never claimed
// again, and rows another call is claiming are skipped rather than waited
// for, so two calls at once share the due reminders and each goes out at
// most once.
func (s *Store) ClaimDueReminders(ctx context.Context, now time.Time, limit int) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
		WITH due AS (
			SELECT id FROM reminders WHERE claimed_at IS NULL AND due_at <= $1
			ORDER BY due_at, id LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		UPDATE reminders r SET claimed_at = $1, attempts = r.attempts + 1 FROM due WHERE r.id = due.id
		RETURNING r.id, r.chat_id, r.user_id, r.user_name, r.message_id, r.kind, r.text, r.language, r.due_at, r.attempts,
			(SELECT p.last_draw_at FROM card_players p WHERE p.user_id = r.user_id)`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: claim reminders: %w", err)
	}
	claimed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Reminder, error) {
		var r Reminder
		var last *time.Time
		err := row.Scan(&r.ID, &r.ChatID, &r.UserID, &r.UserName, &r.MessageID, &r.Kind, &r.Text, &r.Language, &r.DueAt, &r.Attempts, &last)
		if last != nil {
			r.LastDraw = *last
		}
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: claim reminders: %w", err)
	}
	return claimed, nil
}

// FinishReminder records what became of a claimed reminder.
func (s *Store) FinishReminder(ctx context.Context, id int64, outcome string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE reminders SET sent_at = now(), outcome = $2 WHERE id = $1`, id, outcome); err != nil {
		return fmt.Errorf("storage: finish a reminder: %w", err)
	}
	return nil
}

// RetryReminder gives a claimed reminder back, due again at, for a later
// call to send: one Telegram refused for now, such as over its rate limit,
// or one the call ran out of time for.
func (s *Store) RetryReminder(ctx context.Context, id int64, at time.Time) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE reminders SET claimed_at = NULL, due_at = $2 WHERE id = $1`, id, at); err != nil {
		return fmt.Errorf("storage: retry a reminder: %w", err)
	}
	return nil
}

// DeleteOldReminders deletes the reminders claimed before before, and
// says how many.
func (s *Store) DeleteOldReminders(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM reminders WHERE claimed_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("storage: delete old reminders: %w", err)
	}
	return tag.RowsAffected(), nil
}
