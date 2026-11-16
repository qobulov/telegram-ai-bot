package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MemberRank is a member's rank in a group, with their name as the group
// knows it and who gave it.
type MemberRank struct {
	UserID   int64
	Name     string
	Username string
	Rank     int
	GivenBy  int64
	GivenAt  time.Time
}

// RankChange is one change of a member's rank: 0 took it away.
type RankChange struct {
	UserID, GivenBy int64
	Name, ByName    string
	Rank            int
	At              time.Time
}

// SetRank gives the member the rank in the chat, or takes it away with 0,
// and logs who did.
func (s *Store) SetRank(ctx context.Context, chatID, userID int64, rank int, by int64) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if rank <= 0 {
			_, err = tx.Exec(ctx, `DELETE FROM member_ranks WHERE chat_id = $1 AND user_id = $2`, chatID, userID)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO member_ranks (chat_id, user_id, rank, given_by) VALUES ($1, $2, $3, $4)
				ON CONFLICT (chat_id, user_id) DO UPDATE SET rank = EXCLUDED.rank, given_by = EXCLUDED.given_by, given_at = now()`,
				chatID, userID, rank, by)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO rank_log (chat_id, user_id, rank, given_by) VALUES ($1, $2, $3, $4)`, chatID, userID, max(rank, 0), by)
		return err
	})
	if err != nil {
		return fmt.Errorf("storage: set a member's rank: %w", err)
	}
	return nil
}

// RankOf is the member's rank in the chat, 0 for none.
func (s *Store) RankOf(ctx context.Context, chatID, userID int64) (int, error) {
	var rank int
	err := s.pool.QueryRow(ctx, `SELECT rank FROM member_ranks WHERE chat_id = $1 AND user_id = $2`, chatID, userID).Scan(&rank)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("storage: read a member's rank: %w", err)
	}
	return rank, nil
}

// Ranks are the chat's ranked members, the highest first.
func (s *Store) Ranks(ctx context.Context, chatID int64) ([]MemberRank, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.user_id, coalesce(g.first_name, ''), coalesce(g.username, ''), r.rank, r.given_by, r.given_at
		FROM member_ranks r LEFT JOIN group_members g ON g.chat_id = r.chat_id AND g.user_id = r.user_id
		WHERE r.chat_id = $1 ORDER BY r.rank DESC, r.given_at`, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: list ranks: %w", err)
	}
	ranked, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (MemberRank, error) {
		var m MemberRank
		err := row.Scan(&m.UserID, &m.Name, &m.Username, &m.Rank, &m.GivenBy, &m.GivenAt)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list ranks: %w", err)
	}
	return ranked, nil
}

// RankLog is the chat's latest rank changes, of userID alone when it is not
// 0, the latest first.
func (s *Store) RankLog(ctx context.Context, chatID, userID int64, limit int) ([]RankChange, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.user_id, coalesce(t.first_name, ''), l.rank, l.given_by, coalesce(b.first_name, ''), l.given_at
		FROM rank_log l
		LEFT JOIN group_members t ON t.chat_id = l.chat_id AND t.user_id = l.user_id
		LEFT JOIN group_members b ON b.chat_id = l.chat_id AND b.user_id = l.given_by
		WHERE l.chat_id = $1 AND ($2 = 0 OR l.user_id = $2) ORDER BY l.given_at DESC, l.id DESC LIMIT $3`, chatID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read the rank log: %w", err)
	}
	log, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RankChange, error) {
		var c RankChange
		err := row.Scan(&c.UserID, &c.Name, &c.Rank, &c.GivenBy, &c.ByName, &c.At)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the rank log: %w", err)
	}
	return log, nil
}

// CommandRanks are the ranks the chat's creator set for commands, by code.
func (s *Store) CommandRanks(ctx context.Context, chatID int64) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT command, rank FROM command_ranks WHERE chat_id = $1`, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: read command ranks: %w", err)
	}
	defer rows.Close()
	set := map[string]int{}
	for rows.Next() {
		var code string
		var rank int
		if err := rows.Scan(&code, &rank); err != nil {
			return nil, fmt.Errorf("storage: read command ranks: %w", err)
		}
		set[code] = rank
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: read command ranks: %w", err)
	}
	return set, nil
}

// SetCommandRank sets the rank the command needs in the chat.
func (s *Store) SetCommandRank(ctx context.Context, chatID int64, code string, rank int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO command_ranks (chat_id, command, rank) VALUES ($1, $2, $3)
		ON CONFLICT (chat_id, command) DO UPDATE SET rank = EXCLUDED.rank`, chatID, code, rank)
	if err != nil {
		return fmt.Errorf("storage: set a command's rank: %w", err)
	}
	return nil
}
