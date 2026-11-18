package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestMain turns the welcome tokens off, as the tests count tokens from
// zero; TestWelcomeTokens turns them on for itself.
func TestMain(m *testing.M) {
	WelcomeUntil = time.Time{}
	os.Exit(m.Run())
}

// A player new to the cards starts with the welcome tokens while it lasts,
// however their row is made; after it, with none.
func TestWelcomeTokens(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	until := WelcomeUntil
	t.Cleanup(func() { WelcomeUntil = until })
	WelcomeUntil = time.Now().Add(time.Hour)
	tokens := func(id int64) int {
		var n int
		_ = store.pool.QueryRow(ctx, `SELECT tokens FROM card_players WHERE user_id = $1`, id).Scan(&n)
		return n
	}
	if err := store.SetCardAutoRemind(ctx, alice, "Alice", 0, "uz"); err != nil {
		t.Fatal(err)
	}
	if n := tokens(alice); n != WelcomeTokens {
		t.Errorf("a player made by the reminder has %d tokens, want %d", n, WelcomeTokens)
	}
	if err := store.SetCardAutoRemind(ctx, alice, "Alice", 0, "uz"); err != nil {
		t.Fatal(err)
	}
	if n := tokens(alice); n != WelcomeTokens {
		t.Errorf("the reminder set again gave %d tokens, want %d once", n, WelcomeTokens)
	}
	WelcomeUntil = time.Now().Add(-time.Hour)
	if err := store.SetCardAutoRemind(ctx, bob, "Bob", 0, "uz"); err != nil {
		t.Fatal(err)
	}
	if n := tokens(bob); n != 0 {
		t.Errorf("after the welcome, a new player has %d tokens", n)
	}
}
