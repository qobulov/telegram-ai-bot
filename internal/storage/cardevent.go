package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrCardEventOver is an event pull asked for once the event has ended.
var ErrCardEventOver = errors.New("storage: the card event is over")

// CardTokensError is a player with too few tokens for an event's pulls:
// they hold Tokens and the pulls take Need.
type CardTokensError struct{ Tokens, Need int }

func (e *CardTokensError) Error() string { return "storage: too few tokens" }

// CardEvent is a card offered for a time: each pull costs Price tokens, Roll
// picks its rarity, and a Mythic is the event's Card when Pick says so, any
// other Mythic when not. The Pity-th pull in a row without Card gives it.
// A player holds Card once at most: one who has it gets other cards, the
// pity no longer counting.
type CardEvent struct {
	Card  int64
	Ends  time.Time
	Pity  int
	Price int
	Roll  CardRoll
	Pick  func() bool
}

// EventOpening is an opening of event pulls, and the pulls in a row since
// the event's card after it.
type EventOpening struct {
	CardOpening
	Pulls int
}

// OpenEventCards pulls n cards of the event for the player's tokens, the
// day's tokens given first; each counts as any draw does, in points, cards
// and the waits for a rare card. Once ev ends it returns ErrCardEventOver,
// and with too few tokens it pulls none and returns a *CardTokensError.
func (s *Store) OpenEventCards(ctx context.Context, userID int64, name string, n int, ev CardEvent, day CardDay, now time.Time) (EventOpening, error) {
	if !now.Before(ev.Ends) {
		return EventOpening{}, ErrCardEventOver
	}
	var open EventOpening
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		if open.Given, err = giveDayTokens(ctx, tx, userID, day, player); err != nil {
			return err
		}
		player.tokens += open.Given
		if price := n * ev.Price; price > player.tokens {
			return &CardTokensError{Tokens: player.tokens, Need: price}
		}
		var held bool
		if err := tx.QueryRow(ctx, `
			SELECT event_pulls, EXISTS (SELECT 1 FROM card_owned WHERE user_id = $1 AND card_id = $2)
			FROM card_players WHERE user_id = $1`, userID, ev.Card).Scan(&open.Pulls, &held); err != nil {
			return err
		}
		for range n {
			card, sure, err := eventCard(ctx, tx, ev, open.Pulls+1, player, held)
			if err != nil {
				return err
			}
			draw, err := addCard(ctx, tx, userID, name, card, sure)
			if err != nil {
				return err
			}
			open.Draws = append(open.Draws, draw)
			open.Pulls++
			if card.ID == ev.Card {
				held = true
			}
			if held {
				open.Pulls = 0
			}
		}
		err = tx.QueryRow(ctx, `
			UPDATE card_players SET tokens = tokens - $2, event_pulls = $3, last_draw_at = $4
			WHERE user_id = $1 RETURNING tokens, shards, points`, userID, n*ev.Price, open.Pulls, now).
			Scan(&open.Tokens, &open.Shards, &open.Points)
		if err != nil || open.Tokens > 0 {
			return err
		}
		return remindAtNextDay(ctx, tx, userID, day.Next())
	})
	var few *CardTokensError
	if errors.As(err, &few) {
		return EventOpening{}, few
	}
	if err != nil {
		return EventOpening{}, fmt.Errorf("storage: open event cards: %w", err)
	}
	return open, nil
}

// eventCard is the card of an event's pull, the pull-th in a row without
// the event's card, and whether the pity made it sure; a player who holds
// the event's card already gets another.
func eventCard(ctx context.Context, tx pgx.Tx, ev CardEvent, pull int, player cardPlayerRow, held bool) (Card, bool, error) {
	if !held && pull >= ev.Pity {
		c, err := catalogCard(ctx, tx, ev.Card)
		return c, true, err
	}
	rarity, raised := ev.Roll(player.sinceLegendary, player.sinceMythic)
	if rarity == mythicRarity && !held && ev.Pick() {
		c, err := catalogCard(ctx, tx, ev.Card)
		return c, raised, err
	}
	c, err := randomCard(ctx, tx, rarity, "", ev.Card)
	return c, raised, err
}

// catalogCard is the card of the catalog by its ID.
func catalogCard(ctx context.Context, tx pgx.Tx, id int64) (Card, error) {
	var c Card
	err := tx.QueryRow(ctx, `
		SELECT id, name, anime, image_url, favourites, rarity, attack, health, value, collection
		FROM card_catalog WHERE id = $1`, id).
		Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
	return c, err
}

// CardEventPulls is the player's event pulls in a row since the event's
// card, and the event's card from the catalog.
func (s *Store) CardEventPulls(ctx context.Context, userID, card int64) (int, Card, error) {
	var pulls int
	var c Card
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT coalesce((SELECT event_pulls FROM card_players WHERE user_id = $1), 0)`, userID).Scan(&pulls); err != nil {
			return err
		}
		var err error
		c, err = catalogCard(ctx, tx, card)
		return err
	})
	if err != nil {
		return 0, Card{}, fmt.Errorf("storage: read a player's event pulls: %w", err)
	}
	return pulls, c, nil
}
