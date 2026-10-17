package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// UserProfile is what a person asked the bot to remember about talking to
// them: the name to call them, and whether to use "siz".
type UserProfile struct {
	UserID   int64
	CallName string
	Siz      bool
}

// Empty reports a profile that asks for nothing.
func (p UserProfile) Empty() bool {
	return p.CallName == "" && !p.Siz
}

// Profile returns the user's profile, empty when they have none.
func (s *Store) Profile(ctx context.Context, userID int64) (UserProfile, error) {
	p := UserProfile{UserID: userID}
	err := s.pool.QueryRow(ctx, `SELECT call_name, siz FROM user_profiles WHERE user_id = $1`, userID).Scan(&p.CallName, &p.Siz)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return UserProfile{}, fmt.Errorf("storage: profile: %w", err)
	}
	return p, nil
}

// SetProfile saves the user's profile. An empty one is removed.
func (s *Store) SetProfile(ctx context.Context, p UserProfile) error {
	if p.Empty() {
		return s.ClearProfile(ctx, p.UserID)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_profiles (user_id, call_name, siz) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET call_name = EXCLUDED.call_name, siz = EXCLUDED.siz, updated_at = now()`,
		p.UserID, p.CallName, p.Siz)
	if err != nil {
		return fmt.Errorf("storage: set profile: %w", err)
	}
	return nil
}

// ClearProfile forgets the user's profile.
func (s *Store) ClearProfile(ctx context.Context, userID int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM user_profiles WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("storage: clear profile: %w", err)
	}
	return nil
}
