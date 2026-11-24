package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Salvage breaks only the spare copies of a rarity into materials: a card
// keeps its one copy and the copies its stars used, and points stay; a box
// spends materials on one card of its rarity, refused with too few.
func TestSalvageCards(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO card_catalog (id, name, rarity, attack, health, value) VALUES
			(1, 'Common', 0, 100, 100, 100), (2, 'Rare', 1, 100, 100, 200), (3, 'Epic', 2, 100, 100, 400), (4, 'Common two', 0, 100, 100, 100)`,
		`INSERT INTO card_players (user_id, name, points, cards, draws, tokens) VALUES (1, 'Alice', 800, 4, 20, 0)`,
		// Common: 5 copies, ★2 used 1 → 3 spare; Rare: 3 copies → 2 spare;
		// Epic: 4 copies → 3 spare, kept; Common two: 1 copy → none.
		`INSERT INTO card_owned (user_id, card_id, count, melted, stars) VALUES (1, 1, 5, 1, 2), (1, 2, 3, 0, 1), (1, 3, 4, 0, 1), (1, 4, 1, 0, 1)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if s, err := store.CardSpares(ctx, 1); err != nil || s.Materials != 0 || s.Spare[0] != 3 || s.Spare[1] != 2 || s.Spare[2] != 3 {
		t.Fatalf("spares = %+v, %v", s, err)
	}
	if copies, materials, err := store.SalvageCards(ctx, 1, "Alice", 0, 1); err != nil || copies != 3 || materials != 3 {
		t.Fatalf("salvage commons = %d, %d, %v", copies, materials, err)
	}
	if copies, materials, err := store.SalvageCards(ctx, 1, "Alice", 1, 2); err != nil || copies != 2 || materials != 7 {
		t.Fatalf("salvage rares = %d, %d, %v", copies, materials, err)
	}
	if copies, materials, _ := store.SalvageCards(ctx, 1, "Alice", 0, 1); copies != 0 || materials != 7 {
		t.Errorf("salvage again = %d, %d", copies, materials)
	}
	var count, melted, stars, points int
	if err := store.pool.QueryRow(ctx, `SELECT o.count, o.melted, o.stars, p.points FROM card_owned o JOIN card_players p USING (user_id) WHERE card_id = 1`).
		Scan(&count, &melted, &stars, &points); err != nil || count != 5 || melted != 4 || stars != 2 || points != 800 {
		t.Errorf("the common after = %d copies %d used ★%d, %d points, %v", count, melted, stars, points, err)
	}
	// a reset gives back only the copies its stars used
	if free, err := store.ResetCardStars(ctx, 1, 1, []int{0, 1, 2, 3, 4}); err != nil || free != 1 {
		t.Errorf("reset after salvage = %d free, %v", free, err)
	}
	if s, _ := store.CardSpares(ctx, 1); s.Materials != 7 || s.Spare[2] != 3 || s.Spare[1] != 0 {
		t.Errorf("spares after = %+v", s)
	}
	epic := func(int, int) (int, bool) { return 2, false }
	if _, _, err := store.OpenMaterialBox(ctx, 1, "Alice", epic, 8, time.Now(), CardQuest{}); !errors.Is(err, ErrCardMaterials) {
		t.Errorf("a box past the materials = %v", err)
	}
	open, materials, err := store.OpenMaterialBox(ctx, 1, "Alice", epic, 5, time.Now(), CardQuest{})
	if err != nil || materials != 2 || len(open.Draws) != 1 || open.Draws[0].Card.Rarity != 2 {
		t.Errorf("a box = %+v, %d, %v", open, materials, err)
	}
}
