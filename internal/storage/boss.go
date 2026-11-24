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

// BossPlayer is a player against the week's boss: their best damage and
// best level in a fight this week and the fights they had today.
type BossPlayer struct {
	Best      int64
	BestLevel int
	Tries     int
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
		SELECT coalesce(max(damage) FILTER (WHERE week = $2::date), 0)::bigint,
			coalesce(max(level) FILTER (WHERE week = $2::date), 0)::int, count(*) FILTER (WHERE fought_at >= $3)
		FROM boss_fights WHERE user_id = $1 AND (week = $2::date OR fought_at >= $3)`,
		userID, rules.week(), rules.Day).Scan(&p.Best, &p.BestLevel, &p.Tries)
	if err != nil {
		return BossPlayer{}, fmt.Errorf("storage: read a boss player: %w", err)
	}
	return p, nil
}

// RecordBossFight records the player's fight dealing damage and gives its
// coins, in one transaction that locks the player's arena row; a fight past
// the day's Tries is ErrBossNoTries.
func (s *Store) RecordBossFight(ctx context.Context, userID int64, damage int64, level int, rules BossRules) (BossOutcome, error) {
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
		if _, err := tx.Exec(ctx, `INSERT INTO boss_fights (user_id, week, damage, level, fought_at) VALUES ($1, $2::date, $3, $4, $5)`,
			userID, rules.week(), damage, level, rules.Now); err != nil {
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

// BossRow is a player on the boss's table: their best level, best damage
// and all their damage of the week.
type BossRow struct {
	UserID int64
	Name   string
	Best   int64
	Level  int
	Total  int64
}

// bossRanked ranks the week's players by their best level, then their best
// damage, or, with byTotal, by all their damage of the week; then by id.
func bossRanked(byTotal bool) string {
	order := "b.level DESC, b.best DESC"
	if byTotal {
		order = "b.total DESC"
	}
	return `
	SELECT b.user_id, coalesce(c.name, '') AS name, b.best, b.level, b.total,
		row_number() OVER (ORDER BY ` + order + `, b.user_id) AS place
	FROM (SELECT user_id, max(damage) AS best, max(level) AS level, sum(damage)::bigint AS total
		FROM boss_fights WHERE week = $1::date GROUP BY user_id) b
	LEFT JOIN card_players c ON c.user_id = b.user_id`
}

// BossTop are the limit players of the week begun at week with the best
// fights, or, with byTotal, the most damage.
func (s *Store) BossTop(ctx context.Context, week time.Time, limit int, byTotal bool) ([]BossRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id, name, best, level, total FROM (`+bossRanked(byTotal)+`) r ORDER BY place LIMIT $2`,
		week.Format(time.DateOnly), limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read the boss table: %w", err)
	}
	top, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (BossRow, error) {
		var r BossRow
		return r, row.Scan(&r.UserID, &r.Name, &r.Best, &r.Level, &r.Total)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the boss table: %w", err)
	}
	return top, nil
}

// BossPlace is the player's row and place on the week's table and how many
// are on it; place 0 for one who has not fought the boss this week.
func (s *Store) BossPlace(ctx context.Context, week time.Time, userID int64, byTotal bool) (me BossRow, place, total int, err error) {
	err = s.pool.QueryRow(ctx, `
		WITH ranked AS (`+bossRanked(byTotal)+`)
		SELECT user_id, name, best, level, total, place, (SELECT count(*) FROM ranked) FROM ranked WHERE user_id = $2`,
		week.Format(time.DateOnly), userID).Scan(&me.UserID, &me.Name, &me.Best, &me.Level, &me.Total, &place, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return BossRow{}, 0, 0, nil
	}
	if err != nil {
		return BossRow{}, 0, 0, fmt.Errorf("storage: read a boss place: %w", err)
	}
	return me, place, total, nil
}

// PayBossWeek pays the boss's week begun at week, once: the players of the
// best fights that week, ranked as the table ranks them, get prizes by
// their place in the mail, with note(place). paid is false for a week paid
// before.
func (s *Store) PayBossWeek(ctx context.Context, week time.Time, prizes []ArenaPrize, note func(place int) string) (players int, paid bool, err error) {
	places := 0
	for _, p := range prizes {
		places = max(places, p.To)
	}
	day := week.Format(time.DateOnly)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO boss_weeks (week) VALUES ($1::date) ON CONFLICT DO NOTHING`, day)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		paid = true
		rows, err := tx.Query(ctx, `
			SELECT user_id FROM boss_fights WHERE week = $1::date
			GROUP BY user_id ORDER BY max(level) DESC, max(damage) DESC, user_id LIMIT $2`, day, places)
		if err != nil {
			return err
		}
		winners, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		var users []int64
		var notes, boxes []string
		var tokens, counts []int32
		for i, userID := range winners {
			p := prizeAt(prizes, i+1)
			if p.Tokens == 0 && p.Boxes == 0 {
				continue
			}
			box := ""
			if p.Boxes > 0 {
				box = p.Box
			}
			users, notes, tokens = append(users, userID), append(notes, note(i+1)), append(tokens, int32(p.Tokens))
			boxes, counts = append(boxes, box), append(counts, int32(p.Boxes))
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO card_mail (user_id, note, tokens, box, boxes)
			SELECT * FROM unnest($1::bigint[], $2::text[], $3::int[], $4::text[], $5::int[])`, users, notes, tokens, boxes, counts); err != nil {
			return err
		}
		players = len(users)
		_, err = tx.Exec(ctx, `UPDATE boss_weeks SET players = $2 WHERE week = $1::date`, day, players)
		return err
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: pay the boss week: %w", err)
	}
	return players, paid, nil
}
