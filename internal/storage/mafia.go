package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ChangeMafiaGame changes a chat's Mafia game with its row locked, so that
// of many requests at once each sees the last one's game: change gets the
// game as kept, as JSON, "{}" when the chat has none, and gives back the
// game to keep, or an error to keep it as it was, and the card-game tokens
// to give each of the players it names: the winners of a game this change
// ended. The tokens are given in the transaction that keeps the game, so a
// game's winners are paid once, with its end, or not at all. The game's
// rules are internal/mafia's; this keeps only its JSON.
func (s *Store) ChangeMafiaGame(ctx context.Context, chatID int64, change func(state []byte) ([]byte, map[int64]int, error)) error {
	return s.changeStateTx(ctx, "mafia_games", chatID, func(tx pgx.Tx, state []byte) ([]byte, error) {
		next, tokens, err := change(state)
		if err != nil {
			return nil, err
		}
		if err := addCardTokens(ctx, tx, tokens); err != nil {
			return nil, fmt.Errorf("storage: pay mafia winners: %w", err)
		}
		return next, nil
	})
}

// addCardTokens gives each player their card-game tokens, making a row for
// whoever has none.
func addCardTokens(ctx context.Context, tx pgx.Tx, tokens map[int64]int) error {
	users, counts := make([]int64, 0, len(tokens)), make([]int32, 0, len(tokens))
	for user, n := range tokens {
		if n > 0 {
			users, counts = append(users, user), append(counts, int32(n))
		}
	}
	if len(users) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO card_players (user_id, tokens)
		SELECT u, n + $3 FROM unnest($1::bigint[], $2::integer[]) AS t(u, n)
		ON CONFLICT (user_id) DO UPDATE SET tokens = card_players.tokens + EXCLUDED.tokens - $3`,
		users, counts, welcomeTokens())
	return err
}

// ChangeTeamLobby changes a chat's MLBB team lobby as ChangeMafiaGame does
// its game; what it holds is internal/service's.
func (s *Store) ChangeTeamLobby(ctx context.Context, chatID int64, change func(state []byte) ([]byte, error)) error {
	return s.changeState(ctx, "team_lobbies", chatID, change)
}

// ChangeBanVote changes a group's vote to ban a member, with its row locked.
func (s *Store) ChangeBanVote(ctx context.Context, chatID int64, change func(state []byte) ([]byte, error)) error {
	return s.changeState(ctx, "ban_votes", chatID, change)
}

// changeState changes the JSON state of a chat's row in table, one of the
// games' tables named above, with the row locked; an error from change is
// given back as it is.
func (s *Store) changeState(ctx context.Context, table string, chatID int64, change func(state []byte) ([]byte, error)) error {
	return s.changeStateTx(ctx, table, chatID, func(_ pgx.Tx, state []byte) ([]byte, error) { return change(state) })
}

// changeStateTx is changeState with change given the transaction, to write
// in it what is kept with the state.
func (s *Store) changeStateTx(ctx context.Context, table string, chatID int64, change func(tx pgx.Tx, state []byte) ([]byte, error)) error {
	var refused error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO `+table+` (chat_id, state) VALUES ($1, '{}') ON CONFLICT (chat_id) DO NOTHING`, chatID); err != nil {
			return err
		}
		var state []byte
		if err := tx.QueryRow(ctx, `SELECT state FROM `+table+` WHERE chat_id = $1 FOR UPDATE`, chatID).Scan(&state); err != nil {
			return err
		}
		changed, err := change(tx, state)
		if err != nil {
			refused = err
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE `+table+` SET state = $2, updated_at = now() WHERE chat_id = $1`, chatID, changed)
		return err
	})
	if refused != nil {
		return refused
	}
	if err != nil {
		return fmt.Errorf("storage: change %s: %w", table, err)
	}
	return nil
}
