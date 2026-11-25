package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Week awards' group settings: SettingWeekAwardsOff, "on", stops a group's
// weekly nominations; SettingWeekAwardsPosted is the week last posted.
const (
	SettingWeekAwardsOff    = "week_awards_off"
	SettingWeekAwardsPosted = "week_awards_posted"
)

// WeekNominee is a member and how many times they did what they are
// nominated for.
type WeekNominee struct {
	UserID int64
	Name   string
	Count  int
}

// WeekLucky is the member who got the best new card of the week.
type WeekLucky struct {
	UserID int64
	Name   string
	Card   string
	Anime  string
	Rarity int
}

// WeekAwards are a group's nominations for a week; a zero nominee has none.
type WeekAwards struct {
	Talker  WeekNominee // the most messages
	Owl     WeekNominee // the most messages from midnight to five
	Laugher WeekNominee // the most messages with 😂 or 🤣
	Muted   WeekNominee // the most mutes
	Lucky   WeekLucky   // the best card first got
}

// otherBots leaves out the messages of bots, whose usernames end in "bot".
const otherBots = `author_id > 0 AND author_name !~* '@\w*bot(\s|$)'`

// WeekAwards are chatID's nominations from since until until, hours read
// in location.
func (s *Store) WeekAwards(ctx context.Context, chatID int64, since, until time.Time, location *time.Location) (WeekAwards, error) {
	var w WeekAwards
	nominees := []struct {
		into  *WeekNominee
		query string
		args  []any
	}{
		{&w.Talker, weekMessages(""), []any{chatID, since, until}},
		{&w.Owl, weekMessages(`AND extract(hour FROM created_at AT TIME ZONE $4::text) < 5`), []any{chatID, since, until, location.String()}},
		{&w.Laugher, weekMessages(`AND text ~ '(😂|🤣)'`), []any{chatID, since, until}},
		{&w.Muted, `
			SELECT a.target_user_id, coalesce((SELECT author_name FROM group_messages m WHERE m.chat_id = a.chat_id AND m.author_id = a.target_user_id ORDER BY m.created_at DESC LIMIT 1), ''), count(*)
			FROM moderation_actions a
			WHERE a.chat_id = $1 AND a.created_at >= $2 AND a.created_at < $3 AND a.action IN ('mute', 'flood_mute') AND a.target_user_id > 0 AND a.undone_at IS NULL
			GROUP BY a.chat_id, a.target_user_id ORDER BY count(*) DESC, a.target_user_id LIMIT 1`, []any{chatID, since, until}},
	}
	for _, n := range nominees {
		err := s.pool.QueryRow(ctx, n.query, n.args...).Scan(&n.into.UserID, &n.into.Name, &n.into.Count)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return WeekAwards{}, fmt.Errorf("storage: read the week's nominations: %w", err)
		}
	}
	err := s.pool.QueryRow(ctx, `
		WITH members AS (
			SELECT author_id, (array_agg(author_name ORDER BY created_at DESC))[1] AS name
			FROM group_messages WHERE chat_id = $1 AND created_at >= $2 AND created_at < $3 AND `+otherBots+`
			GROUP BY author_id)
		SELECT o.user_id, m.name, c.name, c.anime, c.rarity
		FROM card_owned o JOIN members m ON m.author_id = o.user_id JOIN card_catalog c ON c.id = o.card_id
		WHERE o.first_at >= $2 AND o.first_at < $3
		ORDER BY c.rarity DESC, c.favourites DESC, o.first_at LIMIT 1`, chatID, since, until).
		Scan(&w.Lucky.UserID, &w.Lucky.Name, &w.Lucky.Card, &w.Lucky.Anime, &w.Lucky.Rarity)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return WeekAwards{}, fmt.Errorf("storage: read the week's luckiest card: %w", err)
	}
	return w, nil
}

// weekMessages finds the member with the most messages matching where.
func weekMessages(where string) string {
	return `
		SELECT author_id, (array_agg(author_name ORDER BY created_at DESC))[1], count(*)
		FROM group_messages
		WHERE chat_id = $1 AND created_at >= $2 AND created_at < $3 AND ` + otherBots + ` ` + where + `
		GROUP BY author_id ORDER BY count(*) DESC, author_id LIMIT 1`
}

// MarkWeekAwardsPosted notes chatID's week as posted, and reports whether
// it was not yet, so that two runs never post it twice.
func (s *Store) MarkWeekAwardsPosted(ctx context.Context, chatID int64, week string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO group_settings (chat_id, key, value, set_by) VALUES ($1, $2, $3, 0)
		ON CONFLICT (chat_id, key) DO UPDATE SET value = EXCLUDED.value, set_at = now()
		WHERE group_settings.value <> EXCLUDED.value`, chatID, SettingWeekAwardsPosted, week)
	if err != nil {
		return false, fmt.Errorf("storage: mark the week's awards posted: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
