package storage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// ShashkaPlayer is a player of the draughts Mini App, with the rating and
// record of their games against people; games against the bot count in
// neither.
type ShashkaPlayer struct {
	UserID int64
	Name   string
	// Language is their Telegram app's, as an IETF tag; often "".
	Language string
	Rating   int
	Wins     int
	Losses   int
	Draws    int
}

// Status of a shashka game.
const (
	ShashkaWaiting = "waiting"
	ShashkaPlaying = "playing"
	ShashkaOver    = "over"
)

// ShashkaGame is a game of the draughts Mini App.
type ShashkaGame struct {
	ID int64
	// Kind is "bot", "friend" or "random"; Level is the bot's, 0 otherwise.
	Kind  string
	Level int
	// WhiteID and BlackID are the players; the bot's side, and black while
	// an invite waits, is 0.
	WhiteID, BlackID int64
	// Position is the board and the side to move, as the rules encode it.
	Position string
	Status   string
	// LastPath and Captured are the last move's squares and what it took.
	LastPath, Captured []int
	// Version grows with each change; a change is saved only at the
	// version it was read at.
	Version    int
	InviteCode string
	Rated      bool
	// Deadline is when the side to move loses on time; zero without a clock.
	Deadline time.Time
	// Winner is 1 for white, -1 for black, 0 for a draw or a game not over.
	Winner int
	Reason string
	// WhiteChange and BlackChange are what a rated game did to the
	// players' ratings.
	WhiteChange, BlackChange int
	UpdatedAt                time.Time
}

// shashkaStartRating is a new player's rating, and shashkaK how far one
// game can move it.
const (
	shashkaStartRating = 1200
	shashkaK           = 32
)

var (
	ErrNoShashkaGame    = errors.New("storage: no such shashka game")
	ErrShashkaStale     = errors.New("storage: the shashka game changed since it was read")
	ErrShashkaTaken     = errors.New("storage: someone else took the shashka invite")
	ErrShashkaOwnInvite = errors.New("storage: the shashka invite is the player's own")
)

