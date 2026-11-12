package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Errors of passing cards between players.
var (
	// ErrCardNotHeld is a card the player who would give it does not hold.
	ErrCardNotHeld = errors.New("storage: card not held")
	// ErrCardNoPlayer is a player to receive a card who has never drawn one.
	ErrCardNoPlayer = errors.New("storage: not a card player")
	// ErrCardTradeOver is a trade already done, called off, too old, or
	// offered to another player.
	ErrCardTradeOver = errors.New("storage: card trade over")
)

// CardGiveLimitError is a player who gave as many cards as they may today.
type CardGiveLimitError struct{ UserID int64 }

func (e *CardGiveLimitError) Error() string { return "storage: cards given today at the limit" }

// CardTrade is a trade done: the card From gave To and the card To gave
// back.
type CardTrade struct {
	ID       int64
	From, To int64
	Give     Card
	Take     Card
}

// CardTradeRules bound passing cards: Limit is how many a player may give
// in Day, and an offer older than Day may no longer be accepted.
type CardTradeRules struct {
	Limit int
	Day   time.Duration
	Now   time.Time
}

// HeldCardsNamed are the player's cards whose name holds name, in any case,
// the one so named first, then the rarest; at most limit.
func (s *Store) HeldCardsNamed(ctx context.Context, userID int64, name string, limit int) ([]Card, error) {
	name = strings.TrimSpace(name)
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.anime, c.image_url, c.favourites, c.rarity, c.attack, c.health, c.value, c.collection
		FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
		WHERE o.user_id = $1 AND c.name ILIKE $2
		ORDER BY lower(c.name) = lower($3) DESC, c.rarity DESC, c.favourites DESC
		LIMIT $4`, userID, "%"+likeMarks.Replace(name)+"%", name, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: find a player's card: %w", err)
	}
	cards, err := pgx.CollectRows(rows, scanCard)
	if err != nil {
		return nil, fmt.Errorf("storage: find a player's card: %w", err)
	}
	return cards, nil
}

// likeMarks are what ILIKE would read in a name as other than itself.
var likeMarks = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

func scanCard(row pgx.CollectableRow) (Card, error) {
	var c Card
	err := row.Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
	return c, err
}

// GiveCard gives a copy of the card from one player to another at once, with
// its points, and records it.
func (s *Store) GiveCard(ctx context.Context, chatID, from, to, cardID int64, rules CardTradeRules) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockTraders(ctx, tx, from, to); err != nil {
			return err
		}
		if err := checkGiveLimit(ctx, tx, from, rules); err != nil {
			return err
		}
		if err := moveCard(ctx, tx, from, to, cardID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO card_trades (chat_id, from_id, to_id, give_card, status, created_at, done_at)
			VALUES ($1, $2, $3, $4, 'done', $5, $5)`, chatID, from, to, cardID, rules.Now)
		return err
	})
	return tradeError("give a card", err)
}

// OfferCardTrade records an offer of a trade, open until the other player
// accepts it or either calls it off, and gives its ID.
func (s *Store) OfferCardTrade(ctx context.Context, chatID, from, to, give, take int64, now time.Time) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO card_trades (chat_id, from_id, to_id, give_card, take_card, created_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, chatID, from, to, give, take, now).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("storage: offer a card trade: %w", err)
	}
	return id, nil
}

