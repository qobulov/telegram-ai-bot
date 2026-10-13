package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"telegram-ai-bot/internal/ai"
)

// ErrDuplicateUpdate means the Telegram update was already saved by an
// earlier turn.
var ErrDuplicateUpdate = errors.New("storage: update already saved")

const (
	uniqueViolation    = "23505"
	updateIDConstraint = "messages_update_id_key"
)

// History returns the user's most recent messages, oldest first.
func (s *Store) History(ctx context.Context, userID int64, limit int) ([]ai.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT role, content, tool_calls, tool_call_id
		FROM (
			SELECT m.id, m.role, m.content, m.tool_calls, m.tool_call_id
			FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE c.user_id = $1
			ORDER BY m.id DESC
			LIMIT $2
		) recent
		ORDER BY id`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: load history: %w", err)
	}
	history, err := pgx.CollectRows(rows, scanMessage)
	if err != nil {
		return nil, fmt.Errorf("storage: load history: %w", err)
	}
	return history, nil
}

func scanMessage(row pgx.CollectableRow) (ai.Message, error) {
	var (
		msg        ai.Message
		toolCalls  []byte
		toolCallID *string
	)
	if err := row.Scan(&msg.Role, &msg.Content, &toolCalls, &toolCallID); err != nil {
		return ai.Message{}, err
	}
	if toolCallID != nil {
		msg.ToolCallID = *toolCallID
	}
	if toolCalls == nil {
		return msg, nil
	}
	if err := json.Unmarshal(toolCalls, &msg.ToolCalls); err != nil {
		return ai.Message{}, fmt.Errorf("decode stored tool calls: %w", err)
	}
	return msg, nil
}

// SaveTurn stores one complete turn: the user's message followed by
// everything the assistant and tools produced. The whole turn is written in a
// single transaction, so the history never contains half of a tool exchange.
//
// updateID is recorded on the first message. Saving the same update twice
// returns ErrDuplicateUpdate and changes nothing.
func (s *Store) SaveTurn(ctx context.Context, userID, updateID int64, msgs []ai.Message) error {
	if len(msgs) == 0 {
		return errors.New("storage: a turn needs at least one message")
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversationID, err := touchConversation(ctx, tx, userID)
		if err != nil {
			return err
		}
		return insertMessages(ctx, tx, conversationID, updateID, msgs)
	})
	if isDuplicateUpdate(err) {
		return ErrDuplicateUpdate
	}
	if err != nil {
		return fmt.Errorf("storage: save turn: %w", err)
	}
	return nil
}

// touchConversation makes sure the user and their conversation exist, marks
// both as active now, and returns the conversation ID.
func touchConversation(ctx context.Context, tx pgx.Tx, userID int64) (int64, error) {
	_, err := tx.Exec(ctx, `
		INSERT INTO users (telegram_id) VALUES ($1)
		ON CONFLICT (telegram_id) DO UPDATE SET last_seen_at = now()`, userID)
	if err != nil {
		return 0, err
	}
	var conversationID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO conversations (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO UPDATE SET updated_at = now()
		RETURNING id`, userID).Scan(&conversationID)
	return conversationID, err
}

func insertMessages(ctx context.Context, tx pgx.Tx, conversationID, updateID int64, msgs []ai.Message) error {
	for i, msg := range msgs {
		toolCalls, err := encodeToolCalls(msg.ToolCalls)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO messages (conversation_id, role, content, tool_calls, tool_call_id, update_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			conversationID, msg.Role, msg.Content, toolCalls, nullable(msg.ToolCallID), firstOnly(i, updateID))
		if err != nil {
			return err
		}
	}
	return nil
}

// encodeToolCalls returns nil, which is stored as NULL, when there are none.
func encodeToolCalls(calls []ai.ToolCall) (any, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(calls)
	if err != nil {
		return nil, fmt.Errorf("encode tool calls: %w", err)
	}
	return string(encoded), nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// firstOnly attaches the update ID to the first message of a turn only.
func firstOnly(index int, updateID int64) any {
	if index != 0 {
		return nil
	}
	return updateID
}

func isDuplicateUpdate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == updateIDConstraint
}

// UpdateProcessed reports whether a turn for this Telegram update was saved.
func (s *Store) UpdateProcessed(ctx context.Context, updateID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM messages WHERE update_id = $1)`, updateID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("storage: check update: %w", err)
	}
	return exists, nil
}

// Reset deletes the user's conversation. The messages are removed with it by
// the foreign key's ON DELETE CASCADE, within the same statement, so the
// deletion is atomic.
func (s *Store) Reset(ctx context.Context, userID int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM conversations WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("storage: reset conversation: %w", err)
	}
	return nil
}

// DeleteIdle removes conversations with no activity for longer than
// olderThan and returns how many were deleted.
func (s *Store) DeleteIdle(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.pool.Exec(ctx, `DELETE FROM conversations WHERE updated_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("storage: delete idle conversations: %w", err)
	}
	return tag.RowsAffected(), nil
}
