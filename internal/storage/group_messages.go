package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GroupMessage is one message seen in a group.
type GroupMessage struct {
	ChatID     int64
	MessageID  int64
	AuthorID   int64
	AuthorName string
	Text       string
	SentAt     time.Time
}

// AppendGroupMessage records a group message for the day's log.
func (s *Store) AppendGroupMessage(ctx context.Context, m GroupMessage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO group_messages (chat_id, message_id, author_id, author_name, text)
		VALUES ($1, $2, $3, $4, $5)`,
		m.ChatID, nonZero(m.MessageID), nonZero(m.AuthorID), m.AuthorName, m.Text)
	if err != nil {
		return fmt.Errorf("storage: append group message: %w", err)
	}
	return nil
}

// GroupMessagesSince returns up to limit of a group's newest messages sent
// at or after since, oldest first.
func (s *Store) GroupMessagesSince(ctx context.Context, chatID int64, since time.Time, limit int) ([]GroupMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT chat_id, coalesce(message_id, 0), coalesce(author_id, 0), author_name, text, created_at
		FROM (
			SELECT * FROM group_messages
			WHERE chat_id = $1 AND created_at >= $2
			ORDER BY created_at DESC, id DESC
			LIMIT $3
		) recent
		ORDER BY created_at, id`, chatID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read group messages: %w", err)
	}
	messages, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupMessage, error) {
		var m GroupMessage
		err := row.Scan(&m.ChatID, &m.MessageID, &m.AuthorID, &m.AuthorName, &m.Text, &m.SentAt)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read group messages: %w", err)
	}
	return messages, nil
}

// ClearGroupMessages deletes a group's logged messages.
func (s *Store) ClearGroupMessages(ctx context.Context, chatID int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM group_messages WHERE chat_id = $1`, chatID); err != nil {
		return fmt.Errorf("storage: clear group messages: %w", err)
	}
	return nil
}
