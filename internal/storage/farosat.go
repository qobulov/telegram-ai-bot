package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// FarosatRow is one member's farosat in a group.
type FarosatRow struct {
	UserID int64
	Name   string
	Grams  int
	Elite  bool
}

// FarosatGains are a day's gain drawn for a plain member and for one of
// the elite; TakeFarosat adds the one that is theirs.
type FarosatGains struct {
	Plain, Elite int
}

// FarosatTake is a day's take: the member's farosat after it, the gain
// added, their place in the group, whether this was the day's first take,
// and whether they are of the elite.
type FarosatTake struct {
	Grams, Gain, Place int
	Taken, Elite       bool
}

// TakeFarosat adds the day's gain to a member's farosat in a group once on
// day; a second take on the same day changes nothing.
func (s *Store) TakeFarosat(ctx context.Context, chatID, userID int64, name string, day time.Time, gains FarosatGains) (FarosatTake, error) {
	var take FarosatTake
	date := day.Format("2006-01-02")
	err := s.pool.QueryRow(ctx, `
		INSERT INTO farosat (chat_id, user_id, name, grams, last_day) VALUES ($1, $2, $3, $5, $4::date)
		ON CONFLICT (chat_id, user_id) DO UPDATE
		SET grams = farosat.grams + CASE WHEN farosat.elite THEN $6 ELSE $5 END, last_day = $4::date, name = $3, updated_at = now()
		WHERE farosat.last_day IS DISTINCT FROM $4::date
		RETURNING grams, elite`, chatID, userID, name, date, gains.Plain, gains.Elite).Scan(&take.Grams, &take.Elite)
	take.Taken = err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `SELECT grams, elite FROM farosat WHERE chat_id = $1 AND user_id = $2`, chatID, userID).Scan(&take.Grams, &take.Elite)
	}
	if err != nil {
		return FarosatTake{}, fmt.Errorf("storage: take farosat: %w", err)
	}
	if take.Taken {
		take.Gain = gains.Plain
		if take.Elite {
			take.Gain = gains.Elite
		}
	}
	err = s.pool.QueryRow(ctx, `SELECT count(*) + 1 FROM farosat WHERE chat_id = $1 AND grams > $2`, chatID, take.Grams).Scan(&take.Place)
	if err != nil {
		return FarosatTake{}, fmt.Errorf("storage: farosat place: %w", err)
	}
	return take, nil
}

// JoinFarosatElite takes price grams from a member's farosat in a group
// and makes them one of the elite, once and only with price grams to give;
// became is false, and nothing taken, otherwise. grams and elite are their
// farosat and standing after.
func (s *Store) JoinFarosatElite(ctx context.Context, chatID, userID int64, price int) (grams int, elite, became bool, err error) {
	err = s.pool.QueryRow(ctx, `
		UPDATE farosat SET grams = grams - $3, elite = true, updated_at = now()
		WHERE chat_id = $1 AND user_id = $2 AND grams >= $3 AND NOT elite
		RETURNING grams`, chatID, userID, price).Scan(&grams)
	if err == nil {
		return grams, true, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, false, fmt.Errorf("storage: join the farosat elite: %w", err)
	}
	err = s.pool.QueryRow(ctx, `
		SELECT coalesce(max(grams), 0), coalesce(bool_or(elite), false) FROM farosat WHERE chat_id = $1 AND user_id = $2`, chatID, userID).Scan(&grams, &elite)
	if err != nil {
		return 0, false, false, fmt.Errorf("storage: join the farosat elite: %w", err)
	}
	return grams, elite, false, nil
}

// FarosatTop is a group's members by farosat, the most first.
func (s *Store) FarosatTop(ctx context.Context, chatID int64, limit int) ([]FarosatRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, name, grams, elite FROM farosat WHERE chat_id = $1
		ORDER BY grams DESC, updated_at LIMIT $2`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: farosat top: %w", err)
	}
	top, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FarosatRow, error) {
		var r FarosatRow
		err := row.Scan(&r.UserID, &r.Name, &r.Grams, &r.Elite)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: farosat top: %w", err)
	}
	return top, nil
}

// FarosatOf is a member's farosat in a group and their place in its
// ranking; a member who never took any has 0 and the place after the last.
func (s *Store) FarosatOf(ctx context.Context, chatID, userID int64) (grams, place int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT coalesce((SELECT grams FROM farosat WHERE chat_id = $1 AND user_id = $2), 0)`, chatID, userID).Scan(&grams)
	if err == nil {
		err = s.pool.QueryRow(ctx, `SELECT count(*) + 1 FROM farosat WHERE chat_id = $1 AND grams > $2`, chatID, grams).Scan(&place)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("storage: a member's farosat: %w", err)
	}
	return grams, place, nil
}

