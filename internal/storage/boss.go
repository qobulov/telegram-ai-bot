package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrBossNoTries is a boss fight past the day's limit.
var ErrBossNoTries = errors.New("storage: no boss fight left today")

// BossRules are a boss fight's: Week the Monday that began its week and
// Day the midnight that began its day, both in Tashkent, Now when it is
// fought, Tries the fights a day allows and Coins the arena coins it gives.
type BossRules struct {
	Week, Day, Now time.Time
	Tries, Coins   int
}

func (r BossRules) week() string { return r.Week.Format(time.DateOnly) }

// BossPlayer is a player against the week's boss: their best damage in a
// fight this week and the fights they had today.
type BossPlayer struct {
	Best  int64
	Tries int
}

// BossOutcome is what a fight changed: the fights had today and the
// player's arena coins after.
type BossOutcome struct {
	Tries, Coins int
}

// BossWeek is all the damage dealt the boss in the week begun at week.
func (s *Store) BossWeek(ctx context.Context, week time.Time) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(damage), 0)::bigint FROM boss_fights WHERE week = $1::date`,
		week.Format(time.DateOnly)).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("storage: read the boss's week: %w", err)
	}
	return total, nil
}

// BossPlayerOf reads the player's best of the week and today's fights.
func (s *Store) BossPlayerOf(ctx context.Context, userID int64, rules BossRules) (BossPlayer, error) {
	var p BossPlayer
	err := s.pool.QueryRow(ctx, `
		SELECT coalesce(max(damage) FILTER (WHERE week = $2::date), 0)::bigint, count(*) FILTER (WHERE fought_at >= $3)
		FROM boss_fights WHERE user_id = $1 AND (week = $2::date OR fought_at >= $3)`,
		userID, rules.week(), rules.Day).Scan(&p.Best, &p.Tries)
	if err != nil {
		return BossPlayer{}, fmt.Errorf("storage: read a boss player: %w", err)
	}
	return p, nil
}

// RecordBossFight records the player's fight dealing damage and gives its
// coins, in one transaction that locks the player's arena row; a fight past
// the day's Tries is ErrBossNoTries.
func (s *Store) RecordBossFight(ctx context.Context, userID int64, damage int64, rules BossRules) (BossOutcome, error) {
	var out BossOutcome
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO arena_players (user_id) VALUES ($1)
			ON CONFLICT (user_id) DO UPDATE SET coins = arena_players.coins`, userID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM boss_fights WHERE user_id = $1 AND fought_at >= $2`,
			userID, rules.Day).Scan(&out.Tries); err != nil {
			return err
		}
		if out.Tries >= rules.Tries {
			return ErrBossNoTries
		}
		out.Tries++
		if _, err := tx.Exec(ctx, `INSERT INTO boss_fights (user_id, week, damage, fought_at) VALUES ($1, $2::date, $3, $4)`,
			userID, rules.week(), damage, rules.Now); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE arena_players SET coins = coins + $2 WHERE user_id = $1 RETURNING coins`,
			userID, rules.Coins).Scan(&out.Coins)
	})
	if errors.Is(err, ErrBossNoTries) {
		return BossOutcome{}, err
	}
	if err != nil {
		return BossOutcome{}, fmt.Errorf("storage: record a boss fight: %w", err)
	}
	return out, nil
}

// BossRow is a player on the boss's table: their best damage of the week.
type BossRow struct {
	UserID int64
	Name   string
	Best   int64
}

// bossRanked ranks the week's players by their best fight, then by id.
const bossRanked = `
	SELECT b.user_id, coalesce(c.name, '') AS name, b.best, row_number() OVER (ORDER BY b.best DESC, b.user_id) AS place
	FROM (SELECT user_id, max(damage) AS best FROM boss_fights WHERE week = $1::date GROUP BY user_id) b
	LEFT JOIN card_players c ON c.user_id = b.user_id`

// BossTop are the limit players of the week begun at week with the best fights.
func (s *Store) BossTop(ctx context.Context, week time.Time, limit int) ([]BossRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id, name, best FROM (`+bossRanked+`) r ORDER BY place LIMIT $2`,
		week.Format(time.DateOnly), limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read the boss table: %w", err)
	}
	top, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (BossRow, error) {
		var r BossRow
		return r, row.Scan(&r.UserID, &r.Name, &r.Best)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the boss table: %w", err)
	}
	return top, nil
}

// BossPlace is the player's row and place on the week's table and how many
// are on it; place 0 for one who has not fought the boss this week.
func (s *Store) BossPlace(ctx context.Context, week time.Time, userID int64) (me BossRow, place, total int, err error) {
	err = s.pool.QueryRow(ctx, `
		WITH ranked AS (`+bossRanked+`)
		SELECT user_id, name, best, place, (SELECT count(*) FROM ranked) FROM ranked WHERE user_id = $2`,
		week.Format(time.DateOnly), userID).Scan(&me.UserID, &me.Name, &me.Best, &place, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return BossRow{}, 0, 0, nil
	}
	if err != nil {
		return BossRow{}, 0, 0, fmt.Errorf("storage: read a boss place: %w", err)
	}
	return me, place, total, nil
}