// SaveShashkaPlayer keeps the player's name and language as Telegram gives
// them now and returns their rating and record, a new player's at the start.
func (s *Store) SaveShashkaPlayer(ctx context.Context, userID int64, name, language string) (ShashkaPlayer, error) {
	p := ShashkaPlayer{UserID: userID}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO shashka_players (user_id, name, language) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET name = excluded.name, language = excluded.language, updated_at = now()
		RETURNING name, language, rating, wins, losses, draws`,
		userID, name, language).Scan(&p.Name, &p.Language, &p.Rating, &p.Wins, &p.Losses, &p.Draws)
	if err != nil {
		return ShashkaPlayer{}, fmt.Errorf("storage: save shashka player: %w", err)
	}
	return p, nil
}

// ShashkaPlayer is the player with this ID, at the start's rating with no
// name when they never opened the app.
func (s *Store) ShashkaPlayer(ctx context.Context, userID int64) (ShashkaPlayer, error) {
	p := ShashkaPlayer{UserID: userID, Rating: shashkaStartRating}
	err := s.pool.QueryRow(ctx, `SELECT name, language, rating, wins, losses, draws FROM shashka_players WHERE user_id = $1`, userID).
		Scan(&p.Name, &p.Language, &p.Rating, &p.Wins, &p.Losses, &p.Draws)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ShashkaPlayer{}, fmt.Errorf("storage: shashka player: %w", err)
	}
	return p, nil
}

// ShashkaTop is the limit best rated players who have played someone.
func (s *Store) ShashkaTop(ctx context.Context, limit int) ([]ShashkaPlayer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, name, rating, wins, losses, draws FROM shashka_players
		WHERE wins + losses + draws > 0
		ORDER BY rating DESC, wins DESC, user_id
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: shashka top: %w", err)
	}
	top, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ShashkaPlayer, error) {
		var p ShashkaPlayer
		return p, row.Scan(&p.UserID, &p.Name, &p.Rating, &p.Wins, &p.Losses, &p.Draws)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: shashka top: %w", err)
	}
	return top, nil
}

const shashkaGameColumns = `id, kind, level, white_id, black_id, position, status, last_path, captured, version,
	coalesce(invite_code, ''), rated, deadline, winner, reason, white_change, black_change, updated_at`

func scanShashkaGame(row pgx.Row) (ShashkaGame, error) {
	var g ShashkaGame
	var deadline *time.Time
	err := row.Scan(&g.ID, &g.Kind, &g.Level, &g.WhiteID, &g.BlackID, &g.Position, &g.Status, &g.LastPath, &g.Captured, &g.Version,
		&g.InviteCode, &g.Rated, &deadline, &g.Winner, &g.Reason, &g.WhiteChange, &g.BlackChange, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShashkaGame{}, ErrNoShashkaGame
	}
	if deadline != nil {
		g.Deadline = *deadline
	}
	return g, err
}

// deadlineOf is the column's value for a deadline, NULL for none.
func deadlineOf(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// inviteOf is the column's value for an invite code, NULL for none, which
// the column's uniqueness lets many games have.
func inviteOf(code string) *string {
	if code == "" {
		return nil
	}
	return &code
}

// ShashkaGame is the game with this ID, ErrNoShashkaGame when none is.
func (s *Store) ShashkaGame(ctx context.Context, id int64) (ShashkaGame, error) {
	g, err := scanShashkaGame(s.pool.QueryRow(ctx, `SELECT `+shashkaGameColumns+` FROM shashka_games WHERE id = $1`, id))
	if err != nil && !errors.Is(err, ErrNoShashkaGame) {
		return ShashkaGame{}, fmt.Errorf("storage: shashka game: %w", err)
	}
	return g, err
}

// OpenShashkaGame is the player's latest game that is not over: an invite
// they sent, or a game in play.
func (s *Store) OpenShashkaGame(ctx context.Context, userID int64) (ShashkaGame, bool, error) {
	g, err := scanShashkaGame(s.pool.QueryRow(ctx, `
		SELECT `+shashkaGameColumns+` FROM shashka_games
		WHERE status <> 'over' AND (white_id = $1 OR black_id = $1)
		ORDER BY updated_at DESC LIMIT 1`, userID))
	switch {
	case errors.Is(err, ErrNoShashkaGame):
		return ShashkaGame{}, false, nil
	case err != nil:
		return ShashkaGame{}, false, fmt.Errorf("storage: open shashka game: %w", err)
	}
	return g, true, nil
}

// CreateShashkaGame keeps a new game and returns it with its ID.
func (s *Store) CreateShashkaGame(ctx context.Context, g ShashkaGame) (ShashkaGame, error) {
	created, err := insertShashkaGame(ctx, s.pool, g)
	if err != nil {
		return ShashkaGame{}, fmt.Errorf("storage: create shashka game: %w", err)
	}
	return created, nil
}

func insertShashkaGame(ctx context.Context, db queryRower, g ShashkaGame) (ShashkaGame, error) {
	return scanShashkaGame(db.QueryRow(ctx, `
		INSERT INTO shashka_games (kind, level, white_id, black_id, position, status, invite_code, rated, deadline)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+shashkaGameColumns,
		g.Kind, g.Level, g.WhiteID, g.BlackID, g.Position, g.Status, inviteOf(g.InviteCode), g.Rated, deadlineOf(g.Deadline)))
}

// JoinShashkaGame has the player take black in the invite with this code,
// which starts it, with the first move's deadline. ErrNoShashkaGame is no
// invite of that code, ErrShashkaTaken one someone else took or that was
// given up, and ErrShashkaOwnInvite the player's own.
func (s *Store) JoinShashkaGame(ctx context.Context, code string, userID int64, deadline time.Time) (ShashkaGame, error) {
	var joined ShashkaGame
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		g, err := scanShashkaGame(tx.QueryRow(ctx, `SELECT `+shashkaGameColumns+` FROM shashka_games WHERE invite_code = $1 FOR UPDATE`, code))
		switch {
		case err != nil:
			return err
		case g.WhiteID == userID:
			return ErrShashkaOwnInvite
		case g.Status != ShashkaWaiting:
			return ErrShashkaTaken
		}
		joined, err = scanShashkaGame(tx.QueryRow(ctx, `
			UPDATE shashka_games SET black_id = $2, status = 'playing', deadline = $3, version = version + 1, updated_at = now()
			WHERE id = $1
			RETURNING `+shashkaGameColumns,
			g.ID, userID, deadlineOf(deadline)))
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNoShashkaGame) || errors.Is(err, ErrShashkaTaken) || errors.Is(err, ErrShashkaOwnInvite) {
			return ShashkaGame{}, err
		}
		return ShashkaGame{}, fmt.Errorf("storage: join shashka game: %w", err)
	}
	return joined, nil
}

