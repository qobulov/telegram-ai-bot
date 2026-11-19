package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Crystals bought are added once per payment and taken back on a refund;
// they buy tokens and packs, and too few buy nothing.
func TestCardShop(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	// Migrations seed some Mythics (the Four Knights' Lancelot and Tristan);
	// the packs here draw from this test's cards alone.
	if _, err := store.pool.Exec(ctx, `DELETE FROM card_catalog WHERE rarity = 4`); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCardCatalog(ctx, []Card{
		{ID: 1, Name: "Gojo", Rarity: 4, Attack: 5000, Health: 20000, Value: 5000},
		{ID: 2, Name: "Usopp", Rarity: 0, Attack: 400, Health: 1500, Value: 100},
	}); err != nil {
		t.Fatal(err)
	}
	const user = int64(8292769065)
	shards, recorded, err := store.RecordCrystalPurchase(ctx, CrystalPurchase{ChargeID: "c1", UserID: user, Name: "Ali", Stars: 25, Crystals: 100})
	if err != nil || !recorded || shards != 100 {
		t.Fatalf("purchase = %d %v %v", shards, recorded, err)
	}
	if _, recorded, _ := store.RecordCrystalPurchase(ctx, CrystalPurchase{ChargeID: "c1", UserID: user, Stars: 25, Crystals: 100}); recorded {
		t.Error("the same payment was added twice")
	}
	after, err := store.BuyCardTokens(ctx, user, "Ali", 5, 60)
	if err != nil || after.Shards != 40 || after.Tokens != 5 {
		t.Fatalf("tokens = %+v, %v", after, err)
	}
	var few *CardFundsError
	if _, err := store.BuyCardTokens(ctx, user, "Ali", 5, 60); !errors.As(err, &few) || few.Shards != 40 {
		t.Errorf("too few for tokens = %v", err)
	}
	mythic := func(int, int) (int, bool) { return 4, false }
	open, err := store.OpenCardPack(ctx, user, "Ali", 2, mythic, 30, time.Now())
	if err != nil || len(open.Draws) != 2 || open.Shards != 10 || open.Tokens != 5 || open.Draws[0].Card.ID != 1 {
		t.Fatalf("pack = %+v, %v", open, err)
	}
	if _, err := store.OpenCardPack(ctx, user, "Ali", 2, mythic, 30, time.Now()); !errors.As(err, &few) {
		t.Errorf("too few for a pack = %v", err)
	}
	if marked, err := store.MarkCrystalRefunded(ctx, "c1"); err != nil || !marked {
		t.Fatalf("refund = %v %v", marked, err)
	}
	if shards, _ := store.CardWallet(ctx, user); shards != 0 {
		t.Errorf("crystals after the refund = %d, want none left below 0", shards)
	}
	if marked, _ := store.MarkCrystalRefunded(ctx, "c1"); marked {
		t.Error("refunded twice")
	}
}
