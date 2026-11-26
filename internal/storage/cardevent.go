package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrCardEventOver is an event pull asked for once the event has ended.
var ErrCardEventOver = errors.New("storage: the card event is over")

// CardTokensError is a player with too few tokens for an event's pulls:
// they hold Tokens and the pulls take Need.
type CardTokensError struct{ Tokens, Need int }

func (e *CardTokensError) Error() string { return "storage: too few tokens" }

// CardEvent is cards offered for a time: each pull costs Price tokens, Roll
// picks its rarity, and a Mythic is one of the event's Cards when Pick says
// so, any other Mythic when not. The Pity-th pull in a row without one of
// Cards gives one, of those the player lacks if any; Key names the count.
// A Once event's card is held once at most: one who has it gets other cards,
// the pity no longer counting. An AtRarity event's cards come at the rarity
// rolled, any of them, not only a Mythic: Pick says whether a pull of a
// rarity it has cards of is one of them. Sure, when given, are the cards
// the pity counts and gives, else Cards. Extra are cards no draw gives that
// the event's pulls give at their rarity like any other.
type CardEvent struct {
	Key      string
	Cards    []int64
	Once     bool
	AtRarity bool
	Sure     []int64
	Extra    []int64
	Ends     time.Time
	Pity     int
	Price    int
	Roll     CardRoll
	Pick     func() bool
}

// sure are the cards the event's pity counts and gives.
func (ev CardEvent) sure() []int64 {
	if len(ev.Sure) > 0 {
		return ev.Sure
	}
	return ev.Cards
}

// fireflyEvent is the first event's key: it counts its pulls in
// card_players.event_pulls, where it began; later events count theirs in
// card_event_pulls.
const fireflyEvent = "firefly"

// eventPulls is the player's pulls of the event in a row without its cards.
func eventPulls(ctx context.Context, tx pgx.Tx, userID int64, key string) (int, error) {
	var pulls int
	if key == fireflyEvent {
		err := tx.QueryRow(ctx, `SELECT coalesce((SELECT event_pulls FROM card_players WHERE user_id = $1), 0)`, userID).Scan(&pulls)
		return pulls, err
	}
	err := tx.QueryRow(ctx, `SELECT coalesce((SELECT pulls FROM card_event_pulls WHERE user_id = $1 AND event = $2), 0)`, userID, key).Scan(&pulls)
	return pulls, err
}

