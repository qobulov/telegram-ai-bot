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
	// ErrArenaNoTickets is an attack with no ticket left.
	ErrArenaNoTickets = errors.New("storage: no arena tickets left")
)

// arenaStartRating is a player's rating before their first battle.
const arenaStartRating = 1000

// ArenaRules are the arena's limits: Tickets a player holds at most, one
// coming back every TicketEvery, each attack taking one; Tokens they win a
// day and the Elo K. Day is the start of today in Tashkent; its date, in
// its own location, is the day the tokens count. Now is the time the
// tickets are counted at. CoinsWin and CoinsLoss are the arena coins an
// attack gives the attacker, won or lost.
type ArenaRules struct {
	Tickets, Tokens, K  int
	CoinsWin, CoinsLoss int
	TicketEvery         time.Duration
	Day, Now            time.Time
}

// day is the date the rules' counters count, as Postgres reads a DATE.
func (r ArenaRules) day() string { return r.Day.Format(time.DateOnly) }

// tickets are the tickets a player holds now, of stored counted at at (nil
// for a full stock), and when the hour toward the next one started.
func (r ArenaRules) tickets(stored int, at *time.Time) (held int, since time.Time) {
	if at == nil || r.TicketEvery <= 0 {
		return r.Tickets, r.Now
	}
	gained := max(int(r.Now.Sub(*at)/r.TicketEvery), 0)
	held = min(stored+gained, r.Tickets)
	if held >= r.Tickets {
		return r.Tickets, r.Now
	}
	return held, at.Add(time.Duration(gained) * r.TicketEvery)
}

// nextTicket is the wait for the next ticket, 0 with the stock full.
func (r ArenaRules) nextTicket(held int, since time.Time) time.Duration {
	if held >= r.Tickets {
		return 0
	}
	return max(since.Add(r.TicketEvery).Sub(r.Now), 0)
}

// ArenaPlayer is a player as the arena knows them: their name, rating,
// wins and losses, the tickets they hold and the wait for the next, the
// day's tokens left, their arena coins and the tokens they bought in the
// arena's shop today (all five counted only by ArenaPlayerOf), and their
// saved team's power, 0 without one.
type ArenaPlayer struct {
	UserID                                           int64
	Name                                             string
	Rating, Wins, Losses, Tickets, TokensLeft, Power int
	Coins, ShopTokens                                int
	NextTicket                                       time.Duration
}

// ArenaOutcome is what an attack changed for the attacker: their rating,
// whether it won them a token, the tickets left and the wait for the next,
// the day's tokens left, and the coins it gave and the coins after.
type ArenaOutcome struct {
	RatingChange        int
	Token               bool
	Tickets, TokensLeft int
	CoinsWon, Coins     int
	NextTicket          time.Duration
}

// ArenaBattle is a battle as one of its players sees it: when, whether they
// attacked, the other player, whether they won and their rating's change.
type ArenaBattle struct {
	At           time.Time
	Attacked     bool
	OpponentID   int64
	Opponent     string
	Won          bool
	RatingChange int
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

// ArenaPlayerOf reads the player, with the tickets they hold now and the
// day's tokens left; one who never fought is at the start rating with a
// full stock and the whole day left.
func (s *Store) ArenaPlayerOf(ctx context.Context, userID int64, rules ArenaRules) (ArenaPlayer, error) {
	p := ArenaPlayer{UserID: userID}
	var stored, tokens int
	var at *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT coalesce(c.name, ''), coalesce(p.rating, $3), coalesce(p.wins, 0), coalesce(p.losses, 0),
			coalesce(p.tickets, 0), p.tickets_at,
			CASE WHEN p.tokens_day = $2::date THEN p.tokens_won ELSE 0 END,
			coalesce(t.power, 0), coalesce(p.coins, 0),
			CASE WHEN p.shop_day = $2::date THEN p.shop_tokens ELSE 0 END
		FROM (SELECT $1::bigint AS user_id) u
		LEFT JOIN arena_players p USING (user_id)
		LEFT JOIN card_players c USING (user_id)
		LEFT JOIN arena_teams t USING (user_id)`, userID, rules.day(), arenaStartRating).
		Scan(&p.Name, &p.Rating, &p.Wins, &p.Losses, &stored, &at, &tokens, &p.Power, &p.Coins, &p.ShopTokens)
	if err != nil {
		return p, fmt.Errorf("storage: read the arena player: %w", err)
	}
	held, since := rules.tickets(stored, at)
	p.Tickets, p.NextTicket = held, rules.nextTicket(held, since)
	p.TokensLeft = max(rules.Tokens-tokens, 0)
	return p, nil
}

// ArenaBattles are the player's latest battles, attacking or attacked, at
// most limit, the newest first.
func (s *Store) ArenaBattles(ctx context.Context, userID int64, limit int) ([]ArenaBattle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.at, b.attacked, b.opponent, coalesce(c.name, ''), b.won, b.change
		FROM (
			SELECT id, at, true AS attacked, defender AS opponent, attacker_won AS won, rating_change AS change
			FROM arena_battles WHERE attacker = $1
			UNION ALL
			SELECT id, at, false, attacker, NOT attacker_won, defender_change
			FROM arena_battles WHERE defender = $1
			ORDER BY id DESC
			LIMIT $2
		) b
		LEFT JOIN card_players c ON c.user_id = b.opponent
		ORDER BY b.id DESC`, userID, max(limit, 0))
	if err != nil {
		return nil, fmt.Errorf("storage: read arena battles: %w", err)
	}
	battles, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ArenaBattle, error) {
		var b ArenaBattle
		err := row.Scan(&b.At, &b.Attacked, &b.OpponentID, &b.Opponent, &b.Won, &b.RatingChange)
		return b, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read arena battles: %w", err)
	}
	return battles, nil
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

