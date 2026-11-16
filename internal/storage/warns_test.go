package storage

import (
	"context"
	"testing"
)

// A member's warnings count up, one comes off, and they clear; another chat
// keeps its own.
func TestWarnsCountUpAndClear(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for want := 1; want <= 3; want++ {
		if n, err := store.AddWarn(ctx, -100, 7); err != nil || n != want {
			t.Fatalf("warning %d = %d, %v", want, n, err)
		}
	}
	if n, err := store.RemoveWarn(ctx, -100, 7); err != nil || n != 2 {
		t.Fatalf("one taken away = %d, %v", n, err)
	}
	if n, _ := store.AddWarn(ctx, -200, 7); n != 1 {
		t.Errorf("another chat's first = %d", n)
	}
	if err := store.ResetWarns(ctx, -100, 7); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.RemoveWarn(ctx, -100, 7); n != 0 {
		t.Errorf("after clearing, one taken away = %d", n)
	}
	if n, _ := store.AddWarn(ctx, -100, 7); n != 1 {
		t.Errorf("after clearing, a warning = %d", n)
	}
}
