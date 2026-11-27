package storage

import (
	"context"
	"testing"
	"time"
)

// A payment adds its grams to the buyer's farosat in its group once, counts
// in the ranking, and a refund takes them back once.
func TestFarosatPurchases(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, _, _, err := store.TakeFarosat(ctx, -100, 2, "Vali", time.Now(), 30); err != nil {
		t.Fatal(err)
	}
	buy := FarosatPurchase{ChargeID: "c1", ChatID: -100, UserID: 1, Name: "Ali", Stars: 50, Grams: 50}
	if grams, recorded, err := store.RecordFarosatPurchase(ctx, buy); err != nil || !recorded || grams != 50 {
		t.Fatalf("first record = %d %v %v", grams, recorded, err)
	}
	if _, recorded, err := store.RecordFarosatPurchase(ctx, buy); err != nil || recorded {
		t.Fatalf("second record = %v %v", recorded, err)
	}
	if grams, place, err := store.FarosatOf(ctx, -100, 1); err != nil || grams != 50 || place != 1 {
		t.Fatalf("Ali = %d, place %d, %v", grams, place, err)
	}
	if grams, place, err := store.FarosatOf(ctx, -100, 3); err != nil || grams != 0 || place != 3 {
		t.Fatalf("a member without farosat = %d, place %d, %v", grams, place, err)
	}
	if found, err := store.RefundFarosatPurchase(ctx, "c1"); err != nil || !found {
		t.Fatalf("refund = %v %v", found, err)
	}
	if found, _ := store.RefundFarosatPurchase(ctx, "c1"); found {
		t.Error("refunded twice")
	}
	if grams, _, _ := store.FarosatOf(ctx, -100, 1); grams != 0 {
		t.Errorf("after the refund = %d", grams)
	}
}

// A galaxy bought is kept once and open until its payment is refunded.
func TestFarosatGalaxies(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	buy := FarosatGalaxyPurchase{ChargeID: "g1", ChatID: -100, UserID: 1, Galaxy: 12, Stars: 50}
	if recorded, err := store.RecordFarosatGalaxy(ctx, buy); err != nil || !recorded {
		t.Fatalf("record = %v %v", recorded, err)
	}
	if recorded, _ := store.RecordFarosatGalaxy(ctx, buy); recorded {
		t.Error("recorded twice")
	}
	if ids, err := store.FarosatGalaxies(ctx, -100, 1); err != nil || len(ids) != 1 || ids[0] != 12 {
		t.Fatalf("galaxies = %v %v", ids, err)
	}
	if ids, _ := store.FarosatGalaxies(ctx, -200, 1); len(ids) != 0 {
		t.Errorf("another group's = %v", ids)
	}
	if found, err := store.RefundFarosatGalaxy(ctx, "g1"); err != nil || !found {
		t.Fatalf("refund = %v %v", found, err)
	}
	if ids, _ := store.FarosatGalaxies(ctx, -100, 1); len(ids) != 0 {
		t.Errorf("after the refund = %v", ids)
	}
}