// UpdateShashkaGame saves the game as changed from g.Version, which it
// must still be at, else ErrShashkaStale. A rated game it ends moves both
// players' ratings, by Elo, and their records, in the same transaction; the
// game returned has those changes and the next version.
func (s *Store) UpdateShashkaGame(ctx context.Context, g ShashkaGame) (ShashkaGame, error) {
	var saved ShashkaGame
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if g.Status == ShashkaOver && g.Rated {
			var err error
			if g.WhiteChange, g.BlackChange, err = rateShashka(ctx, tx, g); err != nil {
				return err
			}
		}
		var err error
		saved, err = scanShashkaGame(tx.QueryRow(ctx, `
			UPDATE shashka_games SET position = $3, status = $4, last_path = $5, captured = $6, deadline = $7,
				winner = $8, reason = $9, white_change = $10, black_change = $11, version = version + 1, updated_at = now()
			WHERE id = $1 AND version = $2
			RETURNING `+shashkaGameColumns,
			g.ID, g.Version, g.Position, g.Status, g.LastPath, g.Captured, deadlineOf(g.Deadline),
			g.Winner, g.Reason, g.WhiteChange, g.BlackChange))
		if errors.Is(err, ErrNoShashkaGame) {
			// Rolling back undoes the ratings too.
			return ErrShashkaStale
		}
		return err
	})
	if err != nil {
		if errors.Is(err, ErrShashkaStale) {
			return ShashkaGame{}, err
		}
		return ShashkaGame{}, fmt.Errorf("storage: update shashka game: %w", err)
	}
	return saved, nil
}

// rateShashka moves the players' ratings for how the game ended and counts
// it in their records; it returns white's change and black's.
func rateShashka(ctx context.Context, tx pgx.Tx, g ShashkaGame) (int, int, error) {
	if _, err := tx.Exec(ctx, `
		INSERT INTO shashka_players (user_id) VALUES ($1), ($2) ON CONFLICT (user_id) DO NOTHING`,
		g.WhiteID, g.BlackID); err != nil {
		return 0, 0, err
	}
	var white, black int
	if err := tx.QueryRow(ctx, `
		SELECT max(rating) FILTER (WHERE user_id = $1), max(rating) FILTER (WHERE user_id = $2)
		FROM (SELECT user_id, rating FROM shashka_players WHERE user_id IN ($1, $2) ORDER BY user_id FOR UPDATE) players`,
		g.WhiteID, g.BlackID).Scan(&white, &black); err != nil {
		return 0, 0, err
	}
	score := 0.5 + float64(g.Winner)/2 // white's: 1, 0.5 or 0
	change := eloChange(white, black, score)
	for _, side := range []struct {
		userID int64
		change int
		result int
	}{{g.WhiteID, change, g.Winner}, {g.BlackID, -change, -g.Winner}} {
		if _, err := tx.Exec(ctx, `
			UPDATE shashka_players SET rating = rating + $2,
				wins = wins + (CASE WHEN $3 > 0 THEN 1 ELSE 0 END),
				losses = losses + (CASE WHEN $3 < 0 THEN 1 ELSE 0 END),
				draws = draws + (CASE WHEN $3 = 0 THEN 1 ELSE 0 END),
				updated_at = now()
			WHERE user_id = $1`,
			side.userID, side.change, side.result); err != nil {
			return 0, 0, err
		}
	}
	return change, -change, nil
}

