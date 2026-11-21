package storage

import (
	"context"
	"testing"
	"time"
)

// Words add up per member and day; the table sums the days from since on,
// most words first, and one member's alone when asked.
func TestWordTop(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	const chat = -100777
	add := func(user int64, name string, d time.Time, words int) {
		t.Helper()
		if err := store.AddMemberWords(ctx, chat, user, name, d, words); err != nil {
			t.Fatal(err)
		}
	}
	add(1, "Ali", day, 5)
	add(1, "Ali B", day, 7)
	add(2, "Vali", day.AddDate(0, 0, -1), 20)
	add(2, "Vali", day.AddDate(0, 0, -10), 100)
	add(3, "Sam", day, 999)
	if err := store.AddMemberWords(ctx, -1, 3, "x", day, 1); err != nil {
		t.Fatal(err)
	}

	top, err := store.WordTop(ctx, chat, day.AddDate(0, 0, -6), 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 3 || top[0].UserID != 3 || top[1].UserID != 2 || top[1].Words != 20 || top[2].Name != "Ali B" || top[2].Words != 12 || top[2].Messages != 2 {
		t.Fatalf("week top = %+v", top)
	}
	mine, err := store.WordTop(ctx, chat, day.AddDate(0, 0, -29), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].Words != 120 || mine[0].Messages != 2 {
		t.Fatalf("own month = %+v", mine)
	}
}
