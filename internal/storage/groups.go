package storage

import (
	"context"
	"errors"
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
	// BotStatus is what Telegram last said about the bot here, or "".
	BotStatus string
	// Rights are the bot's admin rights here, as Telegram last reported
	// them; empty when it holds none or that is not known.
	Rights []string
}

// Groups returns up to limit groups, most recently active first, with their
// message counts since the given moment.
func (s *Store) Groups(ctx context.Context, since time.Time, limit int) ([]GroupSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.chat_id, g.title, coalesce(g.username, ''), g.first_seen_at, g.last_seen_at, coalesce(g.bot_status, ''),
			coalesce(g.bot_rights, '[]'),
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
		err := row.Scan(&g.ChatID, &g.Title, &g.Username, &g.FirstSeen, &g.LastSeen, &g.BotStatus, &g.Rights, &g.MessagesToday, &g.Muted)
		return g, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read groups: %w", err)
	}
	return groups, nil
}

// GroupStatus is a change to the bot's membership in a group.
type GroupStatus struct {
	ChatID    int64
	Title     string
	Username  string
	BotStatus string
	// Rights are the admin rights the bot holds after the change, by their
	// Bot API names; none for a plain member, or a bot that left.
	Rights []string
}

// RecordGroup notes the bot being added to, promoted in or removed from a
// group. Telegram reports every change to the bot's rights this way, so the
// rights stored here stay current.
func (s *Store) RecordGroup(ctx context.Context, g GroupStatus) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO groups (chat_id, title, username, bot_status, bot_rights) VALUES ($1, $2, nullif($3, ''), $4, $5)
		ON CONFLICT (chat_id) DO UPDATE
		SET title = coalesce(nullif(EXCLUDED.title, ''), groups.title),
		    username = EXCLUDED.username,
		    bot_status = EXCLUDED.bot_status,
		    bot_rights = EXCLUDED.bot_rights,
		    last_seen_at = now()`,
		g.ChatID, g.Title, g.Username, g.BotStatus, rightsArray(g.Rights))
	if err != nil {
		return fmt.Errorf("storage: record group: %w", err)
	}
	return nil
}

// BotRights returns the bot's admin rights in a group as last stored. known
// is false when none were ever stored: the group was only seen in messages,
// or before rights were kept. Then the caller has to ask Telegram.
func (s *Store) BotRights(ctx context.Context, chatID int64) (rights []string, known bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT bot_rights IS NOT NULL, coalesce(bot_rights, '[]') FROM groups WHERE chat_id = $1`, chatID).
		Scan(&known, &rights)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("storage: read bot rights: %w", err)
	}
	return rights, known, nil
}

// SetBotRights stores what Telegram answered when asked about the bot in a
// group. Unlike RecordGroup it is not a membership change, so it leaves the
// group's name and last-seen time alone. The last writer wins: the caller has
// just asked Telegram, and its answer must be able to correct rights already
// stored. An answer that loses a race with a newer my_chat_member update
// leaves older rights until the next answer or membership change.
func (s *Store) SetBotRights(ctx context.Context, chatID int64, status string, rights []string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO groups (chat_id, bot_status, bot_rights) VALUES ($1, $2, $3)
		ON CONFLICT (chat_id) DO UPDATE
		SET bot_status = EXCLUDED.bot_status, bot_rights = EXCLUDED.bot_rights`,
		chatID, status, rightsArray(rights))
	if err != nil {
		return fmt.Errorf("storage: set bot rights: %w", err)
	}
	return nil
}

// rightsArray keeps "holds none" apart from "not known": no rights are
// stored as an empty JSON array, never as NULL.
func rightsArray(rights []string) []string {
	if rights == nil {
		return []string{}
	}
	return rights
}
