package cards

import (
	"context"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

func (f *fakeStore) BuyArenaToken(_ context.Context, userID int64, _ string, price, daily int, _ time.Time) (int, int, error) {
	switch {
	case f.arena.bought[userID] >= daily:
		return 0, 0, storage.ErrArenaShopDaily
	case f.arena.coins[userID] < price:
		return 0, 0, storage.ErrArenaCoins
	}
	if f.arena.bought == nil {
		f.arena.bought = map[int64]int{}
	}
	f.arena.coins[userID] -= price
	f.arena.bought[userID]++
	f.tokens++
	return f.arena.coins[userID], f.tokens, nil
}

func (f *fakeStore) OpenArenaPack(_ context.Context, userID int64, _ string, n int, roll storage.CardRoll, price int, _ time.Time, quest storage.CardQuest) (storage.CardOpening, int, error) {
	if f.arena.coins[userID] < price {
		return storage.CardOpening{}, 0, storage.ErrArenaCoins
	}
	f.arena.coins[userID] -= price
	open := storage.CardOpening{Tokens: f.tokens, Shards: f.shards}
	for i := range n {
		rarity, _ := roll(0, 0)
		card := lockedCard
		card.Rarity = rarity
		open.Draws = append(open.Draws, storage.CardDraw{Card: card, Count: 1, New: true, Serial: i + 1})
	}
	f.count(userID, quest, n)
	return open, f.arena.coins[userID], nil
}

// An attack gives coins won or lost, which the arena shows and its shop
// spends: a token, refused past the day's limit or with too few coins, and
// a pack with its sure rarity.
func TestArenaCoinsAndShop(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	held := map[int64][]storage.OwnedCard{7: arenaCards(), 8: arenaCards()}
	store := &fakeStore{arena: fakeArena{held: held}}
	call := arenaHandler(t, store, now)
	for _, user := range []int64{7, 8} {
		if code, out := call(user, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack}); code != 200 || out["refused"] != nil {
			t.Fatalf("team of %d: %d %v", user, code, out)
		}
	}
	_, out := call(7, map[string]any{"action": "arena_attack", "opponent": 8})
	want := float64(ArenaCoinsLoss)
	if out["won"] == true {
		want = ArenaCoinsWin
	}
	if out["coins_won"] != want || out["coins"] != want {
		t.Fatalf("an attack gave %v coins (%v after), want %v", out["coins_won"], out["coins"], want)
	}
	_, view := call(7, map[string]any{"action": "arena"})
	if view["player"].(map[string]any)["coins"] != want {
		t.Errorf("the arena shows %v coins", view["player"].(map[string]any)["coins"])
	}
	shop := view["shop"].(map[string]any)
	if len(shop["items"].([]any)) != len(ArenaShopItems) || shop["tokens_left"] != float64(ArenaShopTokensDaily) {
		t.Errorf("the arena's shop = %v", shop)
	}

	store.arena.coins[7] = 1000
	if _, out := call(7, map[string]any{"action": "arena_shop", "item": "token"}); out["coins"] != float64(970) || out["tokens"] != float64(1) {
		t.Fatalf("a token = %v", out)
	}
	store.arena.bought[7] = ArenaShopTokensDaily
	if _, out := call(7, map[string]any{"action": "arena_shop", "item": "token"}); out["refused"] != "daily" {
		t.Errorf("past the day's tokens = %v", out)
	}
	_, out = call(7, map[string]any{"action": "arena_shop", "item": "legendary"})
	drawn, _ := out["drawn"].([]any)
	if len(drawn) != 5 || out["coins"] != float64(970-450) {
		t.Fatalf("a Legendary pack = %v", out)
	}
	if got := drawn[4].(map[string]any)["card"].(map[string]any)["rarity"]; got != float64(Legendary) {
		t.Errorf("the pack's sure card is of rarity %v", got)
	}
	store.arena.coins[7] = 449
	if _, out := call(7, map[string]any{"action": "arena_shop", "item": "legendary"}); out["refused"] != "coins" || store.arena.coins[7] != 449 {
		t.Errorf("with %d coins = %v", store.arena.coins[7], out)
	}
	if code, _ := call(7, map[string]any{"action": "arena_shop", "item": "mythic"}); code != 400 {
		t.Errorf("an item not sold: %d", code)
	}
}
