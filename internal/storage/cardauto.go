package storage

import (
	"context"
	"fmt"
)

// CardAuto is a player whose cards the auto opener opens: their name as
// the table shows it, and the language their cards are sent in.
type CardAuto struct {
	UserID   int64
	Name     string
	Language string
}

// ToggleCardAuto turns the player's auto opener on, sending in language,
// or off when it was on, and reports whether it is on now.
func (s *Store) ToggleCardAuto(ctx context.Context, userID int64, name, language string) (bool, error) {
	var on bool
	err := s.pool.QueryRow(ctx, `
		INSERT INTO card_players (user_id, name, auto_draw, auto_language) VALUES ($1, $2, true, $3)
		ON CONFLICT (user_id) DO UPDATE SET
			auto_draw = NOT card_players.auto_draw, auto_language = EXCLUDED.auto_language
		RETURNING auto_draw`, userID, name, language).Scan(&on)
	if err != nil {
		return false, fmt.Errorf("storage: toggle the card auto opener: %w", err)
	}
	return on, nil
}

// SetCardAuto turns the player's auto opener on, sending in language, or
// off, as the Mini App's switch does: a switch pressed twice by a slow page
// lands where it shows.
func (s *Store) SetCardAuto(ctx context.Context, userID int64, name string, on bool, language string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO card_players (user_id, name, auto_draw, auto_language) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET auto_draw = EXCLUDED.auto_draw, auto_language = EXCLUDED.auto_language`,
		userID, name, on, language)
	if err != nil {
		return fmt.Errorf("storage: set the card auto opener: %w", err)
	}
	return nil
}

// StopCardAuto turns the player's auto opener off, as when the bot may no
// longer write to them.
func (s *Store) StopCardAuto(ctx context.Context, userID int64) error {
	if _, err := s.pool.Exec(ctx, `UPDATE card_players SET auto_draw = false WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("storage: stop the card auto opener: %w", err)
	}
	return nil
}

// DueCardAutos is at most limit players whose auto opener is on and who
// have a free card of day left, the longest since a draw first.
func (s *Store) DueCardAutos(ctx context.Context, day CardDay, limit int) ([]CardAuto, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, name, auto_language FROM card_players
		WHERE auto_draw AND (free_day IS DISTINCT FROM $1 OR free_used < $2)
		ORDER BY last_draw_at NULLS FIRST LIMIT $3`, day.Start, day.PerDay, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: due card auto openers: %w", err)
	}
	defer rows.Close()
	var due []CardAuto
	for rows.Next() {
		var a CardAuto
		if err := rows.Scan(&a.UserID, &a.Name, &a.Language); err != nil {
			return nil, fmt.Errorf("storage: due card auto openers: %w", err)
		}
		due = append(due, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: due card auto openers: %w", err)
	}
	return due, nil
}
