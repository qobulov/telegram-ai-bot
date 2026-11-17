package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Report is a member's report of another's message to a group's admins.
type Report struct {
	ID                       int64
	ChatID                   int64
	ReporterID, TargetID     int64
	ReporterName, TargetName string
	MessageID                int64
	Quote                    string
	Status                   string // open, seen, banned, rejected
	HandledBy                int64
	At                       time.Time
}

// AddReport keeps a report and gives its ID.
func (s *Store) AddReport(ctx context.Context, r Report) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO reports (chat_id, reporter_id, reporter_name, target_id, target_name, message_id, quote)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		r.ChatID, r.ReporterID, r.ReporterName, r.TargetID, r.TargetName, r.MessageID, r.Quote).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("storage: keep a report: %w", err)
	}
	return id, nil
}

// OpenReports are the chat's reports no admin settled yet, made since, the
// latest first.
func (s *Store) OpenReports(ctx context.Context, chatID int64, since time.Time, limit int) ([]Report, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, chat_id, reporter_id, target_id, reporter_name, target_name, message_id, quote, status, coalesce(handled_by, 0), created_at
		FROM reports WHERE chat_id = $1 AND status = 'open' AND created_at > $2 ORDER BY created_at DESC LIMIT $3`, chatID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list reports: %w", err)
	}
	reports, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Report, error) { return scanReport(row) })
	if err != nil {
		return nil, fmt.Errorf("storage: list reports: %w", err)
	}
	return reports, nil
}

// SettleReport marks an open report seen, banned or rejected by an admin; ok
// is false when it was settled already, and the report is given either way.
// A ban is also recorded as the bot's, for /banlist.
func (s *Store) SettleReport(ctx context.Context, id int64, status string, by int64) (Report, bool, error) {
	var r Report
	settled := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT id, chat_id, reporter_id, target_id, reporter_name, target_name, message_id, quote, status, coalesce(handled_by, 0), created_at
			FROM reports WHERE id = $1 FOR UPDATE`, id)
		var err error
		if r, err = scanReport(row); err != nil {
			return err
		}
		if r.Status != "open" {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE reports SET status = $2, handled_by = $3, handled_at = now() WHERE id = $1`, id, status, by); err != nil {
			return err
		}
		r.Status, r.HandledBy, settled = status, by, true
		if status != "banned" {
			return nil
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO moderation_actions (chat_id, action, requester_id, target_user_id, reason, update_id)
			VALUES ($1, 'ban', $2, $3, $4, $5) ON CONFLICT (update_id, action) DO NOTHING`,
			r.ChatID, by, r.TargetID, "shikoyat: "+r.Quote, -id)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, false, nil
	}
	if err != nil {
		return Report{}, false, fmt.Errorf("storage: settle a report: %w", err)
	}
	return r, settled, nil
}

func scanReport(row pgx.Row) (Report, error) {
	var r Report
	err := row.Scan(&r.ID, &r.ChatID, &r.ReporterID, &r.TargetID, &r.ReporterName, &r.TargetName, &r.MessageID, &r.Quote, &r.Status, &r.HandledBy, &r.At)
	return r, err
}