// eloChange is how far a game moves the first player's rating, against the
// second's, for their score in it: 1 a win, 0.5 a draw, 0 a loss.
func eloChange(rating, opponent int, score float64) int {
	expected := 1 / (1 + math.Pow(10, float64(opponent-rating)/400))
	return int(math.Round(shashkaK * (score - expected)))
}

// DeleteShashkaInvite takes back the player's invite no one took.
func (s *Store) DeleteShashkaInvite(ctx context.Context, id, userID int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM shashka_games WHERE id = $1 AND white_id = $2 AND status = 'waiting'`, id, userID); err != nil {
		return fmt.Errorf("storage: delete shashka invite: %w", err)
	}
	return nil
}

// QueueShashka pairs the player with whoever else waits for a random
// opponent, the nearest in rating first, and keeps the game newGame makes
// for the two; until someone does, the player waits. A player already in a
// game gets that one, as the other side's asking may have paired them. A wait not renewed
// within fresh is given up, as its player stopped asking. Every pairing
// holds one lock, so two players asking at once cannot each take the
// other into a game of their own.
func (s *Store) QueueShashka(ctx context.Context, userID int64, rating int, fresh time.Duration, newGame func(first, second int64) ShashkaGame) (ShashkaGame, bool, error) {
	var game ShashkaGame
	var paired bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('shashka_queue'))`); err != nil {
			return err
		}
		// Someone who was paired while asking again has that game.
		var err error
		game, err = scanShashkaGame(tx.QueryRow(ctx, `
			SELECT `+shashkaGameColumns+` FROM shashka_games
			WHERE status = 'playing' AND (white_id = $1 OR black_id = $1)
			ORDER BY updated_at DESC LIMIT 1`, userID))
		if err == nil {
			paired = true
			_, err = tx.Exec(ctx, `DELETE FROM shashka_queue WHERE user_id = $1`, userID)
			return err
		}
		if !errors.Is(err, ErrNoShashkaGame) {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM shashka_queue WHERE joined_at < now() - make_interval(secs => $1)`, fresh.Seconds()); err != nil {
			return err
		}
		var opponent int64
		err = tx.QueryRow(ctx, `
			SELECT user_id FROM shashka_queue WHERE user_id <> $1
			ORDER BY abs(rating - $2), joined_at LIMIT 1`, userID, rating).Scan(&opponent)
		if errors.Is(err, pgx.ErrNoRows) {
			_, err = tx.Exec(ctx, `
				INSERT INTO shashka_queue (user_id, rating) VALUES ($1, $2)
				ON CONFLICT (user_id) DO UPDATE SET rating = excluded.rating, joined_at = now()`, userID, rating)
			return err
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM shashka_queue WHERE user_id IN ($1, $2)`, userID, opponent); err != nil {
			return err
		}
		game, err = insertShashkaGame(ctx, tx, newGame(opponent, userID))
		paired = err == nil
		return err
	})
	if err != nil {
		return ShashkaGame{}, false, fmt.Errorf("storage: queue for shashka: %w", err)
	}
	return game, paired, nil
}

// LeaveShashkaQueue stops the player waiting for a random opponent.
func (s *Store) LeaveShashkaQueue(ctx context.Context, userID int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM shashka_queue WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("storage: leave shashka queue: %w", err)
	}
	return nil
}

// InShashkaQueue reports a player waiting for a random opponent, within
// fresh of their last asking.
func (s *Store) InShashkaQueue(ctx context.Context, userID int64, fresh time.Duration) (bool, error) {
	var waiting bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM shashka_queue WHERE user_id = $1 AND joined_at >= now() - make_interval(secs => $2))`,
		userID, fresh.Seconds()).Scan(&waiting)
	if err != nil {
		return false, fmt.Errorf("storage: in shashka queue: %w", err)
	}
	return waiting, nil
}

// DeleteOldShashkaGames deletes games not changed since before: those
// over, and invites no one took. A game in play is kept, as a deadline
// ends it when it is next looked at.
func (s *Store) DeleteOldShashkaGames(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM shashka_games WHERE status <> 'playing' AND updated_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("storage: delete old shashka games: %w", err)
	}
	return tag.RowsAffected(), nil
}
