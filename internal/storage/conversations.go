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

// Scope identifies one conversation: one user in one chat. In a private chat
// the chat ID equals the user ID; in a group each member has their own
// scope. Both IDs must come from the Telegram update, never from message
// text, because the scope is what keeps conversations apart.
type Scope struct {
	ChatID int64
	UserID int64
}

// Turn is one complete exchange to save: the user's message followed by
// everything the assistant and tools produced, ending with the answer.
type Turn struct {
	Scope    Scope
	UpdateID int64
	// MessageID is the incoming Telegram message. Zero is stored as NULL.
	MessageID int64
	// ReplyToMessageID is the message the incoming one replied to, if any.
	ReplyToMessageID int64
	Messages         []ai.Message
}

// History returns the most recent messages of one conversation, oldest first.
func (s *Store) History(ctx context.Context, scope Scope, limit int) ([]ai.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT role, content, tool_calls, tool_call_id
		FROM (
			SELECT m.id, m.role, m.content, m.tool_calls, m.tool_call_id
			FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE c.chat_id = $1 AND c.user_id = $2
			ORDER BY m.id DESC
			LIMIT $3
		) recent
		ORDER BY id`, scope.ChatID, scope.UserID, limit)
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

// SaveTurn stores one complete turn in a single transaction, so the history
// never contains half of a tool exchange. It returns the row ID of the last
// message, the assistant's answer, for use with SetSentMessageID.
//
// The update ID is recorded on the first message. Saving the same update
// twice returns ErrDuplicateUpdate and changes nothing.
func (s *Store) SaveTurn(ctx context.Context, turn Turn) (int64, error) {
	if len(turn.Messages) == 0 {
		return 0, errors.New("storage: a turn needs at least one message")
	}
	var answerID int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversationID, err := touchConversation(ctx, tx, turn.Scope)
		if err != nil {
			return err
		}
		answerID, err = insertMessages(ctx, tx, conversationID, turn)
		return err
	})
	if isDuplicateUpdate(err) {
		return 0, ErrDuplicateUpdate
	}
	if err != nil {
		return 0, fmt.Errorf("storage: save turn: %w", err)
	}
	return answerID, nil
}

// touchConversation makes sure the user and the conversation exist, marks
// both as active now, and returns the conversation ID.
func touchConversation(ctx context.Context, tx pgx.Tx, scope Scope) (int64, error) {
	_, err := tx.Exec(ctx, `
		INSERT INTO users (telegram_id) VALUES ($1)
		ON CONFLICT (telegram_id) DO UPDATE SET last_seen_at = now()`, scope.UserID)
	if err != nil {
		return 0, err
	}
	var conversationID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO conversations (chat_id, user_id) VALUES ($1, $2)
		ON CONFLICT (chat_id, user_id) DO UPDATE SET updated_at = now()
		RETURNING id`, scope.ChatID, scope.UserID).Scan(&conversationID)
	return conversationID, err
}

// insertMessages writes the turn's messages in order and returns the row ID
// of the last one.
func insertMessages(ctx context.Context, tx pgx.Tx, conversationID int64, turn Turn) (int64, error) {
	var lastID int64
	for i, msg := range turn.Messages {
		toolCalls, err := encodeToolCalls(msg.ToolCalls)
		if err != nil {
			return 0, err
		}
		ids := telegramIDs(turn, i)
		err = tx.QueryRow(ctx, `
			INSERT INTO messages (conversation_id, role, content, tool_calls, tool_call_id,
			                      update_id, telegram_message_id, reply_to_message_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id`,
			conversationID, msg.Role, msg.Content, toolCalls, nullable(msg.ToolCallID),
			ids.update, ids.message, ids.replyTo).Scan(&lastID)
		if err != nil {
			return 0, err
		}
	}
	return lastID, nil
}

// rowIDs are the Telegram identifiers stored with one message row. A nil
// value is stored as NULL.
type rowIDs struct {
	update, message, replyTo any
}

// telegramIDs decides which identifiers belong on the message at index. The
// first message is the user's: it carries the update, the incoming message
// and what that message replied to. The last is the answer, which replies to
// the incoming message; its own message ID is set after sending. Tool
// exchanges in between correspond to no Telegram message.
func telegramIDs(turn Turn, index int) rowIDs {
	switch index {
	case 0:
		return rowIDs{update: turn.UpdateID, message: nonZero(turn.MessageID), replyTo: nonZero(turn.ReplyToMessageID)}
	case len(turn.Messages) - 1:
		return rowIDs{replyTo: nonZero(turn.MessageID)}
	default:
		return rowIDs{}
	}
}

func nonZero(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// SetSentMessageID records the Telegram message ID of an answer once it has
// been sent. answerID is the value SaveTurn returned.
func (s *Store) SetSentMessageID(ctx context.Context, answerID, telegramMessageID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE messages SET telegram_message_id = $2 WHERE id = $1`, answerID, telegramMessageID)
	if err != nil {
		return fmt.Errorf("storage: record sent message: %w", err)
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

// Reset deletes one conversation. Its messages are removed with it by the
// foreign key's ON DELETE CASCADE, within the same statement, so the deletion
// is atomic. Other conversations of the same user, in other chats, are kept.
func (s *Store) Reset(ctx context.Context, scope Scope) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM conversations WHERE chat_id = $1 AND user_id = $2`, scope.ChatID, scope.UserID)
	if err != nil {
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
