package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// JoinRequest is a request to join a group that waits for the requester to
// press the button the bot sent them.
type JoinRequest struct {
	ChatID, UserID int64
	// UserChatID is the private chat the button went to.
	UserChatID int64
	// Expires is when the request is declined unless pressed.
	Expires time.Time
}

// AddJoinRequest keeps a request until it is answered, and reports whether
// it is new: Telegram may deliver one update twice, and the requester must
// get one button.
func (s *Store) AddJoinRequest(ctx context.Context, r JoinRequest) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO join_requests (chat_id, user_id, user_chat_id, expires_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (chat_id, user_id) DO NOTHING`, r.ChatID, r.UserID, r.UserChatID, r.Expires)
	if err != nil {
		return false, fmt.Errorf("storage: add join request: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// TakeJoinRequest removes a waiting request and returns it, if there was
// one. Taking it is what lets a button pressed twice approve once.
func (s *Store) TakeJoinRequest(ctx context.Context, chatID, userID int64) (JoinRequest, bool, error) {
	r := JoinRequest{ChatID: chatID, UserID: userID}
	err := s.pool.QueryRow(ctx, `
		DELETE FROM join_requests WHERE chat_id = $1 AND user_id = $2
		RETURNING user_chat_id, expires_at`, chatID, userID).Scan(&r.UserChatID, &r.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return JoinRequest{}, false, nil
	}
	if err != nil {
		return JoinRequest{}, false, fmt.Errorf("storage: take join request: %w", err)
	}
	return r, true, nil
}

// TakeLateJoinRequests removes and returns up to limit requests whose time
// ran out before now, the oldest first. Two instances sweeping at once take
// different ones, so that none is declined twice.
func (s *Store) TakeLateJoinRequests(ctx context.Context, now time.Time, limit int) ([]JoinRequest, error) {
	rows, err := s.pool.Query(ctx, `
		WITH late AS (
			DELETE FROM join_requests
			WHERE (chat_id, user_id) IN (
				SELECT chat_id, user_id FROM join_requests
				WHERE expires_at <= $1
				ORDER BY expires_at
				LIMIT $2
				FOR UPDATE SKIP LOCKED)
			RETURNING chat_id, user_id, user_chat_id, expires_at)
		SELECT * FROM late ORDER BY expires_at`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: take late join requests: %w", err)
	}
	late, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (JoinRequest, error) {
		var r JoinRequest
		err := row.Scan(&r.ChatID, &r.UserID, &r.UserChatID, &r.Expires)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: take late join requests: %w", err)
	}
	return late, nil
}
