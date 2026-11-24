package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrNoKassaEntry is an entry that is not the user's, or no longer there.
	ErrNoKassaEntry = errors.New("storage: no such kassa entry")
	// ErrNoKassaBook is a book that is not the user's, or no longer there.
	ErrNoKassaBook = errors.New("storage: no such kassa book")
	// ErrKassaBooksFull is one book more than a user may keep.
	ErrKassaBooksFull = errors.New("storage: too many kassa books")
)

// MaxKassaBooks is how many cash books a user may keep.
const MaxKassaBooks = 20

// Kassa entry kinds: money in (kirim) and money out (chiqim).
const (
	KassaIn  = "in"
	KassaOut = "out"
)

// KassaEntry is one line of a cash book.
type KassaEntry struct {
	ID     int64
	Kind   string
	Amount int64
	Note   string
	Day    time.Time
}

// KassaTotals are a book's sums: all money in, all out, and what is left.
type KassaTotals struct {
	In      int64 `json:"in"`
	Out     int64 `json:"out"`
	Balance int64 `json:"balance"`
}

// KassaBook is one of a user's cash books, with its sums.
type KassaBook struct {
	ID     int64
	Name   string
	Totals KassaTotals
}

// KassaBooks are userID's books, oldest first, each with its sums.
func (s *Store) KassaBooks(ctx context.Context, userID int64) ([]KassaBook, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.name,
		       COALESCE(SUM(e.amount) FILTER (WHERE e.kind = 'in'), 0)::bigint,
		       COALESCE(SUM(e.amount) FILTER (WHERE e.kind = 'out'), 0)::bigint
		FROM kassa_books b LEFT JOIN kassa_entries e ON e.book_id = b.id
		WHERE b.user_id = $1
		GROUP BY b.id ORDER BY b.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("storage: kassa books: %w", err)
	}
	books, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (KassaBook, error) {
		var b KassaBook
		err := row.Scan(&b.ID, &b.Name, &b.Totals.In, &b.Totals.Out)
		b.Totals.Balance = b.Totals.In - b.Totals.Out
		return b, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: kassa books: %w", err)
	}
	return books, nil
}

// CreateKassaBook opens a book named name for userID and returns its ID;
// past MaxKassaBooks it refuses with ErrKassaBooksFull.
func (s *Store) CreateKassaBook(ctx context.Context, userID int64, name string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO kassa_books (user_id, name)
		SELECT $1, $2 WHERE (SELECT count(*) FROM kassa_books WHERE user_id = $1) < $3
		RETURNING id`, userID, name, MaxKassaBooks).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrKassaBooksFull
	}
	if err != nil {
		return 0, fmt.Errorf("storage: create kassa book: %w", err)
	}
	return id, nil
}

// RenameKassaBook names userID's book bookID name.
func (s *Store) RenameKassaBook(ctx context.Context, userID, bookID int64, name string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE kassa_books SET name = $3 WHERE id = $1 AND user_id = $2`, bookID, userID, name)
	if err != nil {
		return fmt.Errorf("storage: rename kassa book: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoKassaBook
	}
	return nil
}

// DeleteKassaBook removes userID's book bookID with all its entries.
func (s *Store) DeleteKassaBook(ctx context.Context, userID, bookID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM kassa_books WHERE id = $1 AND user_id = $2`, bookID, userID)
	if err != nil {
		return fmt.Errorf("storage: delete kassa book: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoKassaBook
	}
	return nil
}

// AddKassaEntry writes e to userID's book bookID and returns its ID.
func (s *Store) AddKassaEntry(ctx context.Context, userID, bookID int64, e KassaEntry) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO kassa_entries (user_id, book_id, kind, amount, note, day)
		SELECT user_id, id, $3, $4, $5, $6 FROM kassa_books WHERE id = $2 AND user_id = $1
		RETURNING id`,
		userID, bookID, e.Kind, e.Amount, e.Note, e.Day).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNoKassaBook
	}
	if err != nil {
		return 0, fmt.Errorf("storage: add kassa entry: %w", err)
	}
	return id, nil
}

// KassaEntries are the entries of kind in userID's book bookID, newest day
// first, at most limit.
func (s *Store) KassaEntries(ctx context.Context, userID, bookID int64, kind string, limit int) ([]KassaEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, amount, note, day FROM kassa_entries
		WHERE book_id = $1 AND user_id = $2 AND kind = $3
		ORDER BY day DESC, id DESC LIMIT $4`, bookID, userID, kind, limit)
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

// KassaBook is userID's book bookID with its sums.
func (s *Store) KassaBook(ctx context.Context, userID, bookID int64) (KassaBook, error) {
	b := KassaBook{ID: bookID}
	err := s.pool.QueryRow(ctx, `
		SELECT b.name,
		       COALESCE(SUM(e.amount) FILTER (WHERE e.kind = 'in'), 0)::bigint,
		       COALESCE(SUM(e.amount) FILTER (WHERE e.kind = 'out'), 0)::bigint
		FROM kassa_books b LEFT JOIN kassa_entries e ON e.book_id = b.id
		WHERE b.id = $1 AND b.user_id = $2
		GROUP BY b.id`, bookID, userID).Scan(&b.Name, &b.Totals.In, &b.Totals.Out)
	if errors.Is(err, pgx.ErrNoRows) {
		return KassaBook{}, ErrNoKassaBook
	}
	if err != nil {
		return KassaBook{}, fmt.Errorf("storage: kassa book: %w", err)
	}
	b.Totals.Balance = b.Totals.In - b.Totals.Out
	return b, nil
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
