package storage

import (
	"context"
	"errors"
	"testing"
)

// A chat's game starts as {}, keeps what a change gives back, and stays as
// it was when a change refuses; coins go to heroes old and new.
func TestChangeMafiaGame(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	var seen []string
	change := func(next string) func([]byte) ([]byte, error) {
		return func(state []byte) ([]byte, error) {
			seen = append(seen, string(state))
			return []byte(next), nil
		}
	}
	if err := store.ChangeMafiaGame(ctx, -5, change(`{"phase": "lobby"}`)); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("no")
	if err := store.ChangeMafiaGame(ctx, -5, func([]byte) ([]byte, error) { return nil, refused }); !errors.Is(err, refused) {
		t.Fatalf("a refused change: %v", err)
	}
	if err := store.ChangeMafiaGame(ctx, -5, change(`{"phase": "night"}`)); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "{}" || seen[1] != `{"phase": "lobby"}` {
		t.Errorf("seen %q", seen)
	}

	if _, err := store.ChangeRaidHero(ctx, alice, 9, func(h *RaidHero) error { h.Coins = 40; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRaidCoins(ctx, []int64{alice, bob}, 150); err != nil {
		t.Fatal(err)
	}
	for user, want := range map[int64]int64{alice: 190, bob: 150} {
		hero, err := store.ChangeRaidHero(ctx, user, 9, func(*RaidHero) error { return nil })
		if err != nil || hero.Coins != want {
			t.Errorf("user %d has %d coins, want %d (%v)", user, hero.Coins, want, err)
		}
	}
}

// A chat's room keeps what a change gives back; a player's levels are their
// hero's, or the first ones when they have none.
func TestChangeMaydonRoomAndLevels(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ChangeMaydonRoom(ctx, -7, func([]byte) ([]byte, error) { return []byte(`{"phase": "lobby"}`), nil }); err != nil {
		t.Fatal(err)
	}
	var seen string
	if err := store.ChangeMaydonRoom(ctx, -7, func(state []byte) ([]byte, error) { seen = string(state); return state, nil }); err != nil || seen != `{"phase": "lobby"}` {
		t.Fatalf("kept %q, %v", seen, err)
	}
	if sword, crit, err := store.RaidLevels(ctx, alice); err != nil || sword != 1 || crit != 0 {
		t.Errorf("no hero: %d %d %v", sword, crit, err)
	}
	if _, err := store.ChangeRaidHero(ctx, alice, 9, func(h *RaidHero) error { h.Sword, h.Crit = 12, 5; return nil }); err != nil {
		t.Fatal(err)
	}
	if sword, crit, err := store.RaidLevels(ctx, alice); err != nil || sword != 12 || crit != 5 {
		t.Errorf("a hero: %d %d %v", sword, crit, err)
	}
}
