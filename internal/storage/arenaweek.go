package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ArenaPrize gives Tokens, and Boxes of the box Box, to each of the places
// From through To, the first place being 1.
type ArenaPrize struct {
	From   int    `json:"from"`
	To     int    `json:"to"`
	Tokens int    `json:"tokens"`
	Boxes  int    `json:"boxes,omitempty"`
	Box    string `json:"box,omitempty"`
}

// prizeAt is the prize for place, none for one no prize covers.
func prizeAt(prizes []ArenaPrize, place int) ArenaPrize {
	for _, p := range prizes {
		if place >= p.From && place <= p.To {
			return p
		}
	}
	return ArenaPrize{}
}

// ArenaRatingStart is the rating a player starts each arena week with.
const ArenaRatingStart = 1000

// PayArenaWeek pays the arena's week from since until until, once: the
// players who attacked within it, by rating, then wins, get their place's
// prize as a letter whose note is note(place); then, as the owners asked on
// 2026-11-22, every rating not frozen starts again at ArenaRatingStart. It
// gives the players paid, and paid false for a week paid before.
func (s *Store) PayArenaWeek(ctx context.Context, since, until time.Time, prizes []ArenaPrize, note func(place int) string) (players int, paid bool, err error) {
	places := 0
	for _, p := range prizes {
		places = max(places, p.To)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO arena_weeks (week) VALUES ($1::date) ON CONFLICT DO NOTHING`, since.Format(time.DateOnly))
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		paid = true
		rows, err := tx.Query(ctx, `
			SELECT p.user_id FROM arena_players p
			WHERE EXISTS (SELECT 1 FROM arena_battles b WHERE b.attacker = p.user_id AND b.at >= $1 AND b.at < $2)
			ORDER BY p.rating DESC, p.wins DESC, p.user_id
			LIMIT $3`, since, until, places)
		if err != nil {
			return err
		}
		winners, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		var users []int64
		var notes, boxes []string
		var tokens, counts []int32
		for i, userID := range winners {
			p := prizeAt(prizes, i+1)
			if p.Tokens == 0 && p.Boxes == 0 {
				continue
			}
			box := ""
			if p.Boxes > 0 {
				box = p.Box
			}
			users, notes, tokens = append(users, userID), append(notes, note(i+1)), append(tokens, int32(p.Tokens))
			boxes, counts = append(boxes, box), append(counts, int32(p.Boxes))
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO card_mail (user_id, note, tokens, box, boxes)
			SELECT * FROM unnest($1::bigint[], $2::text[], $3::int[], $4::text[], $5::int[])`, users, notes, tokens, boxes, counts); err != nil {
			return err
		}
		players = len(users)
		if _, err := tx.Exec(ctx, `UPDATE arena_weeks SET players = $2 WHERE week = $1::date`, since.Format(time.DateOnly), players); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE arena_players SET rating = $1 WHERE NOT rating_frozen AND rating <> $1`, ArenaRatingStart)
		return err
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: pay the arena week: %w", err)
	}
	return players, paid, nil
}

// ArenaWeekPlace is the player's place this week, as the week from since
// would be paid now: among the players who attacked since since, by rating,
// then wins; 0 for a player who has not.
func (s *Store) ArenaWeekPlace(ctx context.Context, userID int64, since time.Time) (int, error) {
	var place int
	err := s.pool.QueryRow(ctx, `
		WITH active AS (
			SELECT p.user_id, p.rating, p.wins FROM arena_players p
			WHERE EXISTS (SELECT 1 FROM arena_battles b WHERE b.attacker = p.user_id AND b.at >= $2)
		)
		SELECT coalesce((
			SELECT 1 + (SELECT count(*) FROM active o WHERE (o.rating, o.wins, -o.user_id) > (me.rating, me.wins, -me.user_id))
			FROM active me WHERE me.user_id = $1
		), 0)::int`, userID, since).Scan(&place)
	if err != nil {
		return 0, fmt.Errorf("storage: read an arena week place: %w", err)
	}
	return place, nil
}
