package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Group settings' keys.
const (
	// SettingCleanService, "on", has a group's service messages deleted.
	SettingCleanService = "clean_service"
	// SettingAntispamOff lists, joined by commas, the anti-spam guards a
	// group turned off with /antispam: "flood", "spam" and "newcomers". They
	// are on everywhere else.
	SettingAntispamOff = "antispam_off"
)

// GroupSetting is a group's value for key, "" when it has none.
func (s *Store) GroupSetting(ctx context.Context, chatID int64, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM group_settings WHERE chat_id = $1 AND key = $2`, chatID, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: read a group setting: %w", err)
	}
	return value, nil
}

// SetGroupSetting keeps a group's value for key; "" removes it.
func (s *Store) SetGroupSetting(ctx context.Context, chatID int64, key, value string, by int64) error {
	var err error
	if value == "" {
		_, err = s.pool.Exec(ctx, `DELETE FROM group_settings WHERE chat_id = $1 AND key = $2`, chatID, key)
	} else {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO group_settings (chat_id, key, value, set_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (chat_id, key) DO UPDATE SET value = EXCLUDED.value, set_by = EXCLUDED.set_by, set_at = now()`,
			chatID, key, value, by)
	}
	if err != nil {
		return fmt.Errorf("storage: keep a group setting: %w", err)
	}
	return nil
}

// ErrRepTooSoon is a second thanks to the same member within RepEvery.
var ErrRepTooSoon = errors.New("storage: thanked them lately")

// RepEvery is how often one member may thank the same other.
const RepEvery = time.Hour

// GiveRep adds one to a member's reputation from another, and gives the new
// total; ErrRepTooSoon when the giver thanked them within RepEvery.
func (s *Store) GiveRep(ctx context.Context, chatID, from, to int64) (int, error) {
	var points int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint # $2::bigint)`, chatID, from); err != nil {
			return err
		}
		var lately bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM reputation_log WHERE chat_id = $1 AND from_id = $2 AND to_id = $3 AND given_at > now() - $4::interval)`,
			chatID, from, to, RepEvery.String()).Scan(&lately); err != nil {
			return err
		}
		if lately {
			return ErrRepTooSoon
		}
		if _, err := tx.Exec(ctx, `INSERT INTO reputation_log (chat_id, from_id, to_id) VALUES ($1, $2, $3)`, chatID, from, to); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO member_reputation (chat_id, user_id, points) VALUES ($1, $2, 1)
			ON CONFLICT (chat_id, user_id) DO UPDATE SET points = member_reputation.points + 1, updated_at = now()
			RETURNING points`, chatID, to).Scan(&points)
	})
	if errors.Is(err, ErrRepTooSoon) {
		return 0, err
	}
	if err != nil {
		return 0, fmt.Errorf("storage: give reputation: %w", err)
	}
	return points, nil
}

// MemberRep is one line of a group's reputation table.
type MemberRep struct {
	UserID   int64
	Name     string
	Username string
	Points   int
}

// RepTop is a group's most thanked members.
func (s *Store) RepTop(ctx context.Context, chatID int64, limit int) ([]MemberRep, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.user_id, coalesce(g.first_name, ''), coalesce(g.username, ''), r.points
		FROM member_reputation r LEFT JOIN group_members g ON g.chat_id = r.chat_id AND g.user_id = r.user_id
		WHERE r.chat_id = $1 AND r.points > 0 ORDER BY r.points DESC, r.updated_at LIMIT $2`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read the reputation table: %w", err)
	}
	top, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (MemberRep, error) {
		var m MemberRep
		err := row.Scan(&m.UserID, &m.Name, &m.Username, &m.Points)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the reputation table: %w", err)
	}
	return top, nil
}

// SettingAI, "on", lets a group talk with the model; without it only the
// bot's commands and code answer there.
const SettingAI = "ai"

// GroupWithSetting is a group whose setting has a value, by its title.
type GroupWithSetting struct {
	ChatID int64
	Title  string
}

// GroupsWithSetting are the groups whose key is value, by title.
func (s *Store) GroupsWithSetting(ctx context.Context, key, value string) ([]GroupWithSetting, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.chat_id, coalesce(g.title, '') FROM group_settings s LEFT JOIN groups g ON g.chat_id = s.chat_id
		WHERE s.key = $1 AND s.value = $2 ORDER BY g.title`, key, value)
	if err != nil {
		return nil, fmt.Errorf("storage: list groups by a setting: %w", err)
	}
	groups, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupWithSetting, error) {
		var g GroupWithSetting
		err := row.Scan(&g.ChatID, &g.Title)
		return g, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list groups by a setting: %w", err)
	}
	return groups, nil
}
