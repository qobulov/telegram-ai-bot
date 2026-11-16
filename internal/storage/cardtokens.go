package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNoTokens is returned by TokenCard when the player has no token left.
var ErrNoTokens = errors.New("storage: no tokens")

// CardTokens is how many 🎟 tokens the player has.
func (s *Store) CardTokens(ctx context.Context, userID int64) (int, error) {
	var tokens int
	err := s.pool.QueryRow(ctx, `SELECT coalesce((SELECT tokens FROM card_players WHERE user_id = $1), 0)`, userID).Scan(&tokens)
	if err != nil {
		return 0, fmt.Errorf("storage: read a player's tokens: %w", err)
	}
	return tokens, nil
}

// TokenCard gives the player a card for one token, as DrawCard does but
// leaving the day's free cards as they were; with none it returns
// ErrNoTokens.
func (s *Store) TokenCard(ctx context.Context, userID int64, name string, roll CardRoll) (CardDraw, error) {
	var draw CardDraw
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		var tokens int
		err = tx.QueryRow(ctx, `UPDATE card_players SET tokens = tokens - 1 WHERE user_id = $1 AND tokens > 0 RETURNING tokens`, userID).Scan(&tokens)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoTokens
		}
		if err != nil {
			return err
		}
		if draw, err = giveCard(ctx, tx, userID, name, "", roll, player); err != nil {
			return err
		}
		draw.Tokens = tokens
		return nil
	})
	if errors.Is(err, ErrNoTokens) {
		return CardDraw{}, ErrNoTokens
	}
	if err != nil {
		return CardDraw{}, fmt.Errorf("storage: draw a card for a token: %w", err)
	}
	return draw, nil
}
