package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// BuyCardTokens trades price crystals for tokens; with too few crystals it
// changes nothing and returns a *CardFundsError.
func (s *Store) BuyCardTokens(ctx context.Context, userID int64, name string, tokens, price int) (CardOpening, error) {
	var after CardOpening
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		if player.shards < price {
			return &CardFundsError{Tokens: player.tokens, Shards: player.shards, Need: price}
		}
		return tx.QueryRow(ctx, `UPDATE card_players SET shards = shards - $2, tokens = tokens + $3 WHERE user_id = $1 RETURNING tokens, shards, points`,
			userID, price, tokens).Scan(&after.Tokens, &after.Shards, &after.Points)
	})
	var few *CardFundsError
	if errors.As(err, &few) {
		return CardOpening{}, few
	}
	if err != nil {
		return CardOpening{}, fmt.Errorf("storage: buy card tokens: %w", err)
	}
	return after, nil
}

// OpenCardPack opens n cards for price crystals, roll picking each one's
// rarity; with too few crystals it opens none and returns a
// *CardFundsError. Tokens are left as they are. The n cards count toward
// quest.
func (s *Store) OpenCardPack(ctx context.Context, userID int64, name string, n int, roll CardRoll, price int, now time.Time, quest CardQuest) (CardOpening, error) {
	var open CardOpening
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		if player.shards < price {
			return &CardFundsError{Tokens: player.tokens, Shards: player.shards, Need: price}
		}
		if open.Draws, err = drawPack(ctx, tx, userID, name, n, roll, player, quest); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE card_players SET shards = shards - $2, last_draw_at = $3 WHERE user_id = $1 RETURNING tokens, shards, points`,
			userID, price, now).Scan(&open.Tokens, &open.Shards, &open.Points)
	})
	var few *CardFundsError
	if errors.As(err, &few) {
		return CardOpening{}, few
	}
	if err != nil {
		return CardOpening{}, fmt.Errorf("storage: open a card pack: %w", err)
	}
	return open, nil
}

// drawPack gives the player n cards, roll picking each one's rarity, and
// counts them toward quest; the player's row is locked already.
func drawPack(ctx context.Context, tx pgx.Tx, userID int64, name string, n int, roll CardRoll, player cardPlayerRow, quest CardQuest) ([]CardDraw, error) {
	draws := make([]CardDraw, 0, n)
	for range n {
		draw, err := giveCard(ctx, tx, userID, name, "", roll, player)
		if err != nil {
			return nil, err
		}
		draws = append(draws, draw)
	}
	return draws, countQuest(ctx, tx, userID, quest, n)
}

// CrystalPurchase is crystals bought for Stars.
type CrystalPurchase struct {
	ChargeID string
	UserID   int64
	Name     string
	Stars    int
	Crystals int
}

// RecordCrystalPurchase keeps a payment and adds its crystals, once: a
// payment Telegram reports again adds nothing, and recorded is false. It
// gives the player's crystals after.
func (s *Store) RecordCrystalPurchase(ctx context.Context, p CrystalPurchase) (shards int, recorded bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO crystal_purchases (charge_id, user_id, stars, crystals) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
			p.ChargeID, p.UserID, p.Stars, p.Crystals)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		recorded = true
		if _, err := lockCardPlayer(ctx, tx, p.UserID, p.Name); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE card_players SET shards = shards + $2 WHERE user_id = $1 RETURNING shards`, p.UserID, p.Crystals).Scan(&shards)
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: record a crystal purchase: %w", err)
	}
	return shards, recorded, nil
}

// CrystalPayment is a kept crystal purchase, with its buyer's name in the
// cards game.
type CrystalPayment struct {
	ChargeID   string
	UserID     int64
	Name       string
	Stars      int
	Crystals   int
	PaidAt     time.Time
	RefundedAt time.Time // zero when not refunded
}

// CrystalPayments are the latest crystal purchases, at most limit, newest
// first.
func (s *Store) CrystalPayments(ctx context.Context, limit int) ([]CrystalPayment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.charge_id, p.user_id, coalesce(cp.name, ''), p.stars, p.crystals, p.created_at, p.refunded_at
		FROM crystal_purchases p
		LEFT JOIN card_players cp ON cp.user_id = p.user_id
		ORDER BY p.created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: crystal payments: %w", err)
	}
	defer rows.Close()
	var payments []CrystalPayment
	for rows.Next() {
		var p CrystalPayment
		var refunded *time.Time
		if err := rows.Scan(&p.ChargeID, &p.UserID, &p.Name, &p.Stars, &p.Crystals, &p.PaidAt, &refunded); err != nil {
			return nil, fmt.Errorf("storage: read crystal payment: %w", err)
		}
		if refunded != nil {
			p.RefundedAt = *refunded
		}
		payments = append(payments, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: crystal payments: %w", err)
	}
	return payments, nil
}

// MarkCrystalRefunded takes back a refunded purchase's crystals, as many as
// the player still has, and reports whether it was a purchase not yet
// marked.
func (s *Store) MarkCrystalRefunded(ctx context.Context, chargeID string) (bool, error) {
	var marked bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var userID int64
		var crystals int
		err := tx.QueryRow(ctx, `UPDATE crystal_purchases SET refunded_at = now() WHERE charge_id = $1 AND refunded_at IS NULL RETURNING user_id, crystals`,
			chargeID).Scan(&userID, &crystals)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		marked = true
		_, err = tx.Exec(ctx, `UPDATE card_players SET shards = greatest(shards - $2, 0) WHERE user_id = $1`, userID, crystals)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("storage: mark a crystal refund: %w", err)
	}
	return marked, nil
}
