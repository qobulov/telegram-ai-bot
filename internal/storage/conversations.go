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

// Scope identifies one conversation.
//
//   - Private chat: ChatID and UserID are both the user's ID.
//   - Group: UserID is zero. The group has one conversation shared by all
//     its members.
//
// Both IDs must come from the Telegram update, never from message text,
// because the scope is what keeps conversations apart.
type Scope struct {
	ChatID int64
	UserID int64
}

// Shared reports whether the scope is a group's shared conversation.
func (s Scope) Shared() bool {
	return s.UserID == 0
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
	// AuthorID and AuthorName say who wrote the user message. They matter in
	// a shared conversation, where the scope does not name the author.
	AuthorID   int64
	AuthorName string
	Messages   []ai.Message
}

// History returns the most recent messages of one conversation, oldest first.
func (s *Store) History(ctx context.Context, scope Scope, limit int) ([]ai.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT role, content, tool_calls, tool_call_id
		FROM (
			SELECT m.id, m.role, m.content, m.tool_calls, m.tool_call_id
			FROM messages m
			JOIN conversations c ON c.id = m.conversation_id
			WHERE c.chat_id = $1 AND c.user_id IS NOT DISTINCT FROM $2
			ORDER BY m.id DESC
			LIMIT $3
		) recent
		ORDER BY id`, scope.ChatID, nonZero(scope.UserID), limit)
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
		conversationID, err := touchConversation(ctx, tx, turn)
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

// touchConversation makes sure the author and the conversation exist, marks
// both as active now, and returns the conversation ID.
func touchConversation(ctx context.Context, tx pgx.Tx, turn Turn) (int64, error) {
	if err := touchUser(ctx, tx, authorOf(turn)); err != nil {
		return 0, err
	}
	var conversationID int64
	err := tx.QueryRow(ctx, upsertConversation(turn.Scope), turn.Scope.ChatID, nonZero(turn.Scope.UserID)).Scan(&conversationID)
	return conversationID, err
}

// authorOf is the user who wrote the turn's message: the scope's user in a
// private chat, the named author in a group.
func authorOf(turn Turn) int64 {
	if turn.Scope.Shared() {
		return turn.AuthorID
	}
	return turn.Scope.UserID
}

func touchUser(ctx context.Context, tx pgx.Tx, userID int64) error {
	if userID == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO users (telegram_id) VALUES ($1)
		ON CONFLICT (telegram_id) DO UPDATE SET last_seen_at = now()`, userID)
	return err
}

// upsertConversation returns the statement for the scope's kind. The two
// kinds are kept unique by different indexes, so the conflict target differs.
func upsertConversation(scope Scope) string {
	if scope.Shared() {
		return `
			INSERT INTO conversations (chat_id, user_id) VALUES ($1, $2)
			ON CONFLICT (chat_id) WHERE user_id IS NULL DO UPDATE SET updated_at = now()
			RETURNING id`
	}
	return `
		INSERT INTO conversations (chat_id, user_id) VALUES ($1, $2)
		ON CONFLICT (chat_id, user_id) DO UPDATE SET updated_at = now()
		RETURNING id`
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
			                      update_id, telegram_message_id, reply_to_message_id,
			                      author_id, author_name)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id`,
			conversationID, msg.Role, msg.Content, toolCalls, nullable(msg.ToolCallID),
			ids.update, ids.message, ids.replyTo, ids.authorID, ids.authorName).Scan(&lastID)
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
	authorID, authorName     any
}

// telegramIDs decides which identifiers belong on the message at index. The
// first message is the user's: it carries the update, the incoming message
// and what that message replied to. The last is the answer, which replies to
// the incoming message; its own message ID is set after sending. Tool
// exchanges in between correspond to no Telegram message.
func telegramIDs(turn Turn, index int) rowIDs {
	switch index {
	case 0:
		return rowIDs{
			update:     turn.UpdateID,
			message:    nonZero(turn.MessageID),
			replyTo:    nonZero(turn.ReplyToMessageID),
			authorID:   nonZero(turn.AuthorID),
			authorName: nullable(turn.AuthorName),
		}
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
	_, err := s.pool.Exec(ctx,
		`DELETE FROM conversations WHERE chat_id = $1 AND user_id IS NOT DISTINCT FROM $2`,
		scope.ChatID, nonZero(scope.UserID))
	if err != nil {
		return fmt.Errorf("storage: reset conversation: %w", err)
	}
	return nil
}

// Expired counts what Expire removed.
type Expired struct {
	Messages      int64 `json:"messages"`
	Conversations int64 `json:"conversations"`
	GroupMessages int64 `json:"group_messages"`
}

// Expire deletes messages older than maxAge, then the conversations that have
// no messages left. A turn's messages are written in one transaction and
// share its timestamp, so a turn is always kept or removed whole. Settings
// such as a group's mute are not history and are kept; standing rules are
// kept until their own expiry, whatever maxAge is.
func (s *Store) Expire(ctx context.Context, maxAge time.Duration) (Expired, error) {
	cutoff := time.Now().Add(-maxAge)
	var expired Expired
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM messages WHERE created_at < $1`, cutoff)
		if err != nil {
			return err
		}
		expired.Messages = tag.RowsAffected()
		tag, err = tx.Exec(ctx, `
			DELETE FROM conversations c
			WHERE NOT EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id)`)
		if err != nil {
			return err
		}
		expired.Conversations = tag.RowsAffected()
		tag, err = tx.Exec(ctx, `DELETE FROM group_messages WHERE created_at < $1`, cutoff)
		if err != nil {
			return err
		}
		expired.GroupMessages = tag.RowsAffected()
		// Limits are counted per day, so older records are no longer needed.
		if _, err = tx.Exec(ctx, `DELETE FROM image_generations WHERE created_at < $1`, cutoff); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM bot_replies WHERE created_at < $1`, cutoff); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM group_replies WHERE created_at < $1`, cutoff); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM private_messages WHERE created_at < $1`, cutoff); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM group_rules WHERE expires_at < now()`); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM broadcasts WHERE created_at < $1`, cutoff)
		return err
	})
	if err != nil {
		return Expired{}, fmt.Errorf("storage: expire history: %w", err)
	}
	return expired, nil
}
