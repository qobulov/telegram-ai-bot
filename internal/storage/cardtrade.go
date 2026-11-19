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
	// ErrCardFused is a card raised above ★1 offered or traded, spare
	// copies or none: its stars stay with its holder, and it never moves.
	ErrCardFused = errors.New("storage: fused card")
	// ErrCardOfferGone is an offer on the market whose player no longer
	// holds the card offered; it is called off.
	ErrCardOfferGone = errors.New("storage: card offer gone")
	// ErrCardOfferOwn is a player taking their own offer.
	ErrCardOfferOwn = errors.New("storage: own card offer")
	// ErrCardTradeUneven is a trade of cards more than tradeRarityGap
	// rarities apart: a Common for a Mythic was how one player's cards went
	// to another's.
	ErrCardTradeUneven = errors.New("storage: cards of rarities too far apart")
	// ErrCardLimited is a limited card given, offered, wanted or traded: it
	// stays with the player who drew it.
	ErrCardLimited = errors.New("storage: limited card")
)

// limitedCards are the cards the owners made limited, one of a kind: none
// of them changes hands, by gift, offer or trade.
var limitedCards = map[int64]bool{
	298761: true, // Satoko Kirigaya, LIMITED No.000001
}

// CardLimited reports whether the card is limited and so never changes
// hands.
func CardLimited(id int64) bool { return limitedCards[id] }

// limitedCardIDs are the limited cards' IDs, for a query to leave them out.
func limitedCardIDs() []int64 {
	ids := make([]int64, 0, len(limitedCards))
	for id := range limitedCards {
		ids = append(ids, id)
	}
	return ids
}

// CardOffersError is a player with as many offers on the market as they may
// have open.
type CardOffersError struct{ Limit int }

func (e *CardOffersError) Error() string { return "storage: card offers at the limit" }

// CardTrade is a trade done: the card From gave To and the card To gave
// back.
type CardTrade struct {
	ID       int64
	From, To int64
	Give     Card
	Take     Card
}

// CardTradeRules are what passing a card needs to know: the time it is done.
// A player gives or trades as many cards a day as they like.
type CardTradeRules struct {
	Now time.Time
}

// likeMarks are what ILIKE would read in a name as other than itself.
var likeMarks = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

func scanCard(row pgx.CollectableRow) (Card, error) {
	var c Card
	err := row.Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
	return c, err
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

// tradeRarityGap is how many rarities apart a trade's two cards may be:
// one, an Epic goes for a Rare, an Epic or a Legendary, never two apart.
const tradeRarityGap = 1

// checkEven refuses a trade of two cards more than tradeRarityGap
// rarities apart, or of a limited card.
func checkEven(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, give, take int64) error {
	if CardLimited(give) || CardLimited(take) {
		return ErrCardLimited
	}
	var gap int
	err := q.QueryRow(ctx, `
		SELECT abs((SELECT rarity FROM card_catalog WHERE id = $1) - (SELECT rarity FROM card_catalog WHERE id = $2))`,
		give, take).Scan(&gap)
	if err != nil {
		return err
	}
	if gap > tradeRarityGap {
		return ErrCardTradeUneven
	}
	return nil
}

// checkFair refuses a trade between two players of uneven cards.
func checkFair(ctx context.Context, tx pgx.Tx, trade CardTrade) error {
	return checkEven(ctx, tx, trade.Give.ID, trade.Take.ID)
}

// moveCard moves one copy of the card, not melted, from one player to the
// other: the giver loses its points only with their last copy, and the
// other gains them only when it is new to them. A player giving their last
// copy not melted no longer holds the card; the used-up ones are gone with
// it. A card raised above ★1 keeps its stars with its holder, so it never
// moves, spare copies or none, ErrCardFused; a limited card never moves,
// ErrCardLimited: every trade comes this way.
func moveCard(ctx context.Context, tx pgx.Tx, from, to, cardID int64) error {
	if CardLimited(cardID) {
		return ErrCardLimited
	}
	var count, melted, stars, value int
	err := tx.QueryRow(ctx, `
		SELECT o.count, o.melted, o.stars, c.value FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
		WHERE o.user_id = $1 AND o.card_id = $2 FOR UPDATE OF o`, from, cardID).Scan(&count, &melted, &stars, &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCardNotHeld
	}
	if err != nil {
		return err
	}
	if stars > 1 {
		return ErrCardFused
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
			points = CASE WHEN user_id = $1 THEN greatest(points - $3::bigint * $4, 0) ELSE points + $3::bigint * $5 END,
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
	var offers *CardOffersError
	switch {
	case err == nil, errors.Is(err, ErrCardNotHeld), errors.Is(err, ErrCardNoPlayer), errors.Is(err, ErrCardTradeOver),
		errors.Is(err, ErrCardFused), errors.Is(err, ErrCardOfferGone), errors.Is(err, ErrCardOfferOwn),
		errors.Is(err, ErrCardTradeUneven), errors.Is(err, ErrCardLimited),
		errors.As(err, &offers):
		return err
	}
	return fmt.Errorf("storage: %s: %w", what, err)
}