// saveEventPulls keeps the player's pulls of the event in a row.
func saveEventPulls(ctx context.Context, tx pgx.Tx, userID int64, key string, pulls int) error {
	if key == fireflyEvent {
		_, err := tx.Exec(ctx, `UPDATE card_players SET event_pulls = $2 WHERE user_id = $1`, userID, pulls)
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO card_event_pulls (user_id, event, pulls) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, event) DO UPDATE SET pulls = $3`, userID, key, pulls)
	return err
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
// The n pulls count toward quest.
func (s *Store) OpenEventCards(ctx context.Context, userID int64, name string, n int, ev CardEvent, day CardDay, now time.Time, quest CardQuest) (EventOpening, error) {
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
		if open.Pulls, err = eventPulls(ctx, tx, userID, ev.Key); err != nil {
			return err
		}
		held := false
		if ev.Once {
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM card_owned WHERE user_id = $1 AND card_id = ANY($2))`, userID, ev.Cards).Scan(&held); err != nil {
				return err
			}
		}
		for range n {
			card, sure, err := eventCard(ctx, tx, userID, ev, open.Pulls+1, player, held)
			if err != nil {
				return err
			}
			draw, err := addCard(ctx, tx, userID, name, card, sure)
			if err != nil {
				return err
			}
			open.Draws = append(open.Draws, draw)
			open.Pulls++
			if slices.Contains(ev.sure(), card.ID) {
				open.Pulls = 0
				held = ev.Once
			}
			if held {
				open.Pulls = 0
			}
		}
		if err := countQuest(ctx, tx, userID, quest, n); err != nil {
			return err
		}
		if err := saveEventPulls(ctx, tx, userID, ev.Key, open.Pulls); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			UPDATE card_players SET tokens = tokens - $2, last_draw_at = $3
			WHERE user_id = $1 RETURNING tokens, shards, points`, userID, n*ev.Price, now).
			Scan(&open.Tokens, &open.Shards, &open.Points)
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
// the event's cards, and whether the pity made it sure; a player who holds a
// Once event's card already gets another.
func eventCard(ctx context.Context, tx pgx.Tx, userID int64, ev CardEvent, pull int, player cardPlayerRow, held bool) (Card, bool, error) {
	if !held && pull >= ev.Pity {
		c, err := pickEventCard(ctx, tx, userID, ev.sure(), -1)
		return c, true, err
	}
	rarity, raised := ev.Roll(player.sinceLegendary, player.sinceMythic)
	if (ev.AtRarity || rarity == mythicRarity) && !held && ev.Pick() {
		of := -1
		if ev.AtRarity {
			of = rarity
		}
		c, err := pickEventCard(ctx, tx, userID, ev.Cards, of)
		if !errors.Is(err, pgx.ErrNoRows) {
			return c, raised, err
		}
	}
	c, err := randomEventCard(ctx, tx, rarity, ev.Cards, ev.Extra)
	return c, raised, err
}

// randomEventCard is a card of rarity at random, as randomCard picks one,
// but that extra, cards no draw gives, may be among them; never one of but.
func randomEventCard(ctx context.Context, tx pgx.Tx, rarity int, but, extra []int64) (Card, error) {
	if len(extra) == 0 {
		return randomCard(ctx, tx, rarity, "", but)
	}
	var c Card
	err := tx.QueryRow(ctx, `
		SELECT id, name, anime, image_url, favourites, rarity, attack, health, value, collection
		FROM card_catalog WHERE rarity = $1 AND id <> ALL($2::bigint[]) AND (id <> ALL($3::bigint[]) OR id = ANY($4::bigint[]))
		ORDER BY random() LIMIT 1`, rarity, append([]int64{}, but...), undrawnCardIDs(), extra).
		Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
	return c, err
}

// pickEventCard is one of an event's cards at random, of rarity or of any
// when it is negative, of those the player lacks while there are, the
// rarest first; pgx.ErrNoRows when the event has none of that rarity.
func pickEventCard(ctx context.Context, tx pgx.Tx, userID int64, cards []int64, rarity int) (Card, error) {
	var c Card
	err := tx.QueryRow(ctx, `
		SELECT c.id, c.name, c.anime, c.image_url, c.favourites, c.rarity, c.attack, c.health, c.value, c.collection
		FROM card_catalog c WHERE c.id = ANY($1) AND ($3::int < 0 OR c.rarity = $3::int)
		ORDER BY EXISTS (SELECT 1 FROM card_owned o WHERE o.user_id = $2 AND o.card_id = c.id AND o.count > 0), c.rarity DESC, random()
		LIMIT 1`, cards, userID, rarity).
		Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
	return c, err
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

// CardEventPulls is the player's pulls of the event key in a row since one
// of its cards, and its cards from the catalog, in their order.
func (s *Store) CardEventPulls(ctx context.Context, userID int64, key string, cards []int64) (int, []Card, error) {
	var pulls int
	found := make([]Card, 0, len(cards))
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if pulls, err = eventPulls(ctx, tx, userID, key); err != nil {
			return err
		}
		for _, id := range cards {
			c, err := catalogCard(ctx, tx, id)
			if err != nil {
				return err
			}
			found = append(found, c)
		}
		return nil
	})
	if err != nil {
		return 0, nil, fmt.Errorf("storage: read a player's event pulls: %w", err)
	}
	return pulls, found, nil
}
