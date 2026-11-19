package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoCardBox is a box the player does not hold.
var ErrNoCardBox = errors.New("storage: no such box held")

// CardMail is a letter to a player: a note, and boxes, tokens or crystals
// they claim in the app.
type CardMail struct {
	ID        int64
	UserID    int64
	Note      string
	Box       string // the box Boxes are of, e.g. "box"
	Boxes     int
	Tokens    int
	Crystals  int
	SentBy    int64
	SentAt    time.Time
	ClaimedAt *time.Time
}

// SendCardMail mails m to its player and returns its ID.
func (s *Store) SendCardMail(ctx context.Context, m CardMail) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO card_mail (user_id, note, box, boxes, tokens, crystals, sent_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		m.UserID, m.Note, m.Box, m.Boxes, m.Tokens, m.Crystals, nonZero(m.SentBy)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("storage: send card mail: %w", err)
	}
	return id, nil
}

// CardMailbox is the player's last limit letters, newest first.
func (s *Store) CardMailbox(ctx context.Context, userID int64, limit int) ([]CardMail, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, note, box, boxes, tokens, crystals, coalesce(sent_by, 0), created_at, claimed_at
		FROM card_mail WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read the card mailbox: %w", err)
	}
	mail, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CardMail, error) {
		var m CardMail
		err := row.Scan(&m.ID, &m.UserID, &m.Note, &m.Box, &m.Boxes, &m.Tokens, &m.Crystals, &m.SentBy, &m.SentAt, &m.ClaimedAt)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the card mailbox: %w", err)
	}
	return mail, nil
}

// CardInbox is what the app's 🎒 button counts: the boxes held and the
// letters not yet claimed.
func (s *Store) CardInbox(ctx context.Context, userID int64) (boxes, unclaimed int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT coalesce((SELECT sum(count) FROM card_boxes WHERE user_id = $1), 0)::int,
		       (SELECT count(*) FROM card_mail WHERE user_id = $1 AND claimed_at IS NULL)::int`, userID).Scan(&boxes, &unclaimed)
	if err != nil {
		return 0, 0, fmt.Errorf("storage: count the card inbox: %w", err)
	}
	return boxes, unclaimed, nil
}

// CardBoxes is the boxes the player holds, by box.
func (s *Store) CardBoxes(ctx context.Context, userID int64) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT box, count FROM card_boxes WHERE user_id = $1 AND count > 0`, userID)
	if err != nil {
		return nil, fmt.Errorf("storage: read card boxes: %w", err)
	}
	boxes := map[string]int{}
	var box string
	var count int
	if _, err := pgx.ForEachRow(rows, []any{&box, &count}, func() error {
		boxes[box] = count
		return nil
	}); err != nil {
		return nil, fmt.Errorf("storage: read card boxes: %w", err)
	}
	return boxes, nil
}

// ClaimCardMail takes a letter's gifts, once: its boxes into the inventory,
// its tokens and crystals into the wallet. claimed is false, and nothing
// given, for a letter already claimed or not the player's.
func (s *Store) ClaimCardMail(ctx context.Context, userID int64, name string, id int64) (m CardMail, after CardOpening, claimed bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := lockCardPlayer(ctx, tx, userID, name); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `
			UPDATE card_mail SET claimed_at = now()
			WHERE id = $1 AND user_id = $2 AND claimed_at IS NULL
			RETURNING id, user_id, note, box, boxes, tokens, crystals`, id, userID).
			Scan(&m.ID, &m.UserID, &m.Note, &m.Box, &m.Boxes, &m.Tokens, &m.Crystals)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		claimed = true
		if m.Boxes > 0 {
			if _, err := tx.Exec(ctx, `
				INSERT INTO card_boxes (user_id, box, count) VALUES ($1, $2, $3)
				ON CONFLICT (user_id, box) DO UPDATE SET count = card_boxes.count + EXCLUDED.count`,
				userID, m.Box, m.Boxes); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `
			UPDATE card_players SET tokens = tokens + $2, shards = shards + $3 WHERE user_id = $1
			RETURNING tokens, shards, points`, userID, m.Tokens, m.Crystals).Scan(&after.Tokens, &after.Shards, &after.Points)
	})
	if err != nil {
		return CardMail{}, CardOpening{}, false, fmt.Errorf("storage: claim card mail: %w", err)
	}
	return m, after, claimed, nil
}

// OpenCardBox opens one of the player's boxes, adding the crystals it
// gave; ErrNoCardBox when they hold none of it. It returns the crystals
// and how many of that box are left.
func (s *Store) OpenCardBox(ctx context.Context, userID int64, name, box string, crystals int) (shards, left int, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := lockCardPlayer(ctx, tx, userID, name); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `
			UPDATE card_boxes SET count = count - 1 WHERE user_id = $1 AND box = $2 AND count > 0 RETURNING count`,
			userID, box).Scan(&left)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoCardBox
		}
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE card_players SET shards = shards + $2 WHERE user_id = $1 RETURNING shards`,
			userID, crystals).Scan(&shards)
	})
	if errors.Is(err, ErrNoCardBox) {
		return 0, 0, err
	}
	if err != nil {
		return 0, 0, fmt.Errorf("storage: open a card box: %w", err)
	}
	return shards, left, nil
}

// MemberByUsername is the user the bot last saw under @username in any
// group, for an owner's command; ErrUnknownAuthor when there is none.
func (s *Store) MemberByUsername(ctx context.Context, username string) (int64, error) {
	username = strings.ToLower(strings.TrimPrefix(username, "@"))
	if !isUsername(username) {
		return 0, ErrUnknownAuthor
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT user_id FROM group_members WHERE lower(username) = $1 ORDER BY last_seen_at DESC LIMIT 1`, username).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUnknownAuthor
	}
	if err != nil {
		return 0, fmt.Errorf("storage: find a member by username: %w", err)
	}
	return id, nil
}
