package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// GroupMessage is one message seen in a group.
type GroupMessage struct {
	ChatID int64
	// ChatTitle and ChatUsername update the group's entry in groups. The
	// bot's own messages leave them empty, which keeps what is stored.
	ChatTitle    string
	ChatUsername string
	MessageID    int64
	AuthorID     int64
	AuthorName   string
	Text         string
	SentAt       time.Time
}

// AppendGroupMessage records a group message for the log, and notes the
// group as one the bot is in.
//
// A message already logged in that chat is not logged again: Telegram
// redelivers an update when the bot was slow to answer it, and the second
// delivery must not repeat the line. Redeliveries come within minutes, so
// only the last day is checked, which the (chat_id, created_at) index keeps
// cheap. The bot's own lines have no message ID and are always added.
func (s *Store) AppendGroupMessage(ctx context.Context, m GroupMessage) error {
	_, err := s.pool.Exec(ctx, `
		WITH seen AS (
			INSERT INTO groups (chat_id, title, username) VALUES ($1, $6, nullif($7, ''))
			ON CONFLICT (chat_id) DO UPDATE
			SET title = coalesce(nullif(EXCLUDED.title, ''), groups.title),
			    username = CASE WHEN EXCLUDED.title = '' THEN groups.username ELSE EXCLUDED.username END,
			    last_seen_at = now()
		)
		INSERT INTO group_messages (chat_id, message_id, author_id, author_name, text)
		SELECT $1, $2, $3, $4, $5
		WHERE $2::bigint IS NULL OR NOT EXISTS (
			SELECT 1 FROM group_messages
			WHERE chat_id = $1 AND message_id = $2 AND created_at > now() - interval '1 day')`,
		m.ChatID, nonZero(m.MessageID), nonZero(m.AuthorID), m.AuthorName, m.Text, m.ChatTitle, m.ChatUsername)
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

// AuthorByUsername returns the member of the group chatID known by the
// @username: from the member list, or else the last author under it in the
// day's log ("Ann @ann"); ErrUnknownAuthor when there is none. It is only a lead: the caller must have Telegram confirm
// the account still has that username, since usernames change hands.
func (s *Store) AuthorByUsername(ctx context.Context, chatID int64, username string) (int64, error) {
	username = strings.ToLower(username)
	if !isUsername(username) {
		return 0, ErrUnknownAuthor
	}
	// The member list keeps each member's current username for good; the
	// day's log only for a day, and it is read when the list has nobody.
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT user_id FROM (
			(SELECT user_id, 1 AS source FROM group_members
			 WHERE chat_id = $1 AND lower(username) = $2
			 ORDER BY last_seen_at DESC LIMIT 1)
			UNION ALL
			(SELECT author_id, 2 FROM group_messages
			 WHERE chat_id = $1 AND author_id IS NOT NULL
			   AND lower(author_name) ~ ('(^|\s)@' || $2 || '(\s|$)')
			 ORDER BY id DESC LIMIT 1)
		) found
		ORDER BY source
		LIMIT 1`, chatID, username).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUnknownAuthor
	}
	if err != nil {
		return 0, fmt.Errorf("storage: author by username: %w", err)
	}
	return id, nil
}

// isUsername reports a Telegram username: 4 to 32 letters, digits and
// underscores. Only such text goes into the pattern above.
func isUsername(s string) bool {
	if len(s) < 4 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}
