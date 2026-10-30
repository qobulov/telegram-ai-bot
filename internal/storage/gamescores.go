package storage

import (
	"context"
	"fmt"
	"time"
)

// GameScore is a play of one of the bot's games scored: which game, whose
// in which chat, and when.
type GameScore struct {
	Game   string
	ChatID int64
	UserID int64
	Name   string
	Score  int
	At     time.Time
}

// RecordGameScore counts a scored play in the player's row for its game
// and chat: one more play, the last score, and the best if it beats it.
func (s *Store) RecordGameScore(ctx context.Context, g GameScore) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO game_scores (game, chat_id, user_id, name, best, last, best_at, played_at)
		VALUES ($1, $2, $3, $4, $5, $5, $6, $6)
		ON CONFLICT (game, chat_id, user_id) DO UPDATE SET
			name      = CASE WHEN EXCLUDED.name <> '' THEN EXCLUDED.name ELSE game_scores.name END,
			best_at   = CASE WHEN EXCLUDED.best > game_scores.best THEN EXCLUDED.best_at ELSE game_scores.best_at END,
			best      = GREATEST(game_scores.best, EXCLUDED.best),
			last      = EXCLUDED.last,
			plays     = game_scores.plays + 1,
			played_at = EXCLUDED.played_at`,
		g.Game, g.ChatID, g.UserID, g.Name, g.Score, g.At)
	if err != nil {
		return fmt.Errorf("storage: record game score: %w", err)
	}
	return nil
}
