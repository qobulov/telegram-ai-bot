package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ChangeMafiaGame changes a chat's Mafia game with its row locked, so that
// of many requests at once each sees the last one's game: change gets the
// game as kept, as JSON, "{}" when the chat has none, and gives back the
// game to keep, or an error to keep it as it was. The game's rules are
// internal/mafia's; this keeps only its JSON.
func (s *Store) ChangeMafiaGame(ctx context.Context, chatID int64, change func(state []byte) ([]byte, error)) error {
	var refused error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO mafia_games (chat_id, state) VALUES ($1, '{}') ON CONFLICT (chat_id) DO NOTHING`, chatID); err != nil {
			return err
		}
		var state []byte
		if err := tx.QueryRow(ctx, `SELECT state FROM mafia_games WHERE chat_id = $1 FOR UPDATE`, chatID).Scan(&state); err != nil {
			return err
		}
		changed, err := change(state)
		if err != nil {
			refused = err
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE mafia_games SET state = $2, updated_at = now() WHERE chat_id = $1`, chatID, changed)
		return err
	})
	if refused != nil {
		return refused
	}
	if err != nil {
		return fmt.Errorf("storage: change mafia game: %w", err)
	}
	return nil
}

// AddRaidCoins gives each of the users coins to spend in Dev jangi, making
// a hero for whoever has none.
func (s *Store) AddRaidCoins(ctx context.Context, userIDs []int64, coins int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO raid_heroes (user_id, seed, coins)
		SELECT u, floor(random() * 4294967295)::bigint, $2 FROM unnest($1::bigint[]) AS u
		ON CONFLICT (user_id) DO UPDATE SET coins = raid_heroes.coins + EXCLUDED.coins, updated_at = now()`,
		userIDs, coins)
	if err != nil {
		return fmt.Errorf("storage: add raid coins: %w", err)
	}
	return nil
}
