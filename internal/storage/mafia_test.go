package storage

import (
	"context"
	"errors"
	"testing"
)

// A chat's game starts as {}, keeps what a change gives back, and stays as
// it was when a change refuses.
func TestChangeMafiaGame(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	var seen []string
	change := func(next string) func([]byte) ([]byte, map[int64]int, error) {
		return func(state []byte) ([]byte, map[int64]int, error) {
			seen = append(seen, string(state))
			return []byte(next), nil, nil
		}
	}
	if err := store.ChangeMafiaGame(ctx, -5, change(`{"phase": "lobby"}`)); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("no")
	if err := store.ChangeMafiaGame(ctx, -5, func([]byte) ([]byte, map[int64]int, error) { return nil, nil, refused }); !errors.Is(err, refused) {
		t.Fatalf("a refused change: %v", err)
	}
	if err := store.ChangeMafiaGame(ctx, -5, change(`{"phase": "night"}`)); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "{}" || seen[1] != `{"phase": "lobby"}` {
		t.Errorf("seen %q", seen)
	}
}

// The winners a change names get their card tokens with the game's end:
// a player who had a row keeps their tokens and gets more, one who had none
// gets a row, a loser gets nothing, and a refused change pays no one.
func TestChangeMafiaGamePaysTokens(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const carol, dave = int64(1003), int64(1004)
	if _, err := store.pool.Exec(ctx, `INSERT INTO card_players (user_id, name, tokens) VALUES ($1, 'Alice', 3), ($2, 'Dave', 1)`, alice, dave); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("no")
	if err := store.ChangeMafiaGame(ctx, -6, func([]byte) ([]byte, map[int64]int, error) {
		return nil, map[int64]int{alice: 5}, refused
	}); !errors.Is(err, refused) {
		t.Fatalf("a refused change: %v", err)
	}
	// The game ends with this change; its next changes find it over and pay
	// no one, as the rules give no winners twice.
	ended := false
	end := func(state []byte) ([]byte, map[int64]int, error) {
		if string(state) == `{"phase": "over"}` {
			return state, nil, nil
		}
		ended = true
		return []byte(`{"phase": "over"}`), map[int64]int{alice: 2, bob: 2, carol: 0}, nil
	}
	for range 3 {
		if err := store.ChangeMafiaGame(ctx, -6, end); err != nil {
			t.Fatal(err)
		}
	}
	if !ended {
		t.Fatal("the game never ended")
	}
	for user, want := range map[int64]int{alice: 5, bob: 2, carol: 0, dave: 1} {
		p, _, err := store.CardPlayerOf(ctx, user)
		if err != nil || p.Tokens != want {
			t.Errorf("user %d has %d tokens, want %d (%v)", user, p.Tokens, want, err)
		}
	}
	var rows int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM card_players WHERE user_id = $1`, carol).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("a row for a player paid nothing: %d %v", rows, err)
	}
}
