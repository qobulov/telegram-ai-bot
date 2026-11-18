package storage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// Errors of the arena.
var (
	// ErrArenaTeamRepeat is a team holding one card twice.
	ErrArenaTeamRepeat = errors.New("storage: a card twice in the arena team")
	// ErrArenaNoAttacks is an attack past the day's last.
	ErrArenaNoAttacks = errors.New("storage: no arena attacks left today")
)

// arenaStartRating is a player's rating before their first battle.
const arenaStartRating = 1000

// ArenaRules are the day's limits of the arena: attacks a player makes,
// tokens they win and the Elo K. Day is the start of today in Tashkent;
// its date, in its own location, is the day the counters count.
type ArenaRules struct {
	Attacks, Tokens, K int
	Day                time.Time
}

// day is the date the rules' counters count, as Postgres reads a DATE.
func (r ArenaRules) day() string { return r.Day.Format(time.DateOnly) }

// ArenaPlayer is a player as the arena knows them: their name, rating,
// wins and losses, the day's attacks and tokens left (counted only by
// ArenaPlayerOf), and their saved team's power, 0 without one.
type ArenaPlayer struct {
	UserID                                               int64
	Name                                                 string
	Rating, Wins, Losses, AttacksLeft, TokensLeft, Power int
}

// ArenaOutcome is what an attack changed for the attacker: their rating,
// whether it won them a token, and the day's attacks and tokens left.
type ArenaOutcome struct {
	RatingChange            int
	Token                   bool
	AttacksLeft, TokensLeft int
}

// SaveArenaTeam saves the player's team, replacing the one before. The five
// cards must be distinct, ErrArenaTeamRepeat, and held, ErrCardNotHeld.
func (s *Store) SaveArenaTeam(ctx context.Context, userID int64, front [2]int64, back [3]int64, power int) error {
	cards := append(front[:], back[:]...)
	if hasRepeat(cards) {
		return ErrArenaTeamRepeat
	}
	held, err := s.heldOf(ctx, userID, cards)
	if err != nil {
		return fmt.Errorf("storage: read the arena team's cards: %w", err)
	}
	if held != len(cards) {
		return ErrCardNotHeld
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO arena_teams (user_id, front, back, power) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET front = $2, back = $3, power = $4, updated_at = now()`,
		userID, front[:], back[:], power)
	if err != nil {
		return fmt.Errorf("storage: save the arena team: %w", err)
	}
	return nil
}

func hasRepeat(ids []int64) bool {
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

// heldOf counts the cards of ids the player holds a copy of.
func (s *Store) heldOf(ctx context.Context, userID int64, ids []int64) (int, error) {
	var held int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM card_owned WHERE user_id = $1 AND card_id = ANY($2) AND count - melted >= 1`,
		userID, ids).Scan(&held)
	return held, err
}

// ArenaTeam reads the player's saved team; ok is false when they have none.
// Its cards are as saved, whether or not all are still held.
func (s *Store) ArenaTeam(ctx context.Context, userID int64) (front [2]int64, back [3]int64, ok bool, err error) {
	var f, b []int64
	err = s.pool.QueryRow(ctx, `SELECT front, back FROM arena_teams WHERE user_id = $1`, userID).Scan(&f, &b)
	if errors.Is(err, pgx.ErrNoRows) {
		return front, back, false, nil
	}
	if err != nil {
		return front, back, false, fmt.Errorf("storage: read the arena team: %w", err)
	}
	if len(f) != len(front) || len(b) != len(back) {
		return front, back, false, fmt.Errorf("storage: arena team of %d has %d in front and %d behind", userID, len(f), len(b))
	}
	copy(front[:], f)
	copy(back[:], b)
	return front, back, true, nil
}

