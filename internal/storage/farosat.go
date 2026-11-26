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
}

// TakeFarosat adds gain to a member's farosat in a group once on day, and
// gives their total, their place in the group and whether this was the
// day's first take; a second take on the same day changes nothing.
func (s *Store) TakeFarosat(ctx context.Context, chatID, userID int64, name string, day time.Time, gain int) (grams, place int, taken bool, err error) {
	date := day.Format("2006-01-02")
	err = s.pool.QueryRow(ctx, `
		INSERT INTO farosat (chat_id, user_id, name, grams, last_day) VALUES ($1, $2, $3, $5, $4::date)
		ON CONFLICT (chat_id, user_id) DO UPDATE SET grams = farosat.grams + $5, last_day = $4::date, name = $3, updated_at = now()
		WHERE farosat.last_day IS DISTINCT FROM $4::date
		RETURNING grams`, chatID, userID, name, date, gain).Scan(&grams)
	taken = err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `SELECT grams FROM farosat WHERE chat_id = $1 AND user_id = $2`, chatID, userID).Scan(&grams)
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("storage: take farosat: %w", err)
	}
	err = s.pool.QueryRow(ctx, `SELECT count(*) + 1 FROM farosat WHERE chat_id = $1 AND grams > $2`, chatID, grams).Scan(&place)
	if err != nil {
		return 0, 0, false, fmt.Errorf("storage: farosat place: %w", err)
	}
	return grams, place, taken, nil
}

// FarosatTop is a group's members by farosat, the most first.
func (s *Store) FarosatTop(ctx context.Context, chatID int64, limit int) ([]FarosatRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, name, grams FROM farosat WHERE chat_id = $1
		ORDER BY grams DESC, updated_at LIMIT $2`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: farosat top: %w", err)
	}
	top, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FarosatRow, error) {
		var r FarosatRow
		err := row.Scan(&r.UserID, &r.Name, &r.Grams)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: farosat top: %w", err)
	}
	return top, nil
}
