package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrCardFunds is returned by OpenCards when the player's tokens and
// crystals do not pay for the cards: how many they have is in
// CardFundsError.
var ErrCardFunds = errors.New("storage: too few tokens and crystals")

// CardFundsError says the player's tokens and crystals, and the crystals the
// cards past their tokens would have cost.
type CardFundsError struct{ Tokens, Shards, Need int }

func (e *CardFundsError) Error() string { return ErrCardFunds.Error() }
func (e *CardFundsError) Unwrap() error { return ErrCardFunds }

// CardOpening is the cards a player opened at once, and their tokens,
// crystals and points after.
type CardOpening struct {
	Draws  []CardDraw
	Tokens int
	Shards int
	Points int64
	// Given is the day's tokens, when this opening was the day's first.
	Given int
}

// OpenCards opens n cards for the player: each takes a token while they
// have one, and cost crystals once they have none. The day's tokens are
// given first, at the day's first opening. With too few to pay for all n it
// opens none and returns a *CardFundsError. A player who turned on the card
// reminders, and has spent their last token, gets one for the next
// midnight.
func (s *Store) OpenCards(ctx context.Context, userID int64, name string, n int, roll CardRoll, cost int, day CardDay, now time.Time) (CardOpening, error) {
	var open CardOpening
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		if open.Given, err = giveDayTokens(ctx, tx, userID, day, player); err != nil {
			return err
		}
		player.tokens += open.Given
		tokens := min(player.tokens, n)
		shards := (n - tokens) * cost
		if shards > player.shards {
			return &CardFundsError{Tokens: player.tokens, Shards: player.shards, Need: shards}
		}
		for range n {
			draw, err := giveCard(ctx, tx, userID, name, "", roll, player)
			if err != nil {
				return err
			}
			open.Draws = append(open.Draws, draw)
		}
		err = tx.QueryRow(ctx, `
			UPDATE card_players SET tokens = tokens - $2, shards = shards - $3, last_draw_at = $4
			WHERE user_id = $1 RETURNING tokens, shards, points`, userID, tokens, shards, now).
			Scan(&open.Tokens, &open.Shards, &open.Points)
		if err != nil || open.Tokens > 0 {
			return err
		}
		return remindAtNextDay(ctx, tx, userID, day.Start.AddDate(0, 0, 1))
	})
	var few *CardFundsError
	if errors.As(err, &few) {
		return CardOpening{}, few
	}
	if err != nil {
		return CardOpening{}, fmt.Errorf("storage: open cards: %w", err)
	}
	return open, nil
}

// GiveDayTokens gives the player the day's tokens if they had none today,
// and says how many it gave; it makes the player's row when there is none.
func (s *Store) GiveDayTokens(ctx context.Context, userID int64, name string, day CardDay) (int, error) {
	var given int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		given, err = giveDayTokens(ctx, tx, userID, day, player)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("storage: give the day's tokens: %w", err)
	}
	return given, nil
}

func giveDayTokens(ctx context.Context, tx pgx.Tx, userID int64, day CardDay, player cardPlayerRow) (int, error) {
	if day.PerDay <= 0 || (player.freeDay != nil && player.freeDay.Equal(day.Start)) {
		return 0, nil
	}
	_, err := tx.Exec(ctx, `UPDATE card_players SET tokens = tokens + $2, free_day = $3, free_used = 0 WHERE user_id = $1`,
		userID, day.PerDay, day.Start)
	return day.PerDay, err
}

// remindAtNextDay moves a player's card reminder, when they asked to be
// reminded of every card, to when the next day's tokens come.
func remindAtNextDay(ctx context.Context, tx pgx.Tx, userID int64, next time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO reminders (chat_id, user_id, user_name, kind, language, due_at)
		SELECT remind_chat_id, user_id, name, 'card', remind_language, $2
		FROM card_players WHERE user_id = $1 AND remind_chat_id IS NOT NULL
		ON CONFLICT (user_id) WHERE kind = 'card' AND claimed_at IS NULL DO UPDATE SET
			chat_id = EXCLUDED.chat_id, user_name = EXCLUDED.user_name, message_id = 0,
			language = EXCLUDED.language, due_at = EXCLUDED.due_at`, userID, next)
	return err
}
