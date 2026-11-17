package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// warnsLast is how long a member's warnings stay: a month after the last,
// they lapse and the next is the first again.
const warnsLast = 30 * 24 * time.Hour

// MemberWarn is a member's warnings in a group, with the last's reason.
type MemberWarn struct {
	UserID int64
	Name   string
	Count  int
	Reason string
	At     time.Time
}

// AddWarn gives the member one more warning in the chat, for reason, and
// returns how many they have; warnings a month old lapse first.
func (s *Store) AddWarn(ctx context.Context, chatID, userID int64, reason string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO member_warns (chat_id, user_id, count, reason) VALUES ($1, $2, 1, $3)
		ON CONFLICT (chat_id, user_id) DO UPDATE SET
			count = CASE WHEN member_warns.updated_at < now() - $4::interval THEN 1 ELSE member_warns.count + 1 END,
			reason = EXCLUDED.reason, updated_at = now()
		RETURNING count`, chatID, userID, reason, warnsLast.String()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: warn a member: %w", err)
	}
	return count, nil
}

// Warns are the chat's members with warnings that have not lapsed, the most
// first; with userID not 0, that member's alone.
func (s *Store) Warns(ctx context.Context, chatID, userID int64, limit int) ([]MemberWarn, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.user_id, coalesce(g.first_name, ''), w.count, w.reason, w.updated_at
		FROM member_warns w LEFT JOIN group_members g ON g.chat_id = w.chat_id AND g.user_id = w.user_id
		WHERE w.chat_id = $1 AND w.count > 0 AND w.updated_at > now() - $2::interval AND ($3 = 0 OR w.user_id = $3)
		ORDER BY w.count DESC, w.updated_at DESC LIMIT $4`, chatID, warnsLast.String(), userID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list warnings: %w", err)
	}
	warns, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (MemberWarn, error) {
		var w MemberWarn
		err := row.Scan(&w.UserID, &w.Name, &w.Count, &w.Reason, &w.At)
		return w, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list warnings: %w", err)
	}
	return warns, nil
}

// RemoveWarn takes one of the member's warnings away and returns how many
// are left.
func (s *Store) RemoveWarn(ctx context.Context, chatID, userID int64) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO member_warns (chat_id, user_id, count) VALUES ($1, $2, 0)
		ON CONFLICT (chat_id, user_id) DO UPDATE SET count = greatest(member_warns.count - 1, 0), updated_at = now()
		RETURNING count`, chatID, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("storage: take a warning away: %w", err)
	}
	return count, nil
}

// ResetWarns clears the member's warnings in the chat.
func (s *Store) ResetWarns(ctx context.Context, chatID, userID int64) error {
	if _, err := s.pool.Exec(ctx, `UPDATE member_warns SET count = 0, updated_at = now() WHERE chat_id = $1 AND user_id = $2`, chatID, userID); err != nil {
		return fmt.Errorf("storage: clear a member's warnings: %w", err)
	}
	return nil
}