// ArenaPlayerOf reads the player, with the day's attacks and tokens left;
// one who never fought is at the start rating with the whole day left.
func (s *Store) ArenaPlayerOf(ctx context.Context, userID int64, rules ArenaRules) (ArenaPlayer, error) {
	p := ArenaPlayer{UserID: userID}
	var attacks, tokens int
	err := s.pool.QueryRow(ctx, `
		SELECT coalesce(c.name, ''), coalesce(p.rating, $3), coalesce(p.wins, 0), coalesce(p.losses, 0),
			CASE WHEN p.attacks_day = $2::date THEN p.attacks ELSE 0 END,
			CASE WHEN p.tokens_day = $2::date THEN p.tokens_won ELSE 0 END,
			coalesce(t.power, 0)
		FROM (SELECT $1::bigint AS user_id) u
		LEFT JOIN arena_players p USING (user_id)
		LEFT JOIN card_players c USING (user_id)
		LEFT JOIN arena_teams t USING (user_id)`, userID, rules.day(), arenaStartRating).
		Scan(&p.Name, &p.Rating, &p.Wins, &p.Losses, &attacks, &tokens, &p.Power)
	if err != nil {
		return p, fmt.Errorf("storage: read the arena player: %w", err)
	}
	p.AttacksLeft = max(rules.Attacks-attacks, 0)
	p.TokensLeft = max(rules.Tokens-tokens, 0)
	return p, nil
}

// ArenaOpponents gives at most n players the player may attack, nearest to
// their rating first: those whose saved team's cards are all still held,
// never the player.
func (s *Store) ArenaOpponents(ctx context.Context, userID int64, n int) ([]ArenaPlayer, error) {
	rows, err := s.pool.Query(ctx, `
		WITH me AS (
			SELECT coalesce((SELECT rating FROM arena_players WHERE user_id = $1), $3) AS rating
		)
		SELECT t.user_id, coalesce(c.name, ''), coalesce(p.rating, $3), coalesce(p.wins, 0), coalesce(p.losses, 0), t.power
		FROM arena_teams t
		CROSS JOIN me
		LEFT JOIN arena_players p ON p.user_id = t.user_id
		LEFT JOIN card_players c ON c.user_id = t.user_id
		WHERE t.user_id <> $1
			AND (SELECT count(*) FROM card_owned o
				WHERE o.user_id = t.user_id AND o.card_id = ANY(t.front || t.back) AND o.count - o.melted >= 1)
				= cardinality(t.front || t.back)
		ORDER BY abs(coalesce(p.rating, $3) - me.rating), t.user_id
		LIMIT $2`, userID, max(n, 0), arenaStartRating)
	if err != nil {
		return nil, fmt.Errorf("storage: read arena opponents: %w", err)
	}
	return scanArenaPlayers(rows)
}

