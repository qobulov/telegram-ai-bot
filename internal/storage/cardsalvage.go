package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrCardMaterials is a box the player's materials do not cover.
var ErrCardMaterials = errors.New("storage: too few materials")

// SpareCards are a player's 🧱 materials and their spare copies of each
// rarity: the copies of a card beyond the one kept and those its stars used.
type SpareCards struct {
	Materials int
	Spare     map[int]int
}

// CardSpares reads the player's materials and spare copies by rarity.
func (s *Store) CardSpares(ctx context.Context, userID int64) (SpareCards, error) {
	spares := SpareCards{Spare: map[int]int{}}
	err := s.pool.QueryRow(ctx, `SELECT coalesce((SELECT materials FROM card_players WHERE user_id = $1), 0)`, userID).
		Scan(&spares.Materials)
	if err != nil {
		return SpareCards{}, fmt.Errorf("storage: read a player's materials: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.rarity, sum(o.count - 1 - o.melted)::int FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
		WHERE o.user_id = $1 AND o.count - 1 - o.melted > 0 GROUP BY c.rarity`, userID)
	if err != nil {
		return SpareCards{}, fmt.Errorf("storage: read a player's spare cards: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rarity, n int
		if err := rows.Scan(&rarity, &n); err != nil {
			return SpareCards{}, fmt.Errorf("storage: read a player's spare cards: %w", err)
		}
		spares.Spare[rarity] = n
	}
	if err := rows.Err(); err != nil {
		return SpareCards{}, fmt.Errorf("storage: read a player's spare cards: %w", err)
	}
	return spares, nil
}

// SalvageCards breaks all the player's spare copies of rarity into
// materials, per a copy: each card keeps its one copy, its stars and the
// copies they used, so the player's cards and points stay. It gives the
// copies broken and the materials after.
func (s *Store) SalvageCards(ctx context.Context, userID int64, name string, rarity, per int) (copies, materials int, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := lockCardPlayer(ctx, tx, userID, name); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			WITH spare AS (
				SELECT o.card_id, o.count - 1 - o.melted AS n FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
				WHERE o.user_id = $1 AND c.rarity = $2 AND o.count - 1 - o.melted > 0 FOR UPDATE OF o),
			broken AS (
				UPDATE card_owned o SET melted = o.melted + s.n FROM spare s WHERE o.user_id = $1 AND o.card_id = s.card_id)
			SELECT coalesce(sum(n), 0)::int FROM spare`, userID, rarity).Scan(&copies); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE card_players SET materials = materials + $2 WHERE user_id = $1 RETURNING materials`,
			userID, copies*per).Scan(&materials)
	})
	if err != nil {
		return 0, 0, fmt.Errorf("storage: salvage cards: %w", err)
	}
	return copies, materials, nil
}

// OpenMaterialBox spends price materials on one card of the rarity roll
// gives, drawn as a pack's: ErrCardMaterials with too few. It gives the
// opening and the materials after.
func (s *Store) OpenMaterialBox(ctx context.Context, userID int64, name string, roll CardRoll, price int, now time.Time, quest CardQuest) (CardOpening, int, error) {
	var (
		open      CardOpening
		materials int
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT materials FROM card_players WHERE user_id = $1`, userID).Scan(&materials); err != nil {
			return err
		}
		if materials < price {
			return ErrCardMaterials
		}
		if open.Draws, err = drawPack(ctx, tx, userID, name, 1, roll, player, quest); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			UPDATE card_players SET materials = materials - $2, last_draw_at = $3 WHERE user_id = $1
			RETURNING materials, tokens, shards, points`, userID, price, now).Scan(&materials, &open.Tokens, &open.Shards, &open.Points)
	})
	if errors.Is(err, ErrCardMaterials) {
		return CardOpening{}, 0, err
	}
	if err != nil {
		return CardOpening{}, 0, fmt.Errorf("storage: open a material box: %w", err)
	}
	return open, materials, nil
}
