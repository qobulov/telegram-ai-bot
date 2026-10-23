package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// HeroGame is a group's round of "guess the hero".
type HeroGame struct {
	ChatID  int64
	Hero    string
	EndsAt  time.Time
	Settled bool
}

// HeroScore is a member's points in a group.
type HeroScore struct {
	UserID int64  `json:"-"`
	Name   string `json:"name"`
	Points int    `json:"points"`
}

// StartHeroGame starts a round in a group, replacing the one before, and
// returns the hero of that one if nobody guessed it, so it can be told.
func (s *Store) StartHeroGame(ctx context.Context, chatID int64, hero string, endsAt time.Time) (previous string, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT hero FROM hero_games WHERE chat_id = $1 AND winner_id IS NULL FOR UPDATE`, chatID).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO hero_games (chat_id, hero, ends_at) VALUES ($1, $2, $3)
			ON CONFLICT (chat_id) DO UPDATE SET hero = EXCLUDED.hero, started_at = now(), ends_at = EXCLUDED.ends_at, winner_id = NULL`,
			chatID, hero, endsAt)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("storage: start hero game: %w", err)
	}
	return previous, nil
}

// OpenHeroGame is a group's round still to be guessed at now, if any.
func (s *Store) OpenHeroGame(ctx context.Context, chatID int64, now time.Time) (HeroGame, bool, error) {
	g := HeroGame{ChatID: chatID}
	err := s.pool.QueryRow(ctx, `SELECT hero, ends_at FROM hero_games WHERE chat_id = $1 AND winner_id IS NULL AND ends_at > $2`,
		chatID, now).Scan(&g.Hero, &g.EndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return HeroGame{}, false, nil
	}
	if err != nil {
		return HeroGame{}, false, fmt.Errorf("storage: open hero game: %w", err)
	}
	return g, true, nil
}

// WinHeroGame gives the open round of a group to a member, if it is still
// open, and returns their points in that group: of two right guesses at
// once, only the first wins.
func (s *Store) WinHeroGame(ctx context.Context, chatID, userID int64, name string, now time.Time) (points int, won bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE hero_games SET winner_id = $2 WHERE chat_id = $1 AND winner_id IS NULL AND ends_at > $3`, chatID, userID, now)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		won = true
		return tx.QueryRow(ctx, `
			INSERT INTO hero_scores (chat_id, user_id, name, points) VALUES ($1, $2, $3, 1)
			ON CONFLICT (chat_id, user_id) DO UPDATE SET points = hero_scores.points + 1, name = EXCLUDED.name, updated_at = now()
			RETURNING points`, chatID, userID, name).Scan(&points)
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: win hero game: %w", err)
	}
	return points, won, nil
}

// HeroScores are a group's best players, most points first.
func (s *Store) HeroScores(ctx context.Context, chatID int64, limit int) ([]HeroScore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, name, points FROM hero_scores WHERE chat_id = $1
		ORDER BY points DESC, updated_at LIMIT $2`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: hero scores: %w", err)
	}
	scores, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (HeroScore, error) {
		var sc HeroScore
		err := row.Scan(&sc.UserID, &sc.Name, &sc.Points)
		return sc, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: hero scores: %w", err)
	}
	return scores, nil
}
