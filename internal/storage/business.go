package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrUnknownBusinessConnection means no connection with that ID, or none
// of the owner's that can be used, is recorded.
var ErrUnknownBusinessConnection = errors.New("storage: unknown business connection")

// ErrNoChecklist means no checklist was sent into that chat.
var ErrNoChecklist = errors.New("storage: no checklist sent there")

// BusinessConnection is a business account that connected the bot as its
// chatbot: whose account it is, what the bot may do there, and whether it
// answers customers there.
type BusinessConnection struct {
	ID         string
	UserID     int64
	UserChatID int64
	FirstName  string
	Username   string

	CanReply         bool
	CanManageStories bool
	Enabled          bool
	// Refused marks the connection of someone who is none of the bot's
	// owners, which is never used.
	Refused bool
	// AutoReply is whether the bot answers customers there. Recording a
	// connection keeps it, and a new connection of the same owner takes
	// that owner's last choice; an owner's first is off until they say
	// /business on, since she would speak for them to people they know.
	AutoReply bool
	UpdatedAt time.Time
}

// RecordBusinessConnection keeps a connection as Telegram reported it and
// returns it as stored, with its AutoReply. The owner of a connection that
// is not refused is recorded as a user, since their conversations with
// customers are kept under their ID.
func (s *Store) RecordBusinessConnection(ctx context.Context, c BusinessConnection) (BusinessConnection, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if !c.Refused {
			if _, err := tx.Exec(ctx, `INSERT INTO users (telegram_id) VALUES ($1) ON CONFLICT DO NOTHING`, c.UserID); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `
			INSERT INTO business_connections
			       (id, user_id, user_chat_id, first_name, username, can_reply, can_manage_stories, enabled, refused, auto_reply)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
			        coalesce((SELECT auto_reply FROM business_connections WHERE user_id = $2 ORDER BY updated_at DESC LIMIT 1), false))
			ON CONFLICT (id) DO UPDATE SET
			       user_id = EXCLUDED.user_id, user_chat_id = EXCLUDED.user_chat_id,
			       first_name = EXCLUDED.first_name, username = EXCLUDED.username,
			       can_reply = EXCLUDED.can_reply, can_manage_stories = EXCLUDED.can_manage_stories,
			       enabled = EXCLUDED.enabled, refused = EXCLUDED.refused, updated_at = now()
			RETURNING auto_reply, updated_at`,
			c.ID, c.UserID, c.UserChatID, c.FirstName, c.Username, c.CanReply, c.CanManageStories, c.Enabled, c.Refused,
		).Scan(&c.AutoReply, &c.UpdatedAt)
	})
	if err != nil {
		return BusinessConnection{}, fmt.Errorf("storage: record business connection: %w", err)
	}
	return c, nil
}

const businessConnectionColumns = `id, user_id, user_chat_id, first_name, username, can_reply, can_manage_stories, enabled, refused, auto_reply, updated_at`

func scanBusinessConnection(row pgx.Row) (BusinessConnection, error) {
	var c BusinessConnection
	err := row.Scan(&c.ID, &c.UserID, &c.UserChatID, &c.FirstName, &c.Username, &c.CanReply, &c.CanManageStories, &c.Enabled, &c.Refused, &c.AutoReply, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BusinessConnection{}, ErrUnknownBusinessConnection
	}
	return c, err
}

// BusinessConnection is the connection with that ID, or
// ErrUnknownBusinessConnection.
func (s *Store) BusinessConnection(ctx context.Context, id string) (BusinessConnection, error) {
	c, err := scanBusinessConnection(s.pool.QueryRow(ctx, `SELECT `+businessConnectionColumns+` FROM business_connections WHERE id = $1`, id))
	if err != nil && !errors.Is(err, ErrUnknownBusinessConnection) {
		return BusinessConnection{}, fmt.Errorf("storage: business connection: %w", err)
	}
	return c, err
}

// OwnerBusinessConnection is the owner's latest connection that is enabled
// and not refused, or ErrUnknownBusinessConnection.
func (s *Store) OwnerBusinessConnection(ctx context.Context, userID int64) (BusinessConnection, error) {
	c, err := scanBusinessConnection(s.pool.QueryRow(ctx, `
		SELECT `+businessConnectionColumns+` FROM business_connections
		WHERE user_id = $1 AND enabled AND NOT refused
		ORDER BY updated_at DESC LIMIT 1`, userID))
	if err != nil && !errors.Is(err, ErrUnknownBusinessConnection) {
		return BusinessConnection{}, fmt.Errorf("storage: owner's business connection: %w", err)
	}
	return c, err
}

