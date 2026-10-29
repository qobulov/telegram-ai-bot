package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// StarPayment is one payment in Telegram Stars for premium.
type StarPayment struct {
	// ChargeID is Telegram's name for the payment, which a refund needs.
	ChargeID  string
	UserID    int64
	FirstName string
	Username  string
	Stars     int
	// Days is how much premium it bought; 0 for a payment whose invoice
	// could not be read, kept only to be refunded.
	Days   int
	PaidAt time.Time
	// RefundedAt is when it was given back; zero while it is kept.
	RefundedAt time.Time
}

// RecordStarPayment keeps a payment and adds its days to the user's
// premium: from its end while it lasts, so that buying again early loses
// nothing, and from now otherwise. It returns when premium now ends.
// Telegram may deliver a payment twice; recorded is false the second time,
// which changes nothing.
func (s *Store) RecordStarPayment(ctx context.Context, p StarPayment) (until time.Time, recorded bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO star_payments (charge_id, user_id, first_name, username, stars, days)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (charge_id) DO NOTHING`,
			p.ChargeID, p.UserID, p.FirstName, p.Username, p.Stars, p.Days)
		if err != nil {
			return err
		}
		recorded = tag.RowsAffected() == 1
		if !recorded || p.Days <= 0 {
			until, err = premiumUntil(ctx, tx, p.UserID)
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO premium_users (user_id, until) VALUES ($1, now() + make_interval(days => $2))
			ON CONFLICT (user_id) DO UPDATE SET until = greatest(premium_users.until, now()) + make_interval(days => $2)
			RETURNING until`,
			p.UserID, p.Days).Scan(&until)
	})
	if err != nil {
		return time.Time{}, false, fmt.Errorf("storage: record star payment: %w", err)
	}
	return until, recorded, nil
}

// PremiumUntil is when the user's premium ends, which may be past; zero
// when they never had it.
func (s *Store) PremiumUntil(ctx context.Context, userID int64) (time.Time, error) {
	until, err := premiumUntil(ctx, s.pool, userID)
	if err != nil {
		return time.Time{}, fmt.Errorf("storage: premium: %w", err)
	}
	return until, nil
}

func premiumUntil(ctx context.Context, db queryRower, userID int64) (time.Time, error) {
	var until time.Time
	err := db.QueryRow(ctx, `SELECT until FROM premium_users WHERE user_id = $1`, userID).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	return until, err
}

// LastStarPayment is the latest payment not refunded of the user with this
// ID or, when username is not empty, who paid under this @username, in any
// case; found is false when there is none.
func (s *Store) LastStarPayment(ctx context.Context, userID int64, username string) (p StarPayment, found bool, err error) {
	rows, err := s.pool.Query(ctx, `
		SELECT charge_id, user_id, first_name, username, stars, days, paid_at, refunded_at
		FROM star_payments
		WHERE refunded_at IS NULL AND (user_id = $1 OR ($2 <> '' AND lower(username) = lower($2)))
		ORDER BY paid_at DESC
		LIMIT 1`, userID, username)
	if err != nil {
		return StarPayment{}, false, fmt.Errorf("storage: last star payment: %w", err)
	}
	payments, err := scanStarPayments(rows)
	if err != nil || len(payments) == 0 {
		return StarPayment{}, false, err
	}
	return payments[0], true, nil
}

// MarkStarRefunded records that a payment was given back, and ends its
// user's premium now: a refund takes back what was bought, and what is
// left of earlier payments with it. It is false when the payment is
// unknown or was refunded already.
func (s *Store) MarkStarRefunded(ctx context.Context, chargeID string) (refunded bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var userID int64
		err := tx.QueryRow(ctx, `
			UPDATE star_payments SET refunded_at = now()
			WHERE charge_id = $1 AND refunded_at IS NULL
			RETURNING user_id`, chargeID).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		refunded = true
		_, err = tx.Exec(ctx, `UPDATE premium_users SET until = least(until, now()) WHERE user_id = $1`, userID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("storage: mark star refund: %w", err)
	}
	return refunded, nil
}

// StarPayments are the latest payments, at most limit, newest first.
func (s *Store) StarPayments(ctx context.Context, limit int) ([]StarPayment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT charge_id, user_id, first_name, username, stars, days, paid_at, refunded_at
		FROM star_payments
		ORDER BY paid_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: star payments: %w", err)
	}
	return scanStarPayments(rows)
}

func scanStarPayments(rows pgx.Rows) ([]StarPayment, error) {
	defer rows.Close()
	var payments []StarPayment
	for rows.Next() {
		var p StarPayment
		var refunded *time.Time
		if err := rows.Scan(&p.ChargeID, &p.UserID, &p.FirstName, &p.Username, &p.Stars, &p.Days, &p.PaidAt, &refunded); err != nil {
			return nil, fmt.Errorf("storage: read star payment: %w", err)
		}
		if refunded != nil {
			p.RefundedAt = *refunded
		}
		payments = append(payments, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: read star payments: %w", err)
	}
	return payments, nil
}
