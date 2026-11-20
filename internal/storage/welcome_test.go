package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	makeRow := func(id int64, name string) {
		t.Helper()
		if err := pgx.BeginFunc(ctx, store.pool, func(tx pgx.Tx) error {
			_, err := lockCardPlayer(ctx, tx, id, name)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	makeRow(alice, "Alice")
	if n := tokens(alice); n != WelcomeTokens {
		t.Errorf("a new player has %d tokens, want %d", n, WelcomeTokens)
	}
	makeRow(alice, "Alice")
	if n := tokens(alice); n != WelcomeTokens {
		t.Errorf("the row made again gave %d tokens, want %d once", n, WelcomeTokens)
	}
	WelcomeUntil = time.Now().Add(-time.Hour)
	makeRow(bob, "Bob")
	if n := tokens(bob); n != 0 {
		t.Errorf("after the welcome, a new player has %d tokens", n)
	}
}
