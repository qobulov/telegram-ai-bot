package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A pack is saved once however often it is imported, its stickers wait to be
// labelled, and only enabled ones of the asked category are sent, the one
// used longest ago first, and not again too soon in the same chat.
func TestStickersArePickedByCategoryAndSpaced(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	pack := []PackSticker{
		{FileUniqueID: "u1", FileID: "f1", Pack: "xaa", Emoji: "🫥", ImageFileID: "f1"},
		{FileUniqueID: "u2", FileID: "f2", Pack: "xaa", Emoji: "🫥", ImageFileID: "t2"},
		{FileUniqueID: "u3", FileID: "f3", Pack: "xaa", Emoji: "😂", ImageFileID: "f3"},
	}
	if added, err := store.SaveStickers(ctx, pack); err != nil || added != 3 {
		t.Fatalf("SaveStickers = %d, %v; want 3", added, err)
	}
	if added, err := store.SaveStickers(ctx, pack); err != nil || added != 0 {
		t.Fatalf("SaveStickers again = %d, %v; want 0", added, err)
	}
	waiting, err := store.UnlabelledStickers(ctx, "xaa")
	if err != nil || len(waiting) != 3 || waiting[1].ImageFileID != "t2" {
		t.Fatalf("UnlabelledStickers = %+v, %v", waiting, err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(store.LabelSticker(ctx, "u1", []string{"funny", "sarcasm"}, "a laughing cat", true))
	must(store.LabelSticker(ctx, "u2", []string{"funny"}, "a dancing man", true))
	must(store.LabelSticker(ctx, "u3", []string{"funny"}, "something rude", false))

	if c, err := store.PackStickers(ctx, "xaa"); err != nil || c != (StickerCount{Total: 3, Labelled: 3, Enabled: 2}) {
		t.Errorf("PackStickers = %+v, %v", c, err)
	}
	first, err := store.PickSticker(ctx, group, "funny", time.Minute)
	if err != nil || (first != "f1" && first != "f2") {
		t.Fatalf("PickSticker = %q, %v; want an enabled funny one", first, err)
	}
	if _, err := store.PickSticker(ctx, group, "funny", time.Minute); !errors.Is(err, ErrNoSticker) {
		t.Errorf("a second sticker within the gap: %v, want ErrNoSticker", err)
	}
	second, err := store.PickSticker(ctx, otherGroup, "funny", time.Minute)
	if err != nil || second == first || second == "f3" {
		t.Errorf("another chat got %q, %v; want the other enabled funny one, not %q", second, err, first)
	}
	if _, err := store.PickSticker(ctx, 777, "sad", time.Minute); !errors.Is(err, ErrNoSticker) {
		t.Errorf("a category with no sticker: %v, want ErrNoSticker", err)
	}
	if got, err := store.PickSticker(ctx, group, "sarcasm", 0); err != nil || got != "f1" {
		t.Errorf("with no gap: %q, %v; want f1", got, err)
	}
}

func TestHasStickersOnlyOnceOneIsEnabled(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if has, err := store.HasStickers(ctx); err != nil || has {
		t.Fatalf("empty: %v, %v", has, err)
	}
	if _, err := store.SaveStickers(ctx, []PackSticker{{FileUniqueID: "u1", FileID: "f1", Pack: "xaa"}}); err != nil {
		t.Fatal(err)
	}
	if has, _ := store.HasStickers(ctx); has {
		t.Error("an unlabelled sticker counts")
	}
	if err := store.LabelSticker(ctx, "u1", []string{"funny"}, "cat", true); err != nil {
		t.Fatal(err)
	}
	if has, err := store.HasStickers(ctx); err != nil || !has {
		t.Errorf("after labelling: %v, %v", has, err)
	}
}

// A sticker and a GIF share a chat's slot: one within the gap of the
// other is refused.
func TestStickersAndGIFsShareTheGap(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if ok, err := store.ClaimMediaSlot(ctx, group, time.Minute); err != nil || !ok {
		t.Fatalf("first GIF: %v, %v", ok, err)
	}
	if ok, _ := store.ClaimMediaSlot(ctx, group, time.Minute); ok {
		t.Error("a second GIF within the gap")
	}
	if ok, _ := store.ClaimMediaSlot(ctx, otherGroup, time.Minute); !ok {
		t.Error("another chat was refused")
	}
	if _, err := store.SaveStickers(ctx, []PackSticker{{FileUniqueID: "u1", FileID: "f1", Pack: "xaa"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.LabelSticker(ctx, "u1", []string{"funny"}, "cat", true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PickSticker(ctx, group, "funny", time.Minute); !errors.Is(err, ErrNoSticker) {
		t.Errorf("a sticker right after a GIF: %v, want ErrNoSticker", err)
	}
}

// A pack's stickers are named as item cards one by one; the cards are found
// by name, the stickers that are no card are not, and neither is ever a mood.
func TestItemCards(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.SaveStickers(ctx, []PackSticker{
		{FileUniqueID: "ua", FileID: "fa", Pack: "items", ImageFileID: "ia"},
		{FileUniqueID: "ub", FileID: "fb", Pack: "items", ImageFileID: "ib"},
		{FileUniqueID: "uc", FileID: "fc", Pack: "items", ImageFileID: "ic"},
	}); err != nil {
		t.Fatal(err)
	}
	// A card the mood labeller saw first is named all the same.
	if err := store.LabelSticker(ctx, "ua", []string{"funny"}, "item stats", true); err != nil {
		t.Fatal(err)
	}
	unnamed, err := store.UnnamedCards(ctx, "items")
	if err != nil || len(unnamed) != 3 {
		t.Fatalf("unnamed = %+v, err = %v", unnamed, err)
	}
	if err := store.LabelSticker(ctx, "ua", []string{ItemCard}, "Athena's Shield", true); err != nil {
		t.Fatal(err)
	}
	if err := store.LabelSticker(ctx, "ub", []string{NotItemCard}, "", false); err != nil {
		t.Fatal(err)
	}
	if unnamed, _ := store.UnnamedCards(ctx, "items"); len(unnamed) != 1 || unnamed[0].FileUniqueID != "uc" {
		t.Errorf("unnamed = %+v, want only uc", unnamed)
	}
	if c, err := store.PackCards(ctx, "items"); err != nil || c != (CardCount{Total: 3, Looked: 2, Named: 1}) {
		t.Errorf("count = %+v, err = %v", c, err)
	}
	if cards, err := store.ItemCards(ctx); err != nil || len(cards) != 1 || cards[0] != (ItemCardSticker{FileID: "fa", Name: "Athena's Shield"}) {
		t.Errorf("cards = %+v, err = %v", cards, err)
	}
	if id, _ := store.PickSticker(ctx, -1, "funny", 0); id != "" {
		t.Errorf("a card was picked for a mood: %q", id)
	}
}