// AcceptCardTrade does the open trade for by, the player it was offered to:
// each gives the other their card with its points.
func (s *Store) AcceptCardTrade(ctx context.Context, id, by int64, rules CardTradeRules) (CardTrade, error) {
	var trade CardTrade
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var created time.Time
		err := tx.QueryRow(ctx, `
			SELECT from_id, to_id, give_card, take_card, created_at FROM card_trades
			WHERE id = $1 AND status = 'open' AND take_card IS NOT NULL FOR UPDATE`, id).
			Scan(&trade.From, &trade.To, &trade.Give.ID, &trade.Take.ID, &created)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (trade.To != by || rules.Now.Sub(created) > rules.Day)) {
			return ErrCardTradeOver
		}
		if err != nil {
			return err
		}
		trade.ID = id
		if err := lockTraders(ctx, tx, trade.From, trade.To); err != nil {
			return err
		}
		for _, giver := range []int64{trade.From, trade.To} {
			if err := checkGiveLimit(ctx, tx, giver, rules); err != nil {
				return err
			}
		}
		if err := moveCard(ctx, tx, trade.From, trade.To, trade.Give.ID); err != nil {
			return err
		}
		if err := moveCard(ctx, tx, trade.To, trade.From, trade.Take.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE card_trades SET status = 'done', done_at = $2 WHERE id = $1`, id, rules.Now); err != nil {
			return err
		}
		return readCards(ctx, tx, &trade.Give, &trade.Take)
	})
	return trade, tradeError("accept a card trade", err)
}

// CallOffCardTrade calls off the open trade, by either of its players, and
// reports whether it was open.
func (s *Store) CallOffCardTrade(ctx context.Context, id, by int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE card_trades SET status = 'off', done_at = now()
		WHERE id = $1 AND status = 'open' AND $2 IN (from_id, to_id)`, id, by)
	if err != nil {
		return false, fmt.Errorf("storage: call off a card trade: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// lockTraders locks both players' rows, in one order so that two trades
// between them wait for each other; the one to receive must have drawn.
func lockTraders(ctx context.Context, tx pgx.Tx, from, to int64) error {
	rows, err := tx.Query(ctx, `SELECT user_id, draws FROM card_players WHERE user_id IN ($1, $2) ORDER BY user_id FOR UPDATE`, from, to)
	if err != nil {
		return err
	}
	draws := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return err
		}
		draws[id] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, ok := draws[from]; !ok {
		return ErrCardNotHeld
	}
	if draws[to] == 0 {
		return ErrCardNoPlayer
	}
	return nil
}

// checkGiveLimit refuses a player who gave rules.Limit cards in rules.Day.
func checkGiveLimit(ctx context.Context, tx pgx.Tx, userID int64, rules CardTradeRules) error {
	var given int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM card_trades WHERE from_id = $1 AND status = 'done' AND done_at > $2)
		     + (SELECT count(*) FROM card_trades WHERE to_id = $1 AND status = 'done' AND take_card IS NOT NULL AND done_at > $2)`,
		userID, rules.Now.Add(-rules.Day)).Scan(&given)
	if err != nil {
		return err
	}
	if given >= rules.Limit {
		return &CardGiveLimitError{UserID: userID}
	}
	return nil
}

// moveCard moves one copy of the card, not melted, from one player to the
// other, with its points. A player giving their last copy not melted no
// longer holds the card; the melted ones went to shards and are gone with it.
func moveCard(ctx context.Context, tx pgx.Tx, from, to, cardID int64) error {
	var count, melted, value int
	err := tx.QueryRow(ctx, `
		SELECT o.count, o.melted, c.value FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
		WHERE o.user_id = $1 AND o.card_id = $2 FOR UPDATE OF o`, from, cardID).Scan(&count, &melted, &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCardNotHeld
	}
	if err != nil {
		return err
	}
	lost := 0
	if count-melted <= 1 {
		lost = 1
		_, err = tx.Exec(ctx, `DELETE FROM card_owned WHERE user_id = $1 AND card_id = $2`, from, cardID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE card_owned SET count = count - 1 WHERE user_id = $1 AND card_id = $2`, from, cardID)
	}
	if err != nil {
		return err
	}
	var received int
	if err := tx.QueryRow(ctx, `
		INSERT INTO card_owned (user_id, card_id) VALUES ($1, $2)
		ON CONFLICT (user_id, card_id) DO UPDATE SET count = card_owned.count + 1
		RETURNING count`, to, cardID).Scan(&received); err != nil {
		return err
	}
	gained := 0
	if received == 1 {
		gained = 1
	}
	_, err = tx.Exec(ctx, `
		UPDATE card_players SET
			points = CASE WHEN user_id = $1 THEN greatest(points - $3, 0) ELSE points + $3 END,
			cards = cards + CASE WHEN user_id = $1 THEN -$4::int ELSE $5::int END
		WHERE user_id IN ($1, $2)`, from, to, value, lost, gained)
	return err
}

// readCards fills the cards from the catalog by their IDs.
func readCards(ctx context.Context, tx pgx.Tx, cards ...*Card) error {
	for _, c := range cards {
		err := tx.QueryRow(ctx, `
			SELECT id, name, anime, image_url, favourites, rarity, attack, health, value, collection
			FROM card_catalog WHERE id = $1`, c.ID).
			Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
		if err != nil {
			return err
		}
	}
	return nil
}

// tradeError wraps err, keeping the errors a player is told as they are.
func tradeError(what string, err error) error {
	var limit *CardGiveLimitError
	switch {
	case err == nil, errors.Is(err, ErrCardNotHeld), errors.Is(err, ErrCardNoPlayer), errors.Is(err, ErrCardTradeOver), errors.As(err, &limit):
		return err
	}
	return fmt.Errorf("storage: %s: %w", what, err)
}
