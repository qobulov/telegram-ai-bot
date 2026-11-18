package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Errors of fusing a card.
var (
	// ErrCardMaxStars is a card at the most stars there are.
	ErrCardMaxStars = errors.New("storage: card at the most stars")
	// ErrCardFewCopies is a card with too few free copies for its next star.
	ErrCardFewCopies = errors.New("storage: too few copies to fuse")
)

// FuseCard raises the player's card one star, using up cost[stars] of its
// free copies, those beyond the one kept and the ones used up before; it
// gives the card's stars after.
func (s *Store) FuseCard(ctx context.Context, userID, cardID int64, cost []int) (int, error) {
	var stars int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var count, melted int
		err := tx.QueryRow(ctx, `
			SELECT count, melted, stars FROM card_owned WHERE user_id = $1 AND card_id = $2 FOR UPDATE`,
			userID, cardID).Scan(&count, &melted, &stars)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrCardNotHeld
		case err != nil:
			return err
		case stars >= len(cost):
			return ErrCardMaxStars
		case count-1-melted < cost[stars]:
			return ErrCardFewCopies
		}
		return tx.QueryRow(ctx, `
			UPDATE card_owned SET melted = melted + $3, stars = stars + 1
			WHERE user_id = $1 AND card_id = $2 RETURNING stars`, userID, cardID, cost[stars]).Scan(&stars)
	})
	switch {
	case err == nil:
		return stars, nil
	case errors.Is(err, ErrCardNotHeld), errors.Is(err, ErrCardMaxStars), errors.Is(err, ErrCardFewCopies):
		return 0, err
	}
	return 0, fmt.Errorf("storage: fuse a card: %w", err)
}
