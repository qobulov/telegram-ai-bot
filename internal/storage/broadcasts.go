package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoBroadcast means the owner has no draft that may be sent now.
var ErrNoBroadcast = errors.New("storage: no broadcast to confirm")

// Broadcast is a message an owner wants posted in some groups.
type Broadcast struct {
	OwnerID int64
	// UpdateID is the Telegram update the draft was made in.
	UpdateID int64
	// Text is sent as a message; or, when it is empty, the message
	// MessageID in FromChatID is copied.
	Text       string
	FromChatID int64
	MessageID  int64
	ChatIDs    []int64
}

// SaveBroadcast stores a draft, replacing the owner's earlier unsent ones.
func (s *Store) SaveBroadcast(ctx context.Context, b Broadcast) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM broadcasts WHERE owner_id = $1 AND sent_at IS NULL`, b.OwnerID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO broadcasts (owner_id, update_id, text, chat_ids, from_chat_id, message_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			b.OwnerID, b.UpdateID, b.Text, b.ChatIDs, nonZero(b.FromChatID), nonZero(b.MessageID))
		return err
	})
	if err != nil {
		return fmt.Errorf("storage: save broadcast: %w", err)
	}
	return nil
}

// ConfirmBroadcast marks the owner's draft as sent and returns it, once. The
// draft must be younger than maxAge and made in an update other than
// updateID, so the message that drafted it cannot also confirm it.
func (s *Store) ConfirmBroadcast(ctx context.Context, ownerID, updateID int64, maxAge time.Duration) (Broadcast, error) {
	b := Broadcast{OwnerID: ownerID}
	err := s.pool.QueryRow(ctx, `
		UPDATE broadcasts SET sent_at = now()
		WHERE owner_id = $1 AND sent_at IS NULL AND update_id <> $2
		  AND created_at > now() - make_interval(secs => $3)
		RETURNING update_id, text, chat_ids, coalesce(from_chat_id, 0), coalesce(message_id, 0)`, ownerID, updateID, maxAge.Seconds()).
		Scan(&b.UpdateID, &b.Text, &b.ChatIDs, &b.FromChatID, &b.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Broadcast{}, ErrNoBroadcast
	}
	if err != nil {
		return Broadcast{}, fmt.Errorf("storage: confirm broadcast: %w", err)
	}
	return b, nil
}

// CancelBroadcast drops the owner's unsent draft. It reports whether there
// was one.
func (s *Store) CancelBroadcast(ctx context.Context, ownerID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM broadcasts WHERE owner_id = $1 AND sent_at IS NULL`, ownerID)
	if err != nil {
		return false, fmt.Errorf("storage: cancel broadcast: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
