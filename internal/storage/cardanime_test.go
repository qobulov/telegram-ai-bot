package storage

import (
	"context"
	"sync"
	"testing"
)

// An anime's reward is given once, even to claims made at the same time.
func TestAnimeRewardOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	setWallet(t, store, 1, 3, 0)

	var wg sync.WaitGroup
	given := make(chan bool, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimed, err := store.ClaimAnimeReward(ctx, 1, "Ali", "ONE PIECE", 10)
			if err != nil {
				t.Error(err)
			}
			given <- claimed
		}()
	}
	wg.Wait()
	close(given)
	n := 0
	for claimed := range given {
		if claimed {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the reward was given %d times", n)
	}
	after, claimed, err := store.ClaimAnimeReward(ctx, 1, "Ali", "ONE PIECE", 10)
	if err != nil || claimed || after != 13 {
		t.Fatalf("a later claim = %d, %v, %v; want the 13 tokens kept", after, claimed, err)
	}
	if _, claimed, _ := store.ClaimAnimeReward(ctx, 1, "Ali", "Naruto", 10); !claimed {
		t.Error("another anime's reward is its own")
	}
	got, err := store.AnimeRewardsClaimed(ctx, 1)
	if err != nil || len(got) != 2 || got[0] != "ONE PIECE" || got[1] != "Naruto" {
		t.Fatalf("claimed = %v, %v", got, err)
	}
}
