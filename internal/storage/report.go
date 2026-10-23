package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// DayReport is what the bot did over a period, for its owners.
type DayReport struct {
	Groups []GroupDay
	// NewGroups are the groups first seen in the period.
	NewGroups []string
	// PrivateUsers wrote in private chats and got PrivateAnswers; past
	// their limit, PrivateRefused were not answered.
	PrivateUsers, PrivateAnswers, PrivateRefused int
	// Moderation counts what the bot did to members, by action.
	Moderation map[string]int
}

// GroupDay is one group's period: its members' messages and the bot's
// answers.
type GroupDay struct {
	Title    string
	Messages int
	Replies  int
}

// Report is what happened from since until until.
func (s *Store) Report(ctx context.Context, since, until time.Time) (DayReport, error) {
	var r DayReport
	rows, err := s.pool.Query(ctx, `
		SELECT g.title,
		       (SELECT count(*) FROM group_messages m WHERE m.chat_id = g.chat_id AND m.created_at >= $1 AND m.created_at < $2),
		       (SELECT count(*) FROM group_replies p WHERE p.chat_id = g.chat_id AND p.created_at >= $1 AND p.created_at < $2)
		FROM groups g`, since, until)
	if err != nil {
		return DayReport{}, fmt.Errorf("storage: report groups: %w", err)
	}
	all, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupDay, error) {
		var d GroupDay
		err := row.Scan(&d.Title, &d.Messages, &d.Replies)
		return d, err
	})
	if err != nil {
		return DayReport{}, fmt.Errorf("storage: report groups: %w", err)
	}
	for _, d := range all {
		if d.Messages > 0 || d.Replies > 0 {
			r.Groups = append(r.Groups, d)
		}
	}
	rows, err = s.pool.Query(ctx, `SELECT title FROM groups WHERE first_seen_at >= $1 AND first_seen_at < $2 ORDER BY first_seen_at`, since, until)
	if err != nil {
		return DayReport{}, fmt.Errorf("storage: report new groups: %w", err)
	}
	if r.NewGroups, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return DayReport{}, fmt.Errorf("storage: report new groups: %w", err)
	}
	err = s.pool.QueryRow(ctx, `
		SELECT count(DISTINCT user_id), count(*) FILTER (WHERE NOT denied), count(*) FILTER (WHERE denied)
		FROM private_messages WHERE created_at >= $1 AND created_at < $2`, since, until).Scan(&r.PrivateUsers, &r.PrivateAnswers, &r.PrivateRefused)
	if err != nil {
		return DayReport{}, fmt.Errorf("storage: report private chats: %w", err)
	}
	rows, err = s.pool.Query(ctx, `SELECT action, count(*) FROM moderation_actions WHERE created_at >= $1 AND created_at < $2 GROUP BY action`, since, until)
	if err != nil {
		return DayReport{}, fmt.Errorf("storage: report moderation: %w", err)
	}
	r.Moderation = map[string]int{}
	var action string
	var count int
	_, err = pgx.ForEachRow(rows, []any{&action, &count}, func() error {
		r.Moderation[action] = count
		return nil
	})
	if err != nil {
		return DayReport{}, fmt.Errorf("storage: report moderation: %w", err)
	}
	return r, nil
}
