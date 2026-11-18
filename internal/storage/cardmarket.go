package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// CardOffer is an offer on the market: the card its player gives and the
// card they want for it; Can is whether the player looking holds the card
// wanted, and Mine whether the offer is theirs.
type CardOffer struct {
	ID       int64
	From     int64
	FromName string
	Give     Card
	Want     Card
	Can      bool
	Mine     bool
	Created  time.Time
}

// CardMarketRules bound the market: Open is how many offers a player may
// have open, and an offer older than Keep is gone.
type CardMarketRules struct {
	Open int
	Keep time.Duration
	Now  time.Time
}

// OfferCard puts the player's card up on the market for the card they want,
// and gives the offer's ID.
func (s *Store) OfferCard(ctx context.Context, from, give, want int64, rules CardMarketRules) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// The player's row, locked, so that two offers at once are counted
		// one after the other.
		var held, fused bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM card_owned WHERE user_id = $1 AND card_id = $2),
				EXISTS (SELECT 1 FROM card_owned WHERE user_id = $1 AND card_id = $2 AND stars > 1 AND count - melted < 2)
			FROM card_players WHERE user_id = $1 FOR UPDATE`, from, give).Scan(&held, &fused)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !held) {
			return ErrCardNotHeld
		}
		if err != nil {
			return err
		}
		// The last copy of a card raised above ★1 keeps its stars with its
		// holder, so it is not offered.
		if fused {
			return ErrCardFused
		}
		var open int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM card_trades
			WHERE from_id = $1 AND status = 'open' AND to_id IS NULL AND created_at > $2`,
			from, rules.Now.Add(-rules.Keep)).Scan(&open); err != nil {
			return err
		}
		if open >= rules.Open {
			return &CardOffersError{Limit: rules.Open}
		}
		if err := checkEven(ctx, tx, give, want); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO card_trades (chat_id, from_id, give_card, take_card, created_at)
			VALUES (0, $1, $2, $3, $4) RETURNING id`, from, give, want, rules.Now).Scan(&id)
	})
	return id, tradeError("offer a card", err)
}

// CardMarket is the offers open on the market as viewer sees them: those
// they can take first, then the newest; at most limit. An offer whose player
// no longer holds the card offered, or holds only the last copy of it raised
// above ★1, is left out, as taking it would fail, and
// so is one placed before trades were of one rarity and of no limited card.
func (s *Store) CardMarket(ctx context.Context, viewer int64, rules CardMarketRules, limit int) ([]CardOffer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.from_id, coalesce(p.name, ''), t.created_at,
			g.id, g.name, g.anime, g.image_url, g.favourites, g.rarity, g.attack, g.health, g.value, g.collection,
			w.id, w.name, w.anime, w.image_url, w.favourites, w.rarity, w.attack, w.health, w.value, w.collection,
			c.can
		FROM card_trades t
		JOIN card_catalog g ON g.id = t.give_card
		JOIN card_catalog w ON w.id = t.take_card
		LEFT JOIN card_players p ON p.user_id = t.from_id
		CROSS JOIN LATERAL (
			SELECT t.from_id <> $1 AND EXISTS (SELECT 1 FROM card_owned o WHERE o.user_id = $1 AND o.card_id = t.take_card) AS can
		) c
		WHERE t.status = 'open' AND t.to_id IS NULL AND t.created_at > $2
			AND EXISTS (SELECT 1 FROM card_owned o WHERE o.user_id = t.from_id AND o.card_id = t.give_card
				AND (o.stars = 1 OR o.count - o.melted >= 2))
			AND g.rarity = w.rarity AND NOT (t.give_card = ANY($4) OR t.take_card = ANY($4))
		ORDER BY c.can DESC, t.created_at DESC, t.id DESC LIMIT $3`, viewer, rules.Now.Add(-rules.Keep), limit, limitedCardIDs())
	if err != nil {
		return nil, fmt.Errorf("storage: read the card market: %w", err)
	}
	offers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CardOffer, error) {
		var o CardOffer
		g, w := &o.Give, &o.Want
		err := row.Scan(&o.ID, &o.From, &o.FromName, &o.Created,
			&g.ID, &g.Name, &g.Anime, &g.ImageURL, &g.Favourites, &g.Rarity, &g.Attack, &g.Health, &g.Value, &g.Collection,
			&w.ID, &w.Name, &w.Anime, &w.ImageURL, &w.Favourites, &w.Rarity, &w.Attack, &w.Health, &w.Value, &w.Collection,
			&o.Can)
		o.Mine = o.From == viewer
		return o, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the card market: %w", err)
	}
	return offers, nil
}

