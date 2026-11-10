package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNoShards is returned by ExtraCard when the player has too few shards:
// how many they have is in ShardsError.
var ErrNoShards = errors.New("storage: too few shards")

// ShardsError says how many shards the player has, and how many it took.
type ShardsError struct{ Have, Need int }

func (e *ShardsError) Error() string { return ErrNoShards.Error() }
func (e *ShardsError) Unwrap() error { return ErrNoShards }

// CardWallet is the player's 💎 shards, and for each rarity the spare
// copies of cards they hold that are not melted yet: all of a card's copies
// but one.
func (s *Store) CardWallet(ctx context.Context, userID int64) (shards int, spares map[int]int, err error) {
	if err := s.pool.QueryRow(ctx, `SELECT coalesce((SELECT shards FROM card_players WHERE user_id = $1), 0)`, userID).Scan(&shards); err != nil {
		return 0, nil, fmt.Errorf("storage: read a player's shards: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.rarity, sum(o.count - 1 - o.melted)::int FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
		WHERE o.user_id = $1 AND o.count - 1 - o.melted > 0 GROUP BY c.rarity`, userID)
	if err != nil {
		return 0, nil, fmt.Errorf("storage: count a player's spare cards: %w", err)
	}
	defer rows.Close()
	spares = map[int]int{}
	for rows.Next() {
		var rarity, n int
		if err := rows.Scan(&rarity, &n); err != nil {
			return 0, nil, fmt.Errorf("storage: count a player's spare cards: %w", err)
		}
		spares[rarity] = n
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("storage: count a player's spare cards: %w", err)
	}
	return shards, spares, nil
}

// MeltCards melts the player's spare copies into shards, rates[r] for one of
// rarity r: the copies stay held and counted in the points, so that melting
// costs no place on the table. It gives how many it melted, the shards they
// made and the player's shards after.
func (s *Store) MeltCards(ctx context.Context, userID int64, rates []int) (melted, gained, shards int, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT shards FROM card_players WHERE user_id = $1 FOR UPDATE`, userID).Scan(&shards)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			WITH spare AS (
				SELECT o.card_id, o.count - 1 - o.melted AS n, c.rarity
				FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
				WHERE o.user_id = $1 AND o.count - 1 - o.melted > 0
			), melt AS (
				UPDATE card_owned o SET melted = o.count - 1 FROM spare
				WHERE o.user_id = $1 AND o.card_id = spare.card_id
			)
			SELECT coalesce(sum(n), 0)::int, coalesce(sum(n * coalesce(($2::int[])[rarity + 1], 0)), 0)::int FROM spare`,
			userID, rates).Scan(&melted, &gained)
		if err != nil || gained == 0 {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE card_players SET shards = shards + $2 WHERE user_id = $1 RETURNING shards`, userID, gained).Scan(&shards)
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("storage: melt cards: %w", err)
	}
	return melted, gained, shards, nil
}

// ExtraCard gives the player a card out of turn for cost shards, as
// DrawCard does but leaving their hourly card where it was; with too few it
// returns a *ShardsError.
func (s *Store) ExtraCard(ctx context.Context, userID int64, name string, roll CardRoll, cost int) (CardDraw, error) {
	var draw CardDraw
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		if player.shards < cost {
			return &ShardsError{Have: player.shards, Need: cost}
		}
		if draw, err = giveCard(ctx, tx, userID, name, roll, player); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE card_players SET shards = shards - $2 WHERE user_id = $1 RETURNING shards`, userID, cost).Scan(&draw.Shards)
	})
	var few *ShardsError
	if errors.As(err, &few) {
		return CardDraw{}, few
	}
	if err != nil {
		return CardDraw{}, fmt.Errorf("storage: draw a card for shards: %w", err)
	}
	return draw, nil
}

// DuelTeam is the player's n strongest cards, by attack and health.
func (s *Store) DuelTeam(ctx context.Context, userID int64, n int) ([]Card, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.anime, c.image_url, c.favourites, c.rarity, c.attack, c.health, c.value, c.collection
		FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
		WHERE o.user_id = $1 ORDER BY c.attack + c.health DESC, c.id LIMIT $2`, userID, n)
	if err != nil {
		return nil, fmt.Errorf("storage: read a duel team: %w", err)
	}
	team, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Card, error) {
		var c Card
		err := row.Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read a duel team: %w", err)
	}
	return team, nil
}

// ResolveDuel settles the duel of the message: the winner takes stake of
// the loser's shards, or all they have when fewer, and moved is how many. A
// duel settled before, by an earlier press, is not settled again: settled is
// false and nothing moves. A winner of 0 closes it with no fight, refused.
func (s *Store) ResolveDuel(ctx context.Context, chatID, messageID, winner, loser int64, stake int) (moved int, settled bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO card_duels (chat_id, message_id, winner_id, loser_id) VALUES ($1, $2, nullif($3, 0), nullif($4, 0))
			ON CONFLICT DO NOTHING`, chatID, messageID, winner, loser)
		if err != nil || tag.RowsAffected() == 0 || winner == 0 || stake <= 0 {
			settled = err == nil && tag.RowsAffected() == 1
			return err
		}
		settled = true
		rows, err := tx.Query(ctx, `SELECT user_id, shards FROM card_players WHERE user_id IN ($1, $2) ORDER BY user_id FOR UPDATE`, winner, loser)
		if err != nil {
			return err
		}
		shards := map[int64]int{}
		for rows.Next() {
			var id int64
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return err
			}
			shards[id] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		moved = min(stake, shards[loser])
		if moved == 0 {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE card_players SET shards = shards + CASE WHEN user_id = $1 THEN $3::int ELSE -$3::int END WHERE user_id IN ($1, $2)`, winner, loser, moved); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE card_duels SET stake = $3 WHERE chat_id = $1 AND message_id = $2`, chatID, messageID, moved)
		return err
	})
	if err != nil {
		return 0, false, fmt.Errorf("storage: settle a duel: %w", err)
	}
	return moved, settled, nil
}
