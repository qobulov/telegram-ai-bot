package storage

import (
	"context"
	"testing"
)

// A player's place counts those above them on each table, and one off the
// table has place 0.
func TestPlaces(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'A', 300, 3, 3), (2, 'B', 200, 2, 2), (3, 'C', 200, 1, 1), (4, 'D', 0, 0, 0)`,
		`INSERT INTO arena_players (user_id, rating, wins) VALUES (1, 1000, 1), (2, 1100, 0), (3, 1000, 5)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for user, want := range map[int64]int{1: 1, 2: 2, 3: 3, 4: 0, 9: 0} {
		me, place, total, err := store.CardPlace(ctx, user)
		if err != nil || place != want || (want > 0 && (total != 3 || me.UserID != user)) {
			t.Errorf("card place of %d = %d of %d (%+v), %v; want %d", user, place, total, me, err, want)
		}
	}
	for user, want := range map[int64]int{2: 1, 3: 2, 1: 3, 4: 0} {
		if place, total, err := store.ArenaPlace(ctx, user); err != nil || place != want || (want > 0 && total != 3) {
			t.Errorf("arena place of %d = %d of %d, %v; want %d", user, place, total, err, want)
		}
	}
}
