package cards

import (
	"context"
	"errors"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// SalvageMaterials are the 🧱 materials a spare copy breaks into, by its
// rarity, as the owners asked on 2026-11-24: only Common and Rare copies
// break, a Rare one for twice a Common's, as a Rare is worth twice the
// points. A card always keeps its one copy, so a player's cards and
// points stay.
var SalvageMaterials = map[Rarity]int{Common: 1, Rare: 2}

// MaterialBox is a box bought for materials: one card of Rarity.
type MaterialBox struct {
	ID     string `json:"id"`
	Rarity Rarity `json:"rarity"`
	Price  int    `json:"price"`
}

// MaterialBoxes are the boxes materials buy. A draw gives an Epic once in
// 11, a Legendary once in 40 and a Mythic once in 200, and its Common and
// Rare copies break into about 1.1 materials a draw: a box costs about six
// times the materials of the draws that would give its card, so that
// salvage helps, and drawing stays the way to cards. The Mythic box costs
// 2000 since 2026-11-24, as the owners asked, from 1500.
var MaterialBoxes = []MaterialBox{
	{ID: "epic", Rarity: Epic, Price: 80},
	{ID: "legendary", Rarity: Legendary, Price: 400},
	{ID: "mythic", Rarity: Mythic, Price: 2000},
}

func materialBox(id string) (MaterialBox, bool) {
	for _, box := range MaterialBoxes {
		if box.ID == id {
			return box, true
		}
	}
	return MaterialBox{}, false
}

// salvageView is the player's materials, their spare copies that break
// into them, with the materials of each, and the boxes they buy.
func (h *handler) salvageView(ctx context.Context, user miniapp.User) (any, failure) {
	spares, err := h.Store.CardSpares(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read a player's spare cards", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return h.salvageOf(spares), failure{}
}

func (h *handler) salvageOf(spares storage.SpareCards) map[string]any {
	salvage := []map[string]any{}
	for _, r := range []Rarity{Common, Rare} {
		salvage = append(salvage, map[string]any{"rarity": r, "spare": spares.Spare[int(r)], "per": SalvageMaterials[r]})
	}
	return map[string]any{"materials": spares.Materials, "salvage": salvage, "boxes": MaterialBoxes}
}

// salvage breaks all the player's spare copies of a Common or Rare rarity
// into materials, and answers the view after.
func (h *handler) salvage(ctx context.Context, user miniapp.User, rarity *int) (any, failure) {
	if rarity == nil {
		return nil, errBadRequest
	}
	per, ok := SalvageMaterials[Rarity(*rarity)]
	if !ok {
		return nil, errBadRequest
	}
	copies, _, err := h.Store.SalvageCards(ctx, user.ID, name(user), *rarity, per)
	if err != nil {
		h.Logger.Error("could not salvage cards", "user_id", user.ID, "rarity", *rarity, "error", err)
		return nil, errUnavailable
	}
	view, fail := h.salvageView(ctx, user)
	if fail != (failure{}) {
		return nil, fail
	}
	out := view.(map[string]any)
	out["broken"], out["gained"] = copies, copies*per
	return out, failure{}
}

// materialBoxOpen buys a material box and opens its card, as an arena pack
// is opened; "materials" when the player's do not cover it.
func (h *handler) materialBoxOpen(ctx context.Context, user miniapp.User, id string) (any, failure) {
	box, ok := materialBox(id)
	if !ok {
		return nil, errBadRequest
	}
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	now := h.Now()
	roll := func(int, int) (int, bool) { return int(box.Rarity), false }
	opened, materials, err := h.Store.OpenMaterialBox(ctx, user.ID, name(user), roll, box.Price, now, questOf(QuestOpen, now))
	if errors.Is(err, storage.ErrCardMaterials) {
		return map[string]any{"refused": "materials"}, failure{}
	}
	if err != nil {
		h.Logger.Error("could not open a material box", "user_id", user.ID, "box", id, "error", err)
		return nil, errUnavailable
	}
	drawn := make([]map[string]any, len(opened.Draws))
	for i, d := range opened.Draws {
		drawn[i] = map[string]any{"card": h.held(d.Card, d.Count), "new": d.New, "serial": d.Serial}
	}
	return map[string]any{"drawn": drawn, "points": opened.Points, "tokens": opened.Tokens, "shards": opened.Shards, "materials": materials}, failure{}
}
