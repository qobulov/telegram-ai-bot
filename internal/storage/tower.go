package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Errors of the tower.
var (
	// ErrTowerNoTries is a fight with the day's attempts used up.
	ErrTowerNoTries = errors.New("storage: no tower attempts left today")
	// ErrTowerFloorMoved is a fight for a floor that is no longer the
	// player's next, another fight having cleared it meanwhile.
	ErrTowerFloorMoved = errors.New("storage: the tower floor fought is not the next")
)

// TowerPlayer is a player in the tower: the highest floor they cleared, 0
// for none, and the attempts they used today.
type TowerPlayer struct {
	Floor, Tries int
}

// TowerRules are a tower fight's: Tries a player has a day, Day the start
// of today in Tashkent (its date, in its own location, is the day counted),
// and the Coins and Tokens a win gives.
type TowerRules struct {
	Tries         int
	Day           time.Time
	Coins, Tokens int
}

func (r TowerRules) day() string { return r.Day.Format(time.DateOnly) }

// TowerOutcome is what a fight changed: the floor cleared now, the
// attempts used today, and the player's arena coins and card tokens after
// (both 0 for a loss, which gives nothing).
type TowerOutcome struct {
	Floor, Tries  int
	Coins, Tokens int
}

// TowerPlayerOf reads the player's tower row as of rules.Day; one never in
// the tower is on floor 0 with no attempt used.
func (s *Store) TowerPlayerOf(ctx context.Context, userID int64, day time.Time) (TowerPlayer, error) {
	var p TowerPlayer
	err := s.pool.QueryRow(ctx, `
		SELECT floor, CASE WHEN tries_day = $2::date THEN tries ELSE 0 END FROM tower_players WHERE user_id = $1`,
		userID, day.Format(time.DateOnly)).Scan(&p.Floor, &p.Tries)
	if errors.Is(err, pgx.ErrNoRows) {
		return TowerPlayer{}, nil
	}
	if err != nil {
		return p, fmt.Errorf("storage: read the tower player: %w", err)
	}
	return p, nil
}

// RecordTowerFight records the player's fight for floor, in one
// transaction: it takes one of the day's attempts, ErrTowerNoTries with
// none, and ErrTowerFloorMoved when floor is not their next; a win clears
// the floor and gives the rules' arena coins and card tokens.
func (s *Store) RecordTowerFight(ctx context.Context, userID int64, name string, floor int, won bool, rules TowerRules) (TowerOutcome, error) {
	var out TowerOutcome
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		p, err := lockTowerPlayer(ctx, tx, userID, rules.day())
		switch {
		case err != nil:
			return err
		case p.Tries >= rules.Tries:
			return ErrTowerNoTries
		case p.Floor+1 != floor:
			return ErrTowerFloorMoved
		}
		out.Floor, out.Tries = p.Floor, p.Tries+1
		if won {
			out.Floor = floor
		}
		if _, err := tx.Exec(ctx, `
			UPDATE tower_players SET floor = $2, tries_day = $3::date, tries = $4, updated_at = now() WHERE user_id = $1`,
			userID, out.Floor, rules.day(), out.Tries); err != nil {
			return err
		}
		if !won {
			return nil
		}
		return giveTowerReward(ctx, tx, userID, name, rules, &out)
	})
	if errors.Is(err, ErrTowerNoTries) || errors.Is(err, ErrTowerFloorMoved) {
		return TowerOutcome{}, err
	}
	if err != nil {
		return TowerOutcome{}, fmt.Errorf("storage: record a tower fight: %w", err)
	}
	return out, nil
}

// lockTowerPlayer makes the player's tower row where missing and locks it;
// attempts of a day before day read as 0.
func lockTowerPlayer(ctx context.Context, tx pgx.Tx, userID int64, day string) (TowerPlayer, error) {
	var p TowerPlayer
	if _, err := tx.Exec(ctx, `INSERT INTO tower_players (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		return p, err
	}
	err := tx.QueryRow(ctx, `
		SELECT floor, CASE WHEN tries_day = $2::date THEN tries ELSE 0 END FROM tower_players WHERE user_id = $1 FOR UPDATE`,
		userID, day).Scan(&p.Floor, &p.Tries)
	return p, err
}

// giveTowerReward adds a win's coins to the player's arena row and its
// tokens to their cards' row, the arena's locked first as elsewhere.
func giveTowerReward(ctx context.Context, tx pgx.Tx, userID int64, name string, rules TowerRules, out *TowerOutcome) error {
	if err := tx.QueryRow(ctx, `
		INSERT INTO arena_players (user_id, coins) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET coins = arena_players.coins + $2 RETURNING coins`,
		userID, rules.Coins).Scan(&out.Coins); err != nil {
		return err
	}
	if _, err := lockCardPlayer(ctx, tx, userID, name); err != nil {
		return err
	}
	return tx.QueryRow(ctx, `UPDATE card_players SET tokens = tokens + $2 WHERE user_id = $1 RETURNING tokens`,
		userID, rules.Tokens).Scan(&out.Tokens)
}
