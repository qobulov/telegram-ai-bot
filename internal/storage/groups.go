package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Overview is what the bot knows, in numbers. The "Today" counts start at
// the moment given to Overview.
type Overview struct {
	Groups             int `json:"groups"`
	MutedGroups        int `json:"muted_groups"`
	Users              int `json:"users"`
	UsersToday         int `json:"users_today"`
	GroupMessagesToday int `json:"group_messages_today"`
	ImagesToday        int `json:"images_today"`
}

// Overview counts the groups, users, messages and pictures the bot knows of.
func (s *Store) Overview(ctx context.Context, since time.Time) (Overview, error) {
	var o Overview
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM groups),
			(SELECT count(*) FROM chats WHERE muted AND chat_id IN (SELECT chat_id FROM groups)),
			(SELECT count(*) FROM users),
			(SELECT count(*) FROM users WHERE last_seen_at >= $1),
			(SELECT count(*) FROM group_messages WHERE created_at >= $1),
			(SELECT count(*) FROM image_generations WHERE created_at >= $1)`, since).
		Scan(&o.Groups, &o.MutedGroups, &o.Users, &o.UsersToday, &o.GroupMessagesToday, &o.ImagesToday)
	if err != nil {
		return Overview{}, fmt.Errorf("storage: overview: %w", err)
	}
	return o, nil
}

// GroupSummary is one group the bot has seen.
type GroupSummary struct {
	ChatID        int64
	Title         string
	Username      string
	FirstSeen     time.Time
	LastSeen      time.Time
	MessagesToday int
	Muted         bool
}

// Groups returns up to limit groups, most recently active first, with their
// message counts since the given moment.
func (s *Store) Groups(ctx context.Context, since time.Time, limit int) ([]GroupSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.chat_id, g.title, coalesce(g.username, ''), g.first_seen_at, g.last_seen_at,
			(SELECT count(*) FROM group_messages m WHERE m.chat_id = g.chat_id AND m.created_at >= $1),
			EXISTS (SELECT 1 FROM chats c WHERE c.chat_id = g.chat_id AND c.muted)
		FROM groups g
		ORDER BY g.last_seen_at DESC
		LIMIT $2`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read groups: %w", err)
	}
	groups, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupSummary, error) {
		var g GroupSummary
		err := row.Scan(&g.ChatID, &g.Title, &g.Username, &g.FirstSeen, &g.LastSeen, &g.MessagesToday, &g.Muted)
		return g, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read groups: %w", err)
	}
	return groups, nil
}