// SetBusinessAutoReply turns answering customers on or off in all the
// owner's connections, and returns how many there are.
func (s *Store) SetBusinessAutoReply(ctx context.Context, userID int64, on bool) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE business_connections SET auto_reply = $2 WHERE user_id = $1 AND NOT refused`, userID, on)
	if err != nil {
		return 0, fmt.Errorf("storage: set business auto-reply: %w", err)
	}
	return tag.RowsAffected(), nil
}

// BusinessChat is a chat of a business account the bot saw a message in:
// the customer, and when the account's owner last wrote there themselves.
type BusinessChat struct {
	ConnectionID  string
	ChatID        int64
	FirstName     string
	Username      string
	LastMessageAt time.Time
	// OwnerWroteAt is zero when the owner has not written there.
	OwnerWroteAt time.Time
}

// SeeBusinessChat notes a message in a business chat, by the account's
// owner when byOwner, and returns when the owner last wrote there, this
// message included; zero when they never did. A customer's name is kept as
// the latest message gave it; an owner's message leaves it.
func (s *Store) SeeBusinessChat(ctx context.Context, chat BusinessChat, byOwner bool) (time.Time, error) {
	var ownerWrote *time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO business_chats (connection_id, chat_id, first_name, username, owner_wrote_at)
		VALUES ($1, $2, $3, $4, CASE WHEN $5 THEN now() END)
		ON CONFLICT (connection_id, chat_id) DO UPDATE SET
		       first_name = CASE WHEN $5 AND EXCLUDED.first_name = '' THEN business_chats.first_name ELSE EXCLUDED.first_name END,
		       username = CASE WHEN $5 AND EXCLUDED.username = '' THEN business_chats.username ELSE EXCLUDED.username END,
		       last_message_at = now(),
		       owner_wrote_at = CASE WHEN $5 THEN now() ELSE business_chats.owner_wrote_at END
		RETURNING owner_wrote_at`,
		chat.ConnectionID, chat.ChatID, chat.FirstName, chat.Username, byOwner).Scan(&ownerWrote)
	if err != nil {
		return time.Time{}, fmt.Errorf("storage: see business chat: %w", err)
	}
	if ownerWrote == nil {
		return time.Time{}, nil
	}
	return *ownerWrote, nil
}

// BusinessChats are the connection's chats, the latest written in first,
// at most limit.
func (s *Store) BusinessChats(ctx context.Context, connectionID string, limit int) ([]BusinessChat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT connection_id, chat_id, first_name, username, last_message_at, owner_wrote_at
		FROM business_chats WHERE connection_id = $1
		ORDER BY last_message_at DESC LIMIT $2`, connectionID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: business chats: %w", err)
	}
	chats, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (BusinessChat, error) {
		var c BusinessChat
		var ownerWrote *time.Time
		err := row.Scan(&c.ConnectionID, &c.ChatID, &c.FirstName, &c.Username, &c.LastMessageAt, &ownerWrote)
		if ownerWrote != nil {
			c.OwnerWroteAt = *ownerWrote
		}
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: business chats: %w", err)
	}
	return chats, nil
}

// BusinessQuota asks for one answer to a customer in a business chat on
// the day that began at Since, of Limit a day.
type BusinessQuota struct {
	ConnectionID string
	ChatID       int64
	Since        time.Time
	Limit        int
}

// ReserveBusinessReply takes one of the customer's answers for the day, and
// returns its ID for ReleaseBusinessReply, when fewer than Limit were
// taken. A lock on the chat makes the count and the insert one step.
func (s *Store) ReserveBusinessReply(ctx context.Context, q BusinessQuota) (id int64, allowed bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2::bigint, 0))`, q.ConnectionID, q.ChatID); err != nil {
			return err
		}
		var used int
		err := tx.QueryRow(ctx, `
			SELECT count(*) FROM business_replies
			WHERE connection_id = $1 AND chat_id = $2 AND created_at >= $3`,
			q.ConnectionID, q.ChatID, q.Since).Scan(&used)
		if err != nil || used >= q.Limit {
			return err
		}
		allowed = true
		return tx.QueryRow(ctx, `INSERT INTO business_replies (connection_id, chat_id) VALUES ($1, $2) RETURNING id`, q.ConnectionID, q.ChatID).Scan(&id)
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: reserve business reply: %w", err)
	}
	return id, allowed, nil
}

// ReleaseBusinessReply gives back an answer whose turn answered nothing.
func (s *Store) ReleaseBusinessReply(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM business_replies WHERE id = $1`, id); err != nil {
		return fmt.Errorf("storage: release business reply: %w", err)
	}
	return nil
}

// BusinessChecklist is a checklist the bot sent into a business chat.
type BusinessChecklist struct {
	ConnectionID string
	ChatID       int64
	MessageID    int64
	Title        string
	Tasks        []string
	SentAt       time.Time
}

// SaveBusinessChecklist keeps a checklist sent, or what one was changed to.
func (s *Store) SaveBusinessChecklist(ctx context.Context, c BusinessChecklist) error {
	tasks, err := json.Marshal(c.Tasks)
	if err != nil {
		return fmt.Errorf("storage: save business checklist: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO business_checklists (connection_id, chat_id, message_id, title, tasks)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (connection_id, chat_id, message_id) DO UPDATE SET title = EXCLUDED.title, tasks = EXCLUDED.tasks`,
		c.ConnectionID, c.ChatID, c.MessageID, c.Title, tasks)
	if err != nil {
		return fmt.Errorf("storage: save business checklist: %w", err)
	}
	return nil
}

// LastBusinessChecklist is the latest checklist sent into the chat, or
// ErrNoChecklist.
func (s *Store) LastBusinessChecklist(ctx context.Context, connectionID string, chatID int64) (BusinessChecklist, error) {
	c := BusinessChecklist{ConnectionID: connectionID, ChatID: chatID}
	var tasks []byte
	err := s.pool.QueryRow(ctx, `
		SELECT message_id, title, tasks, sent_at FROM business_checklists
		WHERE connection_id = $1 AND chat_id = $2
		ORDER BY sent_at DESC, message_id DESC LIMIT 1`, connectionID, chatID).Scan(&c.MessageID, &c.Title, &tasks, &c.SentAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return BusinessChecklist{}, ErrNoChecklist
	case err != nil:
		return BusinessChecklist{}, fmt.Errorf("storage: last business checklist: %w", err)
	}
	if err := json.Unmarshal(tasks, &c.Tasks); err != nil {
		return BusinessChecklist{}, fmt.Errorf("storage: last business checklist: %w", err)
	}
	return c, nil
}
