package storage

import (
	"context"
	"fmt"
	"time"
)

// Exchange is one message a person sent the bot in some chat, and the bot's
// answer to it if it answered.
type Exchange struct {
	ChatID int64
	// ChatTitle is the group's name, empty for a private chat.
	ChatTitle string
	Asked     string
	Answer    string
	At        time.Time
}

// ExchangesElsewhere returns up to limit of the person's most recent
// exchanges with the bot since since, in any chat but chatID, oldest first.
// Only their own messages and the answers to them are read: what anyone
// else said in those chats is not. A business account's chats with its
// customers, kept under the account's owner, are no one's elsewhere: they
// stay apart from both the customer's and the owner's chats with the bot.
func (s *Store) ExchangesElsewhere(ctx context.Context, userID, chatID int64, since time.Time, limit int) ([]Exchange, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT chat_id, title, asked, answer, created_at FROM (
			SELECT c.chat_id, coalesce(g.title, '') AS title, q.content AS asked,
			       coalesce(a.content, '') AS answer, q.created_at, q.id
			FROM messages q
			JOIN conversations c ON c.id = q.conversation_id
			LEFT JOIN groups g ON g.chat_id = c.chat_id
			LEFT JOIN LATERAL (
				SELECT content FROM messages a
				WHERE a.conversation_id = q.conversation_id AND a.role = 'assistant'
				  AND a.reply_to_message_id = q.telegram_message_id
				ORDER BY a.id LIMIT 1
			) a ON q.telegram_message_id IS NOT NULL
			WHERE q.role = 'user' AND c.chat_id <> $2 AND q.created_at >= $3
			  AND (q.author_id = $1 OR c.user_id = $1)
			  AND NOT (c.chat_id > 0 AND c.user_id IS DISTINCT FROM c.chat_id)
			ORDER BY q.id DESC
			LIMIT $4
		) recent
		ORDER BY id`, userID, chatID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: exchanges elsewhere: %w", err)
	}
	defer rows.Close()
	var exchanges []Exchange
	for rows.Next() {
		var e Exchange
		if err := rows.Scan(&e.ChatID, &e.ChatTitle, &e.Asked, &e.Answer, &e.At); err != nil {
			return nil, fmt.Errorf("storage: exchanges elsewhere: %w", err)
		}
		exchanges = append(exchanges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: exchanges elsewhere: %w", err)
	}
	return exchanges, nil
}