// ArenaTop gives the players who fought, the highest rating first, then the
// most wins.
func (s *Store) ArenaTop(ctx context.Context, limit int) ([]ArenaPlayer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.user_id, coalesce(c.name, ''), p.rating, p.wins, p.losses, coalesce(t.power, 0)
		FROM arena_players p
		LEFT JOIN card_players c ON c.user_id = p.user_id
		LEFT JOIN arena_teams t ON t.user_id = p.user_id
		ORDER BY p.rating DESC, p.wins DESC, p.user_id
		LIMIT $1`, max(limit, 0))
	if err != nil {
		return nil, fmt.Errorf("storage: read the arena table: %w", err)
	}
	return scanArenaPlayers(rows)
}

func scanArenaPlayers(rows pgx.Rows) ([]ArenaPlayer, error) {
	players, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ArenaPlayer, error) {
		var p ArenaPlayer
		err := row.Scan(&p.UserID, &p.Name, &p.Rating, &p.Wins, &p.Losses, &p.Power)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read arena players: %w", err)
	}
	return players, nil
}

// arenaRow is a player's arena row, its counters already those of the day.
type arenaRow struct {
	rating, attacks, tokensWon int
}

// RecordArenaBattle records the attacker's battle against the defender, in
// one transaction with both players' rows locked, the lower ID first: it
// takes one of the attacker's day's attacks, ErrArenaNoAttacks past the
// last, moves both ratings by Elo (never below 0), gives the attacker a
// token for a win while the day's arena tokens last, and keeps the battle
// with its seed. Defending changes only the defender's rating, wins and
// losses.
func (s *Store) RecordArenaBattle(ctx context.Context, attacker, defender, seed int64, attackerWon bool, rules ArenaRules) (ArenaOutcome, error) {
	var out ArenaOutcome
	if attacker == defender {
		return out, fmt.Errorf("storage: player %d attacking themselves", attacker)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := lockArenaPlayers(ctx, tx, rules.day(), attacker, defender)
		if err != nil {
			return err
		}
		a, d := rows[attacker], rows[defender]
		if a.attacks >= rules.Attacks {
			return ErrArenaNoAttacks
		}
		ratingA, ratingD := arenaElo(a.rating, d.rating, rules.K, attackerWon)
		out.RatingChange = ratingA - a.rating
		if attackerWon && a.tokensWon < rules.Tokens {
			tag, err := tx.Exec(ctx, `UPDATE card_players SET tokens = tokens + 1 WHERE user_id = $1`, attacker)
			if err != nil {
				return err
			}
			out.Token = tag.RowsAffected() == 1
		}
		if out.Token {
			a.tokensWon++
		}
		a.attacks++
		won := 0
		if attackerWon {
			won = 1
		}
		if _, err := tx.Exec(ctx, `
			UPDATE arena_players SET rating = $2, wins = wins + $3, losses = losses + 1 - $3,
				attacks_day = $4::date, attacks = $5, tokens_day = $4::date, tokens_won = $6
			WHERE user_id = $1`, attacker, ratingA, won, rules.day(), a.attacks, a.tokensWon); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE arena_players SET rating = $2, wins = wins + 1 - $3, losses = losses + $3 WHERE user_id = $1`,
			defender, ratingD, won); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO arena_battles (attacker, defender, seed, attacker_won, rating_change) VALUES ($1, $2, $3, $4, $5)`,
			attacker, defender, seed, attackerWon, out.RatingChange)
		out.AttacksLeft = max(rules.Attacks-a.attacks, 0)
		out.TokensLeft = max(rules.Tokens-a.tokensWon, 0)
		return err
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, ErrArenaNoAttacks):
		return ArenaOutcome{}, err
	}
	return ArenaOutcome{}, fmt.Errorf("storage: record an arena battle: %w", err)
}

// lockArenaPlayers makes the players' arena rows where missing and locks
// them, the lower ID first so that two players attacking each other at once
// wait for one another instead of deadlocking. Counters of a day before
// day read as 0.
func lockArenaPlayers(ctx context.Context, tx pgx.Tx, day string, a, b int64) (map[int64]arenaRow, error) {
	ids := []int64{min(a, b), max(a, b)}
	if _, err := tx.Exec(ctx, `
		INSERT INTO arena_players (user_id) VALUES ($1), ($2) ON CONFLICT (user_id) DO NOTHING`, ids[0], ids[1]); err != nil {
		return nil, err
	}
	rows := make(map[int64]arenaRow, len(ids))
	for _, id := range ids {
		var r arenaRow
		err := tx.QueryRow(ctx, `
			SELECT rating,
				CASE WHEN attacks_day = $2::date THEN attacks ELSE 0 END,
				CASE WHEN tokens_day = $2::date THEN tokens_won ELSE 0 END
			FROM arena_players WHERE user_id = $1 FOR UPDATE`, id, day).Scan(&r.rating, &r.attacks, &r.tokensWon)
		if err != nil {
			return nil, err
		}
		rows[id] = r
	}
	return rows, nil
}

// arenaElo gives the attacker's and defender's ratings after the battle:
// each moves by round(k × (score − expected)), the attacker expecting
// 1 / (1 + 10^((defender − attacker) / 400)), and neither goes below 0.
func arenaElo(attacker, defender, k int, attackerWon bool) (int, int) {
	expected := 1 / (1 + math.Pow(10, float64(defender-attacker)/400))
	score := 0.0
	if attackerWon {
		score = 1
	}
	change := int(math.Round(float64(k) * (score - expected)))
	return max(attacker+change, 0), max(defender-change, 0)
}
