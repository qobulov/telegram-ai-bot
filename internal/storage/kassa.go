package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoKassaEntry is an entry that is not the user's, or no longer there.
var ErrNoKassaEntry = errors.New("storage: no such kassa entry")

// Kassa entry kinds: money in (kirim) and money out (chiqim).
const (
	KassaIn  = "in"
	KassaOut = "out"
)

// KassaEntry is one line of a user's cash book.
type KassaEntry struct {
	ID     int64
	Kind   string
	Amount int64
	Note   string
	Day    time.Time
}

// KassaTotals are a user's sums: all money in, all out, and what is left.
type KassaTotals struct {
	In      int64 `json:"in"`
	Out     int64 `json:"out"`
	Balance int64 `json:"balance"`
}

// AddKassaEntry writes e to userID's cash book and returns its ID.
func (s *Store) AddKassaEntry(ctx context.Context, userID int64, e KassaEntry) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO kassa_entries (user_id, kind, amount, note, day)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		userID, e.Kind, e.Amount, e.Note, e.Day).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("storage: add kassa entry: %w", err)
	}
	return id, nil
}

// KassaEntries are userID's entries of kind, newest day first, at most limit.
func (s *Store) KassaEntries(ctx context.Context, userID int64, kind string, limit int) ([]KassaEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, amount, note, day FROM kassa_entries
		WHERE user_id = $1 AND kind = $2
		ORDER BY day DESC, id DESC LIMIT $3`, userID, kind, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: kassa entries: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (KassaEntry, error) {
		var e KassaEntry
		err := row.Scan(&e.ID, &e.Kind, &e.Amount, &e.Note, &e.Day)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: kassa entries: %w", err)
	}
	return entries, nil
}

// KassaTotals sums userID's cash book.
func (s *Store) KassaTotals(ctx context.Context, userID int64) (KassaTotals, error) {
	var t KassaTotals
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount) FILTER (WHERE kind = 'in'), 0)::bigint,
		       COALESCE(SUM(amount) FILTER (WHERE kind = 'out'), 0)::bigint
		FROM kassa_entries WHERE user_id = $1`, userID).Scan(&t.In, &t.Out)
	if err != nil {
		return KassaTotals{}, fmt.Errorf("storage: kassa totals: %w", err)
	}
	t.Balance = t.In - t.Out
	return t, nil
}

// DeleteKassaEntry removes userID's entry id; another user's is not touched.
func (s *Store) DeleteKassaEntry(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM kassa_entries WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("storage: delete kassa entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoKassaEntry
	}
	return nil
}
