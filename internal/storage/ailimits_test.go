package storage

import (
	"context"
	"testing"
	"time"
)

// A chat's lost replies count up within an outage, each chat on its own,
// and from 1 again once it has had none for the quiet time.
func TestNoteAILimitCountsAnOutage(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	quiet := 6 * time.Hour
	for i, want := range []int{1, 2, 3, 4} {
		if n, err := store.NoteAILimit(ctx, -100, start.Add(time.Duration(i)*time.Minute), quiet); err != nil || n != want {
			t.Fatalf("lost reply %d = %d, %v; want %d", i+1, n, err, want)
		}
	}
	if n, _ := store.NoteAILimit(ctx, 42, start, quiet); n != 1 {
		t.Errorf("another chat's first = %d, want 1", n)
	}
	if n, _ := store.NoteAILimit(ctx, -100, start.Add(3*time.Minute+quiet+time.Second), quiet); n != 1 {
		t.Errorf("after a quiet time = %d, want 1 again", n)
	}
}
