package storage

import (
	"context"
	"errors"
	"testing"
)

// Spare copies melt into shards and stay counted in the points; shards draw
// a card out of turn; a duel moves the stake once, however often its button
// is pressed.
func TestShardsAndDuels(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SaveCardCatalog(ctx, []Card{
		{ID: 1, Name: "Gojo", Rarity: 4, Attack: 5000, Health: 20000, Value: 5000},
		{ID: 2, Name: "Usopp", Rarity: 0, Attack: 400, Health: 1500, Value: 100},
	}); err != nil {
		t.Fatal(err)
	}
	const alice, bob = 1, 2
	for _, q := range []string{
		`INSERT INTO card_players (user_id, name, points, cards, draws) VALUES (1, 'Alice', 5300, 2, 4), (2, 'Bob', 100, 1, 1)`,
		`INSERT INTO card_owned (user_id, card_id, count) VALUES (1, 1, 1), (1, 2, 3), (2, 2, 1)`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	rates := []int{1, 3, 8, 20, 50}
	if shards, spares, err := store.CardWallet(ctx, alice); err != nil || shards != 0 || spares[0] != 2 {
		t.Fatalf("Alice's wallet = %d %v, %v", shards, spares, err)
	}
	melted, gained, shards, err := store.MeltCards(ctx, alice, rates)
	if err != nil || melted != 2 || gained != 2 || shards != 2 {
		t.Fatalf("melt = %d %d %d, %v", melted, gained, shards, err)
	}
	if melted, gained, _, err = store.MeltCards(ctx, alice, rates); err != nil || melted != 0 || gained != 0 {
		t.Errorf("a second melt = %d %d, %v", melted, gained, err)
	}
	if _, _, _, err = store.MeltCards(ctx, 99, rates); err != nil {
		t.Errorf("a melt by no player = %v", err)
	}
	if p, _, err := store.CardPlayerOf(ctx, alice); err != nil || p.Points != 5300 {
		t.Errorf("Alice's points after melting = %d, %v", p.Points, err)
	}

	_, err = store.ExtraCard(ctx, alice, "Alice", rarity(0), 15)
	var few *ShardsError
	if !errors.As(err, &few) || few.Have != 2 || few.Need != 15 {
		t.Fatalf("an extra card for too few shards = %v", err)
	}
	draw, err := store.ExtraCard(ctx, alice, "Alice", rarity(0), 2)
	if err != nil || draw.Card.ID != 2 || draw.Count != 4 || draw.Shards != 0 || draw.Points != 5400 {
		t.Fatalf("an extra card = %+v, %v", draw, err)
	}
	if _, last, err := store.CardPlayerOf(ctx, alice); err != nil || !last.IsZero() {
		t.Errorf("an extra card moved the hourly card to %v, %v", last, err)
	}

	team, err := store.DuelTeam(ctx, alice, 3)
	if err != nil || len(team) != 2 || team[0].ID != 1 {
		t.Fatalf("Alice's team = %+v, %v", team, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE card_players SET shards = 7 WHERE user_id = 2`); err != nil {
		t.Fatal(err)
	}
	if moved, settled, err := store.ResolveDuel(ctx, -100, 5, alice, bob, 10); err != nil || !settled || moved != 7 {
		t.Fatalf("the duel = %d %v, %v", moved, settled, err)
	}
	if moved, settled, err := store.ResolveDuel(ctx, -100, 5, alice, bob, 10); err != nil || settled || moved != 0 {
		t.Errorf("the duel pressed again = %d %v, %v", moved, settled, err)
	}
	if shards, _, _ := store.CardWallet(ctx, alice); shards != 7 {
		t.Errorf("Alice won %d shards, want 7", shards)
	}
	if _, settled, err := store.ResolveDuel(ctx, -100, 6, 0, 0, 0); err != nil || !settled {
		t.Errorf("a refused duel = %v, %v", settled, err)
	}
}
