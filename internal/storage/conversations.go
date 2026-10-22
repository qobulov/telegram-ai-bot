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

// SaveTurn stores one complete turn in a single statement, so the history
// never contains half of a tool exchange, and the database, which bills
// every query, is asked once. It returns the row ID of the last message,
// the assistant's answer, for use with SetSentMessageID.
//
// The update ID is recorded on the first message. Saving the same update
// twice returns ErrDuplicateUpdate and changes nothing.
func (s *Store) SaveTurn(ctx context.Context, turn Turn) (int64, error) {
	if len(turn.Messages) == 0 {
		return 0, errors.New("storage: a turn needs at least one message")
	}
	rows, err := turnRows(turn)
	if err != nil {
		return 0, fmt.Errorf("storage: save turn: %w", err)
	}
	var answerID int64
	err = s.pool.QueryRow(ctx, saveTurnSQL(turn.Scope),
		turn.Scope.ChatID, nonZero(turn.Scope.UserID), nonZero(authorOf(turn)),
		rows.role, rows.content, rows.toolCalls, rows.toolCallID,
		rows.update, rows.message, rows.replyTo, rows.authorID, rows.authorName).Scan(&answerID)
	if isDuplicateUpdate(err) {
		return 0, ErrDuplicateUpdate
	}
	if err != nil {
		return 0, fmt.Errorf("storage: save turn: %w", err)
	}
	return answerID, nil
}

// saveTurnSQL makes sure the author and the conversation exist, marks both
// as active now, and appends the turn's messages in order, returning the
// last one's ID. The two kinds of conversation are kept unique by different
// indexes, so the conflict target differs.
func saveTurnSQL(scope Scope) string {
	conflict := `(chat_id, user_id)`
	if scope.Shared() {
		conflict = `(chat_id) WHERE user_id IS NULL`
	}
	return `
		WITH author AS (
			INSERT INTO users (telegram_id) SELECT $3 WHERE $3::bigint IS NOT NULL
			ON CONFLICT (telegram_id) DO UPDATE SET last_seen_at = now()
		), conversation AS (
			INSERT INTO conversations (chat_id, user_id) VALUES ($1, $2)
			ON CONFLICT ` + conflict + ` DO UPDATE SET updated_at = now()
			RETURNING id
		), saved AS (
			INSERT INTO messages (conversation_id, role, content, tool_calls, tool_call_id,
			                      update_id, telegram_message_id, reply_to_message_id,
			                      author_id, author_name)
			SELECT conversation.id, m.role, m.content, m.tool_calls::jsonb, m.tool_call_id,
			       m.update_id, m.message_id, m.reply_to, m.author_id, m.author_name
			FROM conversation,
			     unnest($4::text[], $5::text[], $6::text[], $7::text[], $8::bigint[], $9::bigint[],
			            $10::bigint[], $11::bigint[], $12::text[])
			     WITH ORDINALITY AS m(role, content, tool_calls, tool_call_id, update_id, message_id,
			                          reply_to, author_id, author_name, ord)
			ORDER BY m.ord
			RETURNING id
		)
		SELECT max(id) FROM saved`
}

// authorOf is the user who wrote the turn's message: the scope's user in a
// private chat, the named author in a group.
func authorOf(turn Turn) int64 {
	if turn.Scope.Shared() {
		return turn.AuthorID
	}
	return turn.Scope.UserID
}

// messageRows are a turn's messages as columns, one array each, for unnest.
type messageRows struct {
	role, content                     []string
	toolCalls, toolCallID, authorName []*string
	update, message, replyTo          []*int64
	authorID                          []*int64
}

func turnRows(turn Turn) (messageRows, error) {
	var rows messageRows
	for i, msg := range turn.Messages {
		toolCalls, err := encodeToolCalls(msg.ToolCalls)
		if err != nil {
			return messageRows{}, err
		}
		ids := telegramIDs(turn, i)
		rows.role = append(rows.role, msg.Role)
		rows.content = append(rows.content, msg.Content)
		rows.toolCalls = append(rows.toolCalls, textOrNil(toolCalls))
		rows.toolCallID = append(rows.toolCallID, textOrNil(nullable(msg.ToolCallID)))
		rows.update = append(rows.update, numberOrNil(ids.update))
		rows.message = append(rows.message, numberOrNil(ids.message))
		rows.replyTo = append(rows.replyTo, numberOrNil(ids.replyTo))
		rows.authorID = append(rows.authorID, numberOrNil(ids.authorID))
		rows.authorName = append(rows.authorName, textOrNil(ids.authorName))
	}
	return rows, nil
}

// textOrNil and numberOrNil turn the values nullable and nonZero give, nil
// or a value, into what an array of a nullable column takes.
func textOrNil(value any) *string {
	if text, ok := value.(string); ok {
		return &text
	}
	return nil
}

func numberOrNil(value any) *int64 {
	if n, ok := value.(int64); ok {
		return &n
	}
	return nil
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

// ErrUnknownAuthor means it is not known whom a message of the bot's
// answered.
var ErrUnknownAuthor = errors.New("storage: unknown author")

// Author is who wrote a message in a group that the bot answered.
type Author struct {
	ID int64
	// Name is the name they were shown to the model under.
	Name string
	// MessageID is their message.
	MessageID int64
}

// AnsweredAuthor returns the author of the message that the bot's message
// botMessageID, sent in the group chatID, answered: whom an owner means by
// "don't answer him" when replying to the bot's answer. It returns
// ErrUnknownAuthor when the bot's message is not an answer it stored.
func (s *Store) AnsweredAuthor(ctx context.Context, chatID, botMessageID int64) (Author, error) {
	var a Author
	err := s.pool.QueryRow(ctx, `
		SELECT q.author_id, coalesce(q.author_name, ''), q.telegram_message_id
		FROM messages a
		JOIN conversations c ON c.id = a.conversation_id
		JOIN messages q ON q.conversation_id = a.conversation_id
		               AND q.role = 'user' AND q.telegram_message_id = a.reply_to_message_id
		WHERE c.chat_id = $1 AND c.user_id IS NULL
		  AND a.role = 'assistant' AND a.telegram_message_id = $2 AND q.author_id IS NOT NULL
		ORDER BY a.id DESC
		LIMIT 1`, chatID, botMessageID).Scan(&a.ID, &a.Name, &a.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Author{}, ErrUnknownAuthor
	}
	if err != nil {
		return Author{}, fmt.Errorf("storage: find answered author: %w", err)
	}
	return a, nil
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
		if _, err = tx.Exec(ctx, `DELETE FROM group_members WHERE status IN ('left', 'kicked') AND last_seen_at < $1`, time.Now().Add(-forgetLeftAfter)); err != nil {
			return err
		}
		// Owner notices count per day, so two days' are more than enough.
		if _, err = tx.Exec(ctx, `DELETE FROM owner_notices WHERE created_at < now() - interval '2 days'`); err != nil {
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
