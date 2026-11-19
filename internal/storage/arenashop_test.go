package storage

import (
	"context"
	"errors"
	"testing"
)

func arenaCoins(t *testing.T, store *Store, userID int64) int {
	t.Helper()
	p, err := store.ArenaPlayerOf(context.Background(), userID, arenaRules)
	if err != nil {
		t.Fatal(err)
	}
	return p.Coins
}

func setArenaCoins(t *testing.T, store *Store, userID int64, coins int) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `
		INSERT INTO arena_players (user_id, coins) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET coins = $2`, userID, coins); err != nil {
		t.Fatal(err)
	}
}

// An attack gives the attacker CoinsWin won and CoinsLoss lost, the
// defender nothing.
func TestArenaCoinsForAttacks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	rules := arenaRules
	rules.CoinsWin, rules.CoinsLoss = 10, 5
	out, err := store.RecordArenaBattle(ctx, 1, 2, 1, true, rules, CardQuest{})
	if err != nil || out.CoinsWon != 10 || out.Coins != 10 {
		t.Fatalf("a win = %+v, %v", out, err)
	}
	out, err = store.RecordArenaBattle(ctx, 1, 2, 2, false, rules, CardQuest{})
	if err != nil || out.CoinsWon != 5 || out.Coins != 15 {
		t.Fatalf("a loss = %+v, %v", out, err)
	}
	if got := arenaCoins(t, store, 1); got != 15 {
		t.Errorf("attacker's coins %d, want 15", got)
	}
	if got := arenaCoins(t, store, 2); got != 0 {
		t.Errorf("defender's coins %d, want 0", got)
	}
}

// A token takes its price in coins and adds a card token, refused with too
// few coins and past the day's limit, which the next day resets.
func TestBuyArenaToken(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	setArenaCoins(t, store, 1, 100)
	coins, tokens, err := store.BuyArenaToken(ctx, 1, "Alice", 30, 2, arenaDay)
	if err != nil || coins != 70 || tokens != 1 {
		t.Fatalf("a token = %d coins, %d tokens, %v", coins, tokens, err)
	}
	if _, _, err := store.BuyArenaToken(ctx, 1, "Alice", 30, 2, arenaDay); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.BuyArenaToken(ctx, 1, "Alice", 30, 2, arenaDay); !errors.Is(err, ErrArenaShopDaily) {
		t.Fatalf("a third token the same day = %v, want ErrArenaShopDaily", err)
	}
	p, _ := store.ArenaPlayerOf(ctx, 1, arenaRules)
	if p.ShopTokens != 2 || p.Coins != 40 || cardTokens(t, store, 1) != 2 {
		t.Errorf("after two: %+v, tokens %d", p, cardTokens(t, store, 1))
	}
	next := arenaDay.AddDate(0, 0, 1)
	if coins, _, err := store.BuyArenaToken(ctx, 1, "Alice", 30, 2, next); err != nil || coins != 10 {
		t.Fatalf("the next day = %d, %v", coins, err)
	}
	if _, _, err := store.BuyArenaToken(ctx, 1, "Alice", 30, 2, next); !errors.Is(err, ErrArenaCoins) {
		t.Fatalf("with 10 coins = %v, want ErrArenaCoins", err)
	}
	if got := arenaCoins(t, store, 1); got != 10 {
		t.Errorf("a refused purchase took coins: %d left", got)
	}
	if _, _, err := store.BuyArenaToken(ctx, 9, "Nobody", 30, 2, arenaDay); !errors.Is(err, ErrArenaCoins) {
		t.Errorf("a player never in the arena = %v", err)
	}
}

// A pack takes its price in coins and gives its cards, none with too few.
func TestOpenArenaPack(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	setArenaCoins(t, store, 1, 200)
	legendary := func(int, int) (int, bool) { return 3, false }
	open, coins, err := store.OpenArenaPack(ctx, 1, "Alice", 5, legendary, 150, arenaNow, CardQuest{})
	if err != nil || len(open.Draws) != 5 || coins != 50 {
		t.Fatalf("a pack = %d cards, %d coins, %v", len(open.Draws), coins, err)
	}
	var held int
	if err := store.pool.QueryRow(ctx, `SELECT coalesce(sum(count), 0) FROM card_owned WHERE user_id = 1`).Scan(&held); err != nil || held != 15 {
		t.Errorf("copies held %d, want 15 (%v)", held, err)
	}
	if _, _, err := store.OpenArenaPack(ctx, 1, "Alice", 5, legendary, 150, arenaNow, CardQuest{}); !errors.Is(err, ErrArenaCoins) {
		t.Fatalf("with 50 coins = %v, want ErrArenaCoins", err)
	}
	if got := arenaCoins(t, store, 1); got != 50 {
		t.Errorf("coins after a refused pack %d", got)
	}
}
