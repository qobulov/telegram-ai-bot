package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CardPlace is the player on the cards' table, ordered as CardTop orders it:
// their row, their place from 1 and how many are on the table; place 0 for a
// player not on it (no points).
func (s *Store) CardPlace(ctx context.Context, userID int64) (me CardPlayer, place, total int, err error) {
	err = s.pool.QueryRow(ctx, `
		WITH ranked AS (
			SELECT user_id, name, points, cards, row_number() OVER (ORDER BY points DESC, user_id) AS place
			FROM card_players WHERE points > 0
		)
		SELECT r.user_id, r.name, r.points, r.cards, r.place, (SELECT count(*) FROM ranked)
		FROM ranked r WHERE r.user_id = $1`, userID).Scan(&me.UserID, &me.Name, &me.Points, &me.Cards, &place, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return CardPlayer{}, 0, 0, nil
	}
	if err != nil {
		return CardPlayer{}, 0, 0, fmt.Errorf("storage: read a card place: %w", err)
	}
	return me, place, total, nil
}

// ArenaPlace is the player's place on the arena's table, ordered as ArenaTop
// orders it, and how many are on it; place 0 for one who never fought.
func (s *Store) ArenaPlace(ctx context.Context, userID int64) (place, total int, err error) {
	err = s.pool.QueryRow(ctx, `
		WITH ranked AS (
			SELECT user_id, row_number() OVER (ORDER BY rating DESC, wins DESC, user_id) AS place FROM arena_players
		)
		SELECT r.place, (SELECT count(*) FROM ranked) FROM ranked r WHERE r.user_id = $1`, userID).Scan(&place, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("storage: read an arena place: %w", err)
	}
	return place, total, nil
}
