package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// WordCount is how much one member wrote in a group over a period.
type WordCount struct {
	UserID   int64
	Name     string
	Words    int
	Messages int
}

// AddMemberWords counts one message of a member, of words words, on day,
// keeping the member's latest name.
func (s *Store) AddMemberWords(ctx context.Context, chatID, userID int64, name string, day time.Time, words int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO member_words (chat_id, user_id, day, name, words, messages) VALUES ($1, $2, $3, $4, $5, 1)
		ON CONFLICT (chat_id, day, user_id) DO UPDATE SET
			name = EXCLUDED.name, words = member_words.words + EXCLUDED.words, messages = member_words.messages + 1`,
		chatID, userID, day.Format(time.DateOnly), name, words)
	if err != nil {
		return fmt.Errorf("storage: add member words: %w", err)
	}
	return nil
}

// WordTop lists the members of a group who wrote the most words from day
// since on, most first, at most limit of them; with user not 0, that
// member's alone.
func (s *Store) WordTop(ctx context.Context, chatID int64, since time.Time, user int64, limit int) ([]WordCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, (array_agg(name ORDER BY day DESC))[1], sum(words)::int, sum(messages)::int FROM member_words
		WHERE chat_id = $1 AND day >= $2 AND ($3 = 0 OR user_id = $3)
		GROUP BY user_id ORDER BY sum(words) DESC, sum(messages) DESC, user_id LIMIT $4`,
		chatID, since.Format(time.DateOnly), user, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: word top: %w", err)
	}
	top, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (WordCount, error) {
		var c WordCount
		err := row.Scan(&c.UserID, &c.Name, &c.Words, &c.Messages)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: word top: %w", err)
	}
	return top, nil
}

// WordPlace is a member's place among a group's writers from day since on,
// counted as WordTop orders them, with their count; place 0 when they wrote
// nothing then.
func (s *Store) WordPlace(ctx context.Context, chatID int64, since time.Time, user int64) (int, WordCount, error) {
	var place int
	c := WordCount{UserID: user}
	err := s.pool.QueryRow(ctx, `
		SELECT place, name, words, messages FROM (
			SELECT user_id, (array_agg(name ORDER BY day DESC))[1] AS name, sum(words)::int AS words, sum(messages)::int AS messages,
				row_number() OVER (ORDER BY sum(words) DESC, sum(messages) DESC, user_id)::int AS place
			FROM member_words WHERE chat_id = $1 AND day >= $2 GROUP BY user_id
		) t WHERE user_id = $3`,
		chatID, since.Format(time.DateOnly), user).Scan(&place, &c.Name, &c.Words, &c.Messages)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, c, nil
	}
	if err != nil {
		return 0, c, fmt.Errorf("storage: word place: %w", err)
	}
	return place, c, nil
}
