package storage

import (
	"context"
	"testing"
)

// A boost is new once per group; the same ID in another group is another
// boost.
func TestNoteBoostOncePerGroup(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, c := range []struct {
		chatID int64
		boost  string
		want   bool
	}{
		{group, "b1", true},
		{group, "b1", false},
		{group, "b2", true},
		{otherGroup, "b1", true},
	} {
		if got, err := store.NoteBoost(ctx, c.chatID, c.boost); err != nil || got != c.want {
			t.Errorf("NoteBoost(%d, %s) = %v, %v; want %v", c.chatID, c.boost, got, err, c.want)
		}
	}
}