// TakeCardOffer takes the offer for by: its player gives by the card
// offered and by gives them the card wanted, each with its points. An offer
// whose player no longer holds its card is called off, ErrCardOfferGone.
func (s *Store) TakeCardOffer(ctx context.Context, id, by int64, rules CardTradeRules, keep time.Duration) (CardTrade, error) {
	var trade CardTrade
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT from_id, give_card, take_card FROM card_trades
			WHERE id = $1 AND status = 'open' AND to_id IS NULL AND created_at > $2 FOR UPDATE`,
			id, rules.Now.Add(-keep)).Scan(&trade.From, &trade.Give.ID, &trade.Take.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrCardTradeOver
		case err != nil:
			return err
		case trade.From == by:
			return ErrCardOfferOwn
		}
		trade.ID, trade.To = id, by
		if err := lockTraders(ctx, tx, by, trade.From); err != nil {
			return err
		}
		if err := moveCard(ctx, tx, trade.From, by, trade.Give.ID); errors.Is(err, ErrCardNotHeld) {
			return ErrCardOfferGone
		} else if err != nil {
			return err
		}
		if err := moveCard(ctx, tx, by, trade.From, trade.Take.ID); err != nil {
			return err
		}
		// Checked once the cards are known to be held, in the transaction
		// that a refusal rolls back.
		if err := checkFair(ctx, tx, trade); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE card_trades SET status = 'done', to_id = $2, done_at = $3 WHERE id = $1`, id, by, rules.Now); err != nil {
			return err
		}
		return readCards(ctx, tx, &trade.Give, &trade.Take)
	})
	if errors.Is(err, ErrCardOfferGone) {
		if _, offErr := s.pool.Exec(ctx, `UPDATE card_trades SET status = 'off', done_at = now() WHERE id = $1 AND status = 'open'`, id); offErr != nil {
			return CardTrade{}, fmt.Errorf("storage: call off a card offer gone: %w", offErr)
		}
	}
	return trade, tradeError("take a card offer", err)
}

// CatalogCardsNamed are the cards of the catalog whose name holds name, in
// any case, the one so named first, then the rarest; at most limit.
func (s *Store) CatalogCardsNamed(ctx context.Context, name string, limit int) ([]Card, error) {
	name = strings.TrimSpace(name)
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, anime, image_url, favourites, rarity, attack, health, value, collection
		FROM card_catalog WHERE name ILIKE $1
		ORDER BY lower(name) = lower($2) DESC, rarity DESC, favourites DESC LIMIT $3`,
		"%"+likeMarks.Replace(name)+"%", name, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: find catalog cards: %w", err)
	}
	cards, err := pgx.CollectRows(rows, scanCard)
	if err != nil {
		return nil, fmt.Errorf("storage: find catalog cards: %w", err)
	}
	return cards, nil
}

// CatalogPage asks for a page of the catalog's cards of one rarity: those
// whose name holds Name, in any case (an empty Name is all of them), but
// Except; Offset are skipped and Limit given.
type CatalogPage struct {
	Rarity int
	Name   string
	Except int64
	Offset int
	Limit  int
}

// CatalogCards are a page of the catalog's cards of one rarity that may be
// traded, the most loved first, then by name; more is whether a page
// follows. Limited cards are left out, as none is traded.
func (s *Store) CatalogCards(ctx context.Context, page CatalogPage) (cards []Card, more bool, err error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, anime, image_url, favourites, rarity, attack, health, value, collection
		FROM card_catalog
		WHERE rarity = $1 AND name ILIKE $2 AND id <> $3 AND NOT id = ANY($4)
		ORDER BY favourites DESC, name, id OFFSET $5 LIMIT $6`,
		page.Rarity, "%"+likeMarks.Replace(strings.TrimSpace(page.Name))+"%", page.Except, limitedCardIDs(),
		max(page.Offset, 0), page.Limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("storage: read a catalog page: %w", err)
	}
	cards, err = pgx.CollectRows(rows, scanCard)
	if err != nil {
		return nil, false, fmt.Errorf("storage: read a catalog page: %w", err)
	}
	if len(cards) > page.Limit {
		return cards[:page.Limit], true, nil
	}
	return cards, false, nil
}
