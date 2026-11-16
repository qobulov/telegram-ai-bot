package storage

import (
	"context"
	"testing"
	"time"
)

// /autocard turns a player's auto opener on and off; the opener finds the
// players with a free card left today, longest waiting first, and stops for
// one.
func TestCardAutoOpenerFindsTheReadyPlayers(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	day := CardDay{Start: time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), PerDay: 3}
	for _, q := range []string{
		`INSERT INTO card_players (user_id, name, last_draw_at, free_day, free_used) VALUES (1, 'Alice', now() - interval '2 hours', '2026-11-16 00:00+00', 2), (2, 'Bob', now() - interval '10 minutes', '2026-11-16 00:00+00', 3), (3, 'Carol', NULL, NULL, 0), (4, 'Dan', now() - interval '3 hours', NULL, 0)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{1, 2, 3} {
		if on, err := store.ToggleCardAuto(ctx, id, "x", "ru"); err != nil || !on {
			t.Fatalf("turning %d on = %v, %v", id, on, err)
		}
	}
	// A player not yet in the table is made, with the opener on.
	if on, err := store.ToggleCardAuto(ctx, 5, "Eve", "en"); err != nil || !on {
		t.Fatalf("a new player = %v, %v", on, err)
	}
	if on, err := store.ToggleCardAuto(ctx, 5, "Eve", "en"); err != nil || on {
		t.Fatalf("turning it off = %v, %v", on, err)
	}
	due, err := store.DueCardAutos(ctx, day, 10)
	if err != nil || len(due) != 2 || due[0].UserID != 3 || due[1].UserID != 1 || due[1].Language != "ru" || due[1].Name != "Alice" {
		t.Fatalf("due = %+v, %v", due, err)
	}
	if due, _ = store.DueCardAutos(ctx, day, 1); len(due) != 1 {
		t.Errorf("limited to one = %+v", due)
	}
	if err := store.StopCardAuto(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if due, _ = store.DueCardAutos(ctx, day, 10); len(due) != 1 || due[0].UserID != 1 {
		t.Errorf("after stopping Carol = %+v", due)
	}
}
