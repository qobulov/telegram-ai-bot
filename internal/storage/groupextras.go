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
	// SettingLogChat is the chat a group's log goes to, as a chat ID.
	SettingLogChat = "log_chat"
	// SettingCleanService, "on", has a group's service messages deleted.
	SettingCleanService = "clean_service"
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

// RepOf is a member's reputation in a group and their place, 0 with none.
func (s *Store) RepOf(ctx context.Context, chatID, userID int64) (points, place int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT points, (SELECT count(*) FROM member_reputation o WHERE o.chat_id = r.chat_id AND o.points > r.points)::int + 1
		FROM member_reputation r WHERE chat_id = $1 AND user_id = $2`, chatID, userID).Scan(&points, &place)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("storage: read reputation: %w", err)
	}
	return points, place, nil
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

// GroupNote is a saved answer.
type GroupNote struct {
	ChatID    int64
	Name      string
	Text      string
	MediaKind string
	FileID    string
	SavedBy   int64
}

// MaxNotes is how many saved answers a group keeps.
const MaxNotes = 100

// ErrTooManyNotes is a new note past MaxNotes.
var ErrTooManyNotes = errors.New("storage: too many notes")

// SaveNote keeps a note, replacing one by the same name.
func (s *Store) SaveNote(ctx context.Context, n GroupNote) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var count int
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT count(*), coalesce(bool_or(name = $2), false) FROM group_notes WHERE chat_id = $1`,
			n.ChatID, n.Name).Scan(&count, &exists); err != nil {
			return err
		}
		if !exists && count >= MaxNotes {
			return ErrTooManyNotes
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO group_notes (chat_id, name, text, media_kind, file_id, saved_by) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (chat_id, name) DO UPDATE SET text = EXCLUDED.text, media_kind = EXCLUDED.media_kind,
				file_id = EXCLUDED.file_id, saved_by = EXCLUDED.saved_by, saved_at = now()`,
			n.ChatID, n.Name, n.Text, n.MediaKind, n.FileID, n.SavedBy)
		return err
	})
	if errors.Is(err, ErrTooManyNotes) {
		return err
	}
	if err != nil {
		return fmt.Errorf("storage: save a note: %w", err)
	}
	return nil
}

// Note is a group's note by name; found is false when it has none.
func (s *Store) Note(ctx context.Context, chatID int64, name string) (n GroupNote, found bool, err error) {
	err = s.pool.QueryRow(ctx, `SELECT chat_id, name, text, media_kind, file_id, saved_by FROM group_notes WHERE chat_id = $1 AND name = $2`,
		chatID, name).Scan(&n.ChatID, &n.Name, &n.Text, &n.MediaKind, &n.FileID, &n.SavedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return GroupNote{}, false, nil
	}
	if err != nil {
		return GroupNote{}, false, fmt.Errorf("storage: read a note: %w", err)
	}
	return n, true, nil
}

// NoteNames are a group's notes' names, in order.
func (s *Store) NoteNames(ctx context.Context, chatID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM group_notes WHERE chat_id = $1 ORDER BY name`, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: list notes: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("storage: list notes: %w", err)
	}
	return names, nil
}

// DeleteNote removes a note, reporting whether there was one.
func (s *Store) DeleteNote(ctx context.Context, chatID int64, name string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM group_notes WHERE chat_id = $1 AND name = $2`, chatID, name)
	if err != nil {
		return false, fmt.Errorf("storage: delete a note: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// GroupTitle is a group's title as the bot last saw it, "" when unknown.
func (s *Store) GroupTitle(ctx context.Context, chatID int64) (string, error) {
	var title string
	err := s.pool.QueryRow(ctx, `SELECT title FROM groups WHERE chat_id = $1`, chatID).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: read a group's title: %w", err)
	}
	return title, nil
}
