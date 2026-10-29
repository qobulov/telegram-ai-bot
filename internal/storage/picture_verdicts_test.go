package storage

import (
	"context"
	"testing"
)

// A picture's verdict is kept once, the first one standing, and a picture
// never judged has none.
func TestPictureVerdictsAreKeptOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, found, err := store.PictureVerdict(ctx, "p1"); err != nil || found {
		t.Fatalf("before = %v %v, want none", found, err)
	}
	if err := store.SavePictureVerdict(ctx, PictureVerdict{FileUniqueID: "p1", Unsafe: true, Reason: "nudity", Judge: JudgeModel}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePictureVerdict(ctx, PictureVerdict{FileUniqueID: "p1", Judge: JudgeSightengine}); err != nil {
		t.Fatalf("saving again: %v", err)
	}
	if err := store.SavePictureVerdict(ctx, PictureVerdict{FileUniqueID: "p2", Judge: JudgeSightengine}); err != nil {
		t.Fatal(err)
	}

	got, found, err := store.PictureVerdict(ctx, "p1")
	if err != nil || !found || got != (PictureVerdict{FileUniqueID: "p1", Unsafe: true, Reason: "nudity", Judge: JudgeModel}) {
		t.Errorf("p1 = %+v %v %v, want the first verdict", got, found, err)
	}
	if got, found, _ := store.PictureVerdict(ctx, "p2"); !found || got.Unsafe || got.Judge != JudgeSightengine {
		t.Errorf("p2 = %+v %v", got, found)
	}
}
