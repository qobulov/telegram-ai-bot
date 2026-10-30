package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

// GameRank is a player's place in a game's table, by their best in any
// chat.
type GameRank struct {
	Position int
	UserID   int64
	Name     string
	Best     int
}

// GameTable is a game's table across all chats: the first limit players
// by their best, the earlier best first of a tie, and userID's row after
// them when they are further down. Players who tie share a position.
func (s *Store) GameTable(ctx context.Context, game string, userID int64, limit int) ([]GameRank, error) {
	rows, err := s.pool.Query(ctx, `
		WITH bests AS (
			SELECT DISTINCT ON (user_id) user_id, name, best, best_at
			FROM game_scores WHERE game = $1
			ORDER BY user_id, best DESC, best_at
		), ranked AS (
			SELECT user_id, name, best,
				rank() OVER (ORDER BY best DESC) AS position,
				row_number() OVER (ORDER BY best DESC, best_at, user_id) AS n
			FROM bests
		)
		SELECT position, user_id, name, best FROM ranked
		WHERE n <= $2 OR user_id = $3 ORDER BY n`,
		game, limit, userID)
	if err != nil {
		return nil, fmt.Errorf("storage: game table: %w", err)
	}
	table, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GameRank, error) {
		var r GameRank
		err := row.Scan(&r.Position, &r.UserID, &r.Name, &r.Best)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: game table: %w", err)
	}
	return table, nil
}
