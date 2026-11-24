package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestKassa(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const user, other = int64(7_000_000_001), int64(42)
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }

	for _, e := range []KassaEntry{
		{Kind: KassaIn, Amount: 500_000, Note: "oylik", Day: day("2026-11-20")},
		{Kind: KassaIn, Amount: 100_000, Note: "qarz qaytdi", Day: day("2026-11-22")},
		{Kind: KassaOut, Amount: 120_000, Note: "bozor", Day: day("2026-11-21")},
	} {
		if _, err := store.AddKassaEntry(ctx, user, e); err != nil {
			t.Fatalf("AddKassaEntry: %v", err)
		}
	}
	otherID, err := store.AddKassaEntry(ctx, other, KassaEntry{Kind: KassaOut, Amount: 1, Day: day("2026-11-21")})
	if err != nil {
		t.Fatalf("AddKassaEntry other: %v", err)
	}

	totals, err := store.KassaTotals(ctx, user)
	if err != nil {
		t.Fatalf("KassaTotals: %v", err)
	}
	if totals != (KassaTotals{In: 600_000, Out: 120_000, Balance: 480_000}) {
		t.Fatalf("totals = %+v", totals)
	}
	in, err := store.KassaEntries(ctx, user, KassaIn, 10)
	if err != nil {
		t.Fatalf("KassaEntries: %v", err)
	}
	if len(in) != 2 || in[0].Note != "qarz qaytdi" || in[1].Amount != 500_000 || !in[0].Day.Equal(day("2026-11-22")) {
		t.Fatalf("in = %+v, want newest day first", in)
	}

	if err := store.DeleteKassaEntry(ctx, user, otherID); !errors.Is(err, ErrNoKassaEntry) {
		t.Fatalf("deleting another user's entry: %v, want ErrNoKassaEntry", err)
	}
	if err := store.DeleteKassaEntry(ctx, user, in[0].ID); err != nil {
		t.Fatalf("DeleteKassaEntry: %v", err)
	}
	if totals, _ := store.KassaTotals(ctx, user); totals.Balance != 380_000 {
		t.Fatalf("balance after delete = %d, want 380000", totals.Balance)
	}
	if totals, _ := store.KassaTotals(ctx, 999); totals != (KassaTotals{}) {
		t.Fatalf("empty book totals = %+v", totals)
	}
}