// FarosatPurchase is farosat bought with Stars for a group.
type FarosatPurchase struct {
	ChargeID string
	ChatID   int64
	UserID   int64
	Name     string
	Stars    int
	Grams    int
}

// RecordFarosatPurchase keeps a payment once and adds its grams to the
// buyer's farosat in its group, giving their farosat after; recorded is
// false, and nothing added, for a payment kept before.
func (s *Store) RecordFarosatPurchase(ctx context.Context, p FarosatPurchase) (grams int, recorded bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO farosat_purchases (charge_id, chat_id, user_id, stars, grams) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING`, p.ChargeID, p.ChatID, p.UserID, p.Stars, p.Grams)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		recorded = true
		return tx.QueryRow(ctx, `
			INSERT INTO farosat (chat_id, user_id, name, grams) VALUES ($1, $2, $3, $4)
			ON CONFLICT (chat_id, user_id) DO UPDATE SET grams = farosat.grams + $4, name = coalesce(nullif($3, ''), farosat.name), updated_at = now()
			RETURNING grams`, p.ChatID, p.UserID, p.Name, p.Grams).Scan(&grams)
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: record a farosat purchase: %w", err)
	}
	return grams, recorded, nil
}

// RefundFarosatPurchase takes a refunded payment's grams back, once; found
// is false for a charge not kept or refunded before.
func (s *Store) RefundFarosatPurchase(ctx context.Context, chargeID string) (found bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var chatID, userID int64
		var grams int
		err := tx.QueryRow(ctx, `
			UPDATE farosat_purchases SET refunded_at = now() WHERE charge_id = $1 AND refunded_at IS NULL
			RETURNING chat_id, user_id, grams`, chargeID).Scan(&chatID, &userID, &grams)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		_, err = tx.Exec(ctx, `UPDATE farosat SET grams = grams - $3, updated_at = now() WHERE chat_id = $1 AND user_id = $2`, chatID, userID, grams)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("storage: refund a farosat purchase: %w", err)
	}
	return found, nil
}

// FarosatGalaxies are the galaxies a member opened for Stars in a group,
// those refunded left out.
func (s *Store) FarosatGalaxies(ctx context.Context, chatID, userID int64) ([]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT galaxy_id FROM farosat_galaxies
		WHERE chat_id = $1 AND user_id = $2 AND refunded_at IS NULL ORDER BY galaxy_id`, chatID, userID)
	if err != nil {
		return nil, fmt.Errorf("storage: farosat galaxies: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, fmt.Errorf("storage: farosat galaxies: %w", err)
	}
	return ids, nil
}

// FarosatGalaxyPurchase is a galaxy opened for Stars in a group.
type FarosatGalaxyPurchase struct {
	ChargeID string
	ChatID   int64
	UserID   int64
	Galaxy   int
	Stars    int
}

// RecordFarosatGalaxy keeps a galaxy's payment once; recorded is false for
// one kept before.
func (s *Store) RecordFarosatGalaxy(ctx context.Context, p FarosatGalaxyPurchase) (recorded bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO farosat_galaxies (charge_id, chat_id, user_id, galaxy_id, stars) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING`, p.ChargeID, p.ChatID, p.UserID, p.Galaxy, p.Stars)
	if err != nil {
		return false, fmt.Errorf("storage: record a farosat galaxy: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// RefundFarosatGalaxy closes a refunded galaxy again, once; found is false
// for a charge not kept or refunded before.
func (s *Store) RefundFarosatGalaxy(ctx context.Context, chargeID string) (found bool, err error) {
	tag, err := s.pool.Exec(ctx, `UPDATE farosat_galaxies SET refunded_at = now() WHERE charge_id = $1 AND refunded_at IS NULL`, chargeID)
	if err != nil {
		return false, fmt.Errorf("storage: refund a farosat galaxy: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
