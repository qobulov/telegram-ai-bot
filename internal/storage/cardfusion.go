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
	// ErrCardNotFused is a card at ★1, with no stars to reset.
	ErrCardNotFused = errors.New("storage: card not fused")
)

// FuseCard raises the player's card one star, using up cost[stars] of its
// free copies, those beyond the one kept and the ones used up before; it
// gives the card's stars after. A star raised counts toward quest.
func (s *Store) FuseCard(ctx context.Context, userID, cardID int64, cost []int, quest CardQuest) (int, error) {
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
		if err := tx.QueryRow(ctx, `
			UPDATE card_owned SET melted = melted + $3, stars = stars + 1
			WHERE user_id = $1 AND card_id = $2 RETURNING stars`, userID, cardID, cost[stars]).Scan(&stars); err != nil {
			return err
		}
		return countQuest(ctx, tx, userID, quest, 1)
	})
	switch {
	case err == nil:
		return stars, nil
	case errors.Is(err, ErrCardNotHeld), errors.Is(err, ErrCardMaxStars), errors.Is(err, ErrCardFewCopies):
		return 0, err
	}
	return 0, fmt.Errorf("storage: fuse a card: %w", err)
}

// ResetCardStars takes the player's card back to ★1, as the owners asked on
// 2026-11-22, for free: the copies its stars used up, cost[1] to
// cost[stars-1] as FuseCard spent them, are free copies again, and copies
// melted into shards before fusion stay gone. It gives the card's free
// copies after.
func (s *Store) ResetCardStars(ctx context.Context, userID, cardID int64, cost []int) (free int, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var count, melted, stars int
		err := tx.QueryRow(ctx, `
			SELECT count, melted, stars FROM card_owned WHERE user_id = $1 AND card_id = $2 FOR UPDATE`,
			userID, cardID).Scan(&count, &melted, &stars)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrCardNotHeld
		case err != nil:
			return err
		case stars <= 1:
			return ErrCardNotFused
		}
		spent := 0
		for star := 1; star < stars && star < len(cost); star++ {
			spent += cost[star]
		}
		melted = max(melted-spent, 0)
		if _, err := tx.Exec(ctx, `UPDATE card_owned SET melted = $3, stars = 1 WHERE user_id = $1 AND card_id = $2`, userID, cardID, melted); err != nil {
			return err
		}
		free = count - 1 - melted
		return nil
	})
	switch {
	case err == nil:
		return free, nil
	case errors.Is(err, ErrCardNotHeld), errors.Is(err, ErrCardNotFused):
		return 0, err
	}
	return 0, fmt.Errorf("storage: reset a card's stars: %w", err)
}