// arenaRow is a player's arena row, its tokens already those of the day
// and its tickets as stored.
type arenaRow struct {
	rating, tickets, tokensWon int
	ticketsAt                  *time.Time
}

// RecordArenaBattle records the attacker's battle against the defender, in
// one transaction with both players' rows locked, the lower ID first: it
// takes one of the attacker's tickets, ErrArenaNoTickets with none, moves
// the ratings by Elo (arenaElo: a defender who holds gets +0), gives the attacker a token for a
// win while the day's arena tokens last, and keeps the battle with its
// seed and both players' rating changes. Defending changes only the
// defender's rating, wins and losses, and takes no ticket. The attacker
// gets the rules' coins, won or lost. A win counts toward quest for the
// attacker.
func (s *Store) RecordArenaBattle(ctx context.Context, attacker, defender, seed int64, attackerWon bool, rules ArenaRules, quest CardQuest) (ArenaOutcome, error) {
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
		held, since := rules.tickets(a.tickets, a.ticketsAt)
		if held < 1 {
			return ErrArenaNoTickets
		}
		held--
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
		won, coins := 0, rules.CoinsLoss
		if attackerWon {
			won, coins = 1, rules.CoinsWin
		}
		if err := countQuest(ctx, tx, attacker, quest, won); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			UPDATE arena_players SET rating = $2, wins = wins + $3, losses = losses + 1 - $3,
				tickets = $5, tickets_at = $6, tokens_day = $4::date, tokens_won = $7, coins = coins + $8
			WHERE user_id = $1 RETURNING coins`, attacker, ratingA, won, rules.day(), held, since, a.tokensWon, coins).Scan(&out.Coins); err != nil {
			return err
		}
		out.CoinsWon = coins
		if _, err := tx.Exec(ctx, `
			UPDATE arena_players SET rating = $2, wins = wins + 1 - $3, losses = losses + $3 WHERE user_id = $1`,
			defender, ratingD, won); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO arena_battles (attacker, defender, seed, attacker_won, rating_change, defender_change)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			attacker, defender, seed, attackerWon, out.RatingChange, ratingD-d.rating)
		out.Tickets, out.NextTicket = held, rules.nextTicket(held, since)
		out.TokensLeft = max(rules.Tokens-a.tokensWon, 0)
		return err
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, ErrArenaNoTickets):
		return ArenaOutcome{}, err
	}
	return ArenaOutcome{}, fmt.Errorf("storage: record an arena battle: %w", err)
}

// lockArenaPlayers makes the players' arena rows where missing and locks
// them, the lower ID first so that two players attacking each other at once
// wait for one another instead of deadlocking. Tokens won on a day before
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
			SELECT rating, tickets, tickets_at,
				CASE WHEN tokens_day = $2::date THEN tokens_won ELSE 0 END
			FROM arena_players WHERE user_id = $1 FOR UPDATE`, id, day).Scan(&r.rating, &r.tickets, &r.ticketsAt, &r.tokensWon)
		if err != nil {
			return nil, err
		}
		rows[id] = r
	}
	return rows, nil
}

// arenaElo gives the attacker's and defender's ratings after the battle:
// the attacker moves by round(k × (score − expected)), expecting
// 1 / (1 + 10^((defender − attacker) / 400)). A defender who loses drops by
// the same amount; one who holds gains nothing (+0): rating is earned by
// attacking. Neither goes below 0.
func arenaElo(attacker, defender, k int, attackerWon bool) (int, int) {
	expected := 1 / (1 + math.Pow(10, float64(defender-attacker)/400))
	score := 0.0
	if attackerWon {
		score = 1
	}
	change := int(math.Round(float64(k) * (score - expected)))
	if !attackerWon {
		return max(attacker+change, 0), defender
	}
	return max(attacker+change, 0), max(defender-change, 0)
}
