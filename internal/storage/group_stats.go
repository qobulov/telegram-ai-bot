package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GroupStats is how a group talked over a span, from its log.
type GroupStats struct {
	Messages int
	Members  int
	// Top are the members who wrote the most, the most first.
	Top []AuthorCount
	// Hours are the messages written in each hour of the day, 0 to 23, in
	// the time zone asked for.
	Hours [24]int
	// Swearers are the members the swear guard caught the most.
	Swearers []AuthorCount
}

// AuthorCount is a member and how many of something is theirs.
type AuthorCount struct {
	UserID int64
	Name   string
	Count  int
}

// GroupStats counts a group's logged messages since a time, without the
// bot's own, and the swears its guard caught: the top limit writers and
// swearers by their latest name, and the messages by hour in timeZone.
func (s *Store) GroupStats(ctx context.Context, chatID, botID int64, since time.Time, timeZone string, limit int) (GroupStats, error) {
	var stats GroupStats
	err := s.pool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT author_id) FROM group_messages
		WHERE chat_id = $1 AND created_at >= $2 AND author_id IS DISTINCT FROM $3`,
		chatID, since, botID).Scan(&stats.Messages, &stats.Members)
	if err != nil {
		return GroupStats{}, fmt.Errorf("storage: group stats: %w", err)
	}
	if stats.Top, err = s.authorCounts(ctx, `
		SELECT author_id, (array_agg(author_name ORDER BY created_at DESC))[1], count(*) FROM group_messages
		WHERE chat_id = $1 AND created_at >= $2 AND author_id IS NOT NULL AND author_id <> $3
		GROUP BY author_id ORDER BY count(*) DESC, author_id LIMIT $4`,
		chatID, since, botID, limit); err != nil {
		return GroupStats{}, err
	}
	if stats.Swearers, err = s.authorCounts(ctx, `
		SELECT a.target_user_id, coalesce(nullif(m.first_name, ''), m.username, ''), count(*) FROM moderation_actions a
		LEFT JOIN group_members m ON m.chat_id = a.chat_id AND m.user_id = a.target_user_id
		WHERE a.chat_id = $1 AND a.created_at >= $2 AND a.action IN ($3, $4, $5) AND a.target_user_id IS NOT NULL
		GROUP BY a.target_user_id, m.first_name, m.username ORDER BY count(*) DESC, a.target_user_id LIMIT $6`,
		chatID, since, ActionSwear, ActionSwearAtBot, ActionSwearWord, limit); err != nil {
		return GroupStats{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT extract(hour FROM created_at AT TIME ZONE $4)::int, count(*) FROM group_messages
		WHERE chat_id = $1 AND created_at >= $2 AND author_id IS DISTINCT FROM $3
		GROUP BY 1`, chatID, since, botID, timeZone)
	if err != nil {
		return GroupStats{}, fmt.Errorf("storage: group stats by hour: %w", err)
	}
	var hour, count int
	if _, err := pgx.ForEachRow(rows, []any{&hour, &count}, func() error {
		stats.Hours[hour] = count
		return nil
	}); err != nil {
		return GroupStats{}, fmt.Errorf("storage: group stats by hour: %w", err)
	}
	return stats, nil
}

func (s *Store) authorCounts(ctx context.Context, query string, args ...any) ([]AuthorCount, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: group stats: %w", err)
	}
	counts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AuthorCount, error) {
		var c AuthorCount
		err := row.Scan(&c.UserID, &c.Name, &c.Count)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: group stats: %w", err)
	}
	return counts, nil
}
