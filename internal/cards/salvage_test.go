package cards

import (
	"context"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

func (f *fakeStore) CardSpares(context.Context, int64) (storage.SpareCards, error) {
	spare := map[int]int{}
	for r, n := range f.spare {
		spare[r] = n
	}
	return storage.SpareCards{Materials: f.materials, Spare: spare}, nil
}

func (f *fakeStore) SalvageCards(_ context.Context, _ int64, _ string, rarity, per int) (int, int, error) {
	copies := f.spare[rarity]
	delete(f.spare, rarity)
	f.materials += copies * per
	return copies, f.materials, nil
}

func (f *fakeStore) OpenMaterialBox(_ context.Context, userID int64, _ string, roll storage.CardRoll, price int, _ time.Time, quest storage.CardQuest) (storage.CardOpening, int, error) {
	if f.materials < price {
		return storage.CardOpening{}, 0, storage.ErrCardMaterials
	}
	f.materials -= price
	rarity, _ := roll(0, 0)
	card := lockedCard
	card.Rarity = rarity
	f.count(userID, quest, 1)
	return storage.CardOpening{Draws: []storage.CardDraw{{Card: card, Count: 1, New: true}}}, f.materials, nil
}

// Spare Common and Rare copies break into materials, a Rare one for two;
// an Epic's are not broken; a box spends materials on a card of its
// rarity, refused with too few.
func TestSalvageAndMaterialBoxes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{spare: map[int]int{0: 50, 1: 20, 2: 9}}
	call := arenaHandler(t, store, now)
	_, view := call(7, map[string]any{"action": "salvage"})
	if view["materials"] != 0.0 || len(view["salvage"].([]any)) != 2 || len(view["boxes"].([]any)) != len(MaterialBoxes) {
		t.Fatalf("the view = %v", view)
	}
	if code, out := call(7, map[string]any{"action": "salvage_do", "rarity": 2}); code != 400 {
		t.Errorf("an Epic salvage = %d %v, want refused", code, out)
	}
	_, out := call(7, map[string]any{"action": "salvage_do", "rarity": 1})
	if out["broken"] != 20.0 || out["gained"] != 40.0 || out["materials"] != 40.0 {
		t.Errorf("a Rare salvage = %v", out)
	}
	call(7, map[string]any{"action": "salvage_do", "rarity": 0})
	if store.materials != 90 || store.spare[2] != 9 {
		t.Errorf("materials %d, spares %v", store.materials, store.spare)
	}
	if _, out := call(7, map[string]any{"action": "material_box", "item": "legendary"}); out["refused"] != "materials" {
		t.Errorf("a box past the materials = %v", out)
	}
	_, out = call(7, map[string]any{"action": "material_box", "item": "epic"})
	drawn, _ := out["drawn"].([]any)
	if len(drawn) != 1 || drawn[0].(map[string]any)["card"].(map[string]any)["rarity"] != float64(Epic) || out["materials"] != 10.0 {
		t.Errorf("an Epic box = %v", out)
	}
}
