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

	home, err := store.CreateKassaBook(ctx, user, "Uy")
	if err != nil {
		t.Fatalf("CreateKassaBook: %v", err)
	}
	shop, _ := store.CreateKassaBook(ctx, user, "Do'kon")
	theirs, _ := store.CreateKassaBook(ctx, other, "Boshqa")

	for _, e := range []KassaEntry{
		{Kind: KassaIn, Amount: 500_000, Note: "oylik", Day: day("2026-11-20")},
		{Kind: KassaIn, Amount: 100_000, Note: "qarz qaytdi", Day: day("2026-11-22")},
		{Kind: KassaOut, Amount: 120_000, Note: "bozor", Day: day("2026-11-21")},
	} {
		if _, err := store.AddKassaEntry(ctx, user, home, e); err != nil {
			t.Fatalf("AddKassaEntry: %v", err)
		}
	}
	if _, err := store.AddKassaEntry(ctx, user, shop, KassaEntry{Kind: KassaIn, Amount: 7, Day: day("2026-11-21")}); err != nil {
		t.Fatalf("AddKassaEntry shop: %v", err)
	}
	if _, err := store.AddKassaEntry(ctx, user, theirs, KassaEntry{Kind: KassaIn, Amount: 1, Day: day("2026-11-21")}); !errors.Is(err, ErrNoKassaBook) {
		t.Fatalf("writing to another user's book: %v, want ErrNoKassaBook", err)
	}
	otherEntry, _ := store.AddKassaEntry(ctx, other, theirs, KassaEntry{Kind: KassaOut, Amount: 1, Day: day("2026-11-21")})

	books, err := store.KassaBooks(ctx, user)
	if err != nil {
		t.Fatalf("KassaBooks: %v", err)
	}
	if len(books) != 2 || books[0].Name != "Uy" || books[0].Totals != (KassaTotals{In: 600_000, Out: 120_000, Balance: 480_000}) || books[1].Totals.Balance != 7 {
		t.Fatalf("books = %+v", books)
	}
	in, err := store.KassaEntries(ctx, user, home, KassaIn, 10)
	if err != nil {
		t.Fatalf("KassaEntries: %v", err)
	}
	if len(in) != 2 || in[0].Note != "qarz qaytdi" || !in[0].Day.Equal(day("2026-11-22")) {
		t.Fatalf("in = %+v, want newest day first", in)
	}
	if got, _ := store.KassaEntries(ctx, user, theirs, KassaOut, 10); len(got) != 0 {
		t.Fatalf("another user's entries leaked: %+v", got)
	}

	if err := store.DeleteKassaEntry(ctx, user, otherEntry); !errors.Is(err, ErrNoKassaEntry) {
		t.Fatalf("deleting another user's entry: %v", err)
	}
	if err := store.DeleteKassaEntry(ctx, user, in[0].ID); err != nil {
		t.Fatalf("DeleteKassaEntry: %v", err)
	}
	if b, _ := store.KassaBook(ctx, user, home); b.Totals.Balance != 380_000 || b.Name != "Uy" {
		t.Fatalf("book after delete = %+v", b)
	}
	if _, err := store.KassaBook(ctx, user, theirs); !errors.Is(err, ErrNoKassaBook) {
		t.Fatalf("reading another user's book: %v", err)
	}

	if err := store.RenameKassaBook(ctx, user, shop, "Magazin"); err != nil {
		t.Fatalf("RenameKassaBook: %v", err)
	}
	if err := store.RenameKassaBook(ctx, user, theirs, "x"); !errors.Is(err, ErrNoKassaBook) {
		t.Fatalf("renaming another user's book: %v", err)
	}
	if err := store.DeleteKassaBook(ctx, user, theirs); !errors.Is(err, ErrNoKassaBook) {
		t.Fatalf("deleting another user's book: %v", err)
	}
	if err := store.DeleteKassaBook(ctx, user, home); err != nil {
		t.Fatalf("DeleteKassaBook: %v", err)
	}
	if books, _ := store.KassaBooks(ctx, user); len(books) != 1 || books[0].Name != "Magazin" {
		t.Fatalf("books after delete = %+v", books)
	}
	if got, _ := store.KassaEntries(ctx, user, home, KassaIn, 10); len(got) != 0 {
		t.Fatalf("a deleted book's entries remain: %+v", got)
	}

	for i := 1; i < MaxKassaBooks; i++ {
		if _, err := store.CreateKassaBook(ctx, user, "k"); err != nil {
			t.Fatalf("book %d: %v", i, err)
		}
	}
	if _, err := store.CreateKassaBook(ctx, user, "one too many"); !errors.Is(err, ErrKassaBooksFull) {
		t.Fatalf("book past the limit: %v, want ErrKassaBooksFull", err)
	}
}
