package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AnimeRewardsClaimed is the animes whose collection reward the player took.
func (s *Store) AnimeRewardsClaimed(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT anime FROM card_anime_rewards WHERE user_id = $1 ORDER BY claimed_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("storage: anime rewards claimed: %w", err)
	}
	claimed, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("storage: anime rewards claimed: %w", err)
	}
	return claimed, nil
}

// ClaimAnimeReward gives the player tokens for anime's collection, once:
// claimed is false, and nothing given, when it was taken before. It
// returns the player's tokens after.
func (s *Store) ClaimAnimeReward(ctx context.Context, userID int64, name, anime string, tokens int) (after int, claimed bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO card_anime_rewards (user_id, anime, tokens) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, anime) DO NOTHING`, userID, anime, tokens)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			after = player.tokens
			return nil
		}
		claimed = true
		return tx.QueryRow(ctx, `UPDATE card_players SET tokens = tokens + $2 WHERE user_id = $1 RETURNING tokens`, userID, tokens).Scan(&after)
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: claim an anime reward: %w", err)
	}
	return after, claimed, nil
}
