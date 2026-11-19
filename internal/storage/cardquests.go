package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Refusals of claiming a daily quest.
var (
	// ErrQuestNotDone is a quest whose progress has not reached its target.
	ErrQuestNotDone = errors.New("storage: quest not done")
	// ErrQuestClaimed is a quest whose tokens were claimed already.
	ErrQuestClaimed = errors.New("storage: quest already claimed")
)

// CardQuest is a daily quest an action counts toward: its ID, its Day (the
// start of the day in Tashkent; its date, in its own location, is the day
// counted) and the progress that completes it. The zero CardQuest counts
// nothing.
type CardQuest struct {
	ID     string
	Day    time.Time
	Target int
}

// day is the date the quest counts, as Postgres reads a DATE.
func (q CardQuest) day() string { return q.Day.Format(time.DateOnly) }

// QuestProgress is a player's progress on one quest of a day, and whether
// its tokens were claimed.
type QuestProgress struct {
	Progress int
	Claimed  bool
}

// countQuest adds n to the player's progress on q, in the transaction of the
// action that counts, never past q's target.
func countQuest(ctx context.Context, tx pgx.Tx, userID int64, q CardQuest, n int) error {
	if q.ID == "" || n <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO card_quests (user_id, day, quest, progress) VALUES ($1, $2::date, $3, least($4::int, $5::int))
		ON CONFLICT (user_id, day, quest) DO UPDATE SET progress = least(card_quests.progress + $4::int, $5::int)`,
		userID, q.day(), q.ID, n, q.Target)
	return err
}

// CardQuests is the player's progress on the quests of day, by quest ID; a
// quest nothing counted toward yet is missing.
func (s *Store) CardQuests(ctx context.Context, userID int64, day time.Time) (map[string]QuestProgress, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT quest, progress, claimed_at IS NOT NULL FROM card_quests WHERE user_id = $1 AND day = $2::date`,
		userID, day.Format(time.DateOnly))
	if err != nil {
		return nil, fmt.Errorf("storage: read card quests: %w", err)
	}
	defer rows.Close()
	quests := map[string]QuestProgress{}
	for rows.Next() {
		var id string
		var p QuestProgress
		if err := rows.Scan(&id, &p.Progress, &p.Claimed); err != nil {
			return nil, fmt.Errorf("storage: read card quests: %w", err)
		}
		quests[id] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: read card quests: %w", err)
	}
	return quests, nil
}

// ClaimCardQuest gives the player reward tokens for q, once: the quest is
// marked claimed by a conditional update, so of two claims at once only one
// pays. A quest short of its target is ErrQuestNotDone, one claimed before
// ErrQuestClaimed.
func (s *Store) ClaimCardQuest(ctx context.Context, userID int64, q CardQuest, reward int, now time.Time) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE card_quests SET claimed_at = $5
			WHERE user_id = $1 AND day = $2::date AND quest = $3 AND progress >= $4 AND claimed_at IS NULL`,
			userID, q.day(), q.ID, q.Target, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return questRefusal(ctx, tx, userID, q)
		}
		paid, err := tx.Exec(ctx, `UPDATE card_players SET tokens = tokens + $2 WHERE user_id = $1`, userID, reward)
		if err == nil && paid.RowsAffected() == 0 {
			err = fmt.Errorf("no card player %d", userID)
		}
		return err
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrQuestNotDone), errors.Is(err, ErrQuestClaimed):
		return err
	}
	return fmt.Errorf("storage: claim a card quest: %w", err)
}

// questRefusal says why a quest could not be claimed: claimed before, or
// not done (no row is a quest nothing counted toward).
func questRefusal(ctx context.Context, tx pgx.Tx, userID int64, q CardQuest) error {
	var claimed bool
	err := tx.QueryRow(ctx, `
		SELECT claimed_at IS NOT NULL FROM card_quests WHERE user_id = $1 AND day = $2::date AND quest = $3`,
		userID, q.day(), q.ID).Scan(&claimed)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrQuestNotDone
	case err != nil:
		return err
	case claimed:
		return ErrQuestClaimed
	}
	return ErrQuestNotDone
}
