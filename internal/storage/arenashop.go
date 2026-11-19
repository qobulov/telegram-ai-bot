package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Errors of the arena's shop.
var (
	// ErrArenaCoins is a purchase the player's arena coins do not cover.
	ErrArenaCoins = errors.New("storage: too few arena coins")
	// ErrArenaShopDaily is a token bought past the day's limit.
	ErrArenaShopDaily = errors.New("storage: the day's arena shop tokens are bought")
)

// arenaWallet is a player's arena coins and the tokens they bought in the
// arena's shop on the day asked, both as locked.
type arenaWallet struct{ coins, tokens int }

// lockArenaWallet makes the player's arena row where missing and locks it;
// tokens bought on a day before day read as 0. The arena row is locked
// before the cards' row, as RecordArenaBattle does.
func lockArenaWallet(ctx context.Context, tx pgx.Tx, userID int64, day string) (arenaWallet, error) {
	var w arenaWallet
	if _, err := tx.Exec(ctx, `INSERT INTO arena_players (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		return w, err
	}
	err := tx.QueryRow(ctx, `
		SELECT coins, CASE WHEN shop_day = $2::date THEN shop_tokens ELSE 0 END
		FROM arena_players WHERE user_id = $1 FOR UPDATE`, userID, day).Scan(&w.coins, &w.tokens)
	return w, err
}

// BuyArenaToken buys the player one card token for price arena coins:
// ErrArenaCoins with too few, ErrArenaShopDaily once they bought daily on
// day (the start of the day in Tashkent, its date in its own location
// counting). It gives the coins and the tokens after.
func (s *Store) BuyArenaToken(ctx context.Context, userID int64, name string, price, daily int, day time.Time) (coins, tokens int, err error) {
	date := day.Format(time.DateOnly)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		w, err := lockArenaWallet(ctx, tx, userID, date)
		switch {
		case err != nil:
			return err
		case w.tokens >= daily:
			return ErrArenaShopDaily
		case w.coins < price:
			return ErrArenaCoins
		}
		if _, err := lockCardPlayer(ctx, tx, userID, name); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			UPDATE arena_players SET coins = coins - $2, shop_day = $3::date, shop_tokens = $4
			WHERE user_id = $1 RETURNING coins`, userID, price, date, w.tokens+1).Scan(&coins); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE card_players SET tokens = tokens + 1 WHERE user_id = $1 RETURNING tokens`, userID).Scan(&tokens)
	})
	if errors.Is(err, ErrArenaCoins) || errors.Is(err, ErrArenaShopDaily) {
		return 0, 0, err
	}
	if err != nil {
		return 0, 0, fmt.Errorf("storage: buy an arena token: %w", err)
	}
	return coins, tokens, nil
}

// OpenArenaPack opens n cards for price arena coins, roll picking each
// one's rarity, as OpenCardPack does for crystals; with too few coins it
// opens none, ErrArenaCoins. It gives the opening and the coins after.
func (s *Store) OpenArenaPack(ctx context.Context, userID int64, name string, n int, roll CardRoll, price int, now time.Time, quest CardQuest) (CardOpening, int, error) {
	var (
		open  CardOpening
		coins int
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		w, err := lockArenaWallet(ctx, tx, userID, now.Format(time.DateOnly))
		if err != nil {
			return err
		}
		if w.coins < price {
			return ErrArenaCoins
		}
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		if open.Draws, err = drawPack(ctx, tx, userID, name, n, roll, player, quest); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `UPDATE arena_players SET coins = coins - $2 WHERE user_id = $1 RETURNING coins`, userID, price).Scan(&coins); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE card_players SET last_draw_at = $2 WHERE user_id = $1 RETURNING tokens, shards, points`,
			userID, now).Scan(&open.Tokens, &open.Shards, &open.Points)
	})
	if errors.Is(err, ErrArenaCoins) {
		return CardOpening{}, 0, err
	}
	if err != nil {
		return CardOpening{}, 0, fmt.Errorf("storage: open an arena pack: %w", err)
	}
	return open, coins, nil
}
