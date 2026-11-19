package cards

import (
	"context"
	"errors"
	mrand "math/rand/v2"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// mailShown is how many of the newest letters the mailbox shows.
const mailShown = 20

// BoxByID is the box a crystal pack sells, e.g. "box", the diamond box: the
// boxes the inventory holds and mail sends.
func BoxByID(id string) (CrystalPack, bool) {
	pack, ok := CrystalPackByID(id)
	if !ok || len(pack.Box) == 0 {
		return CrystalPack{}, false
	}
	return pack, true
}

type mailView struct {
	ID       int64  `json:"id"`
	Note     string `json:"note"`
	Box      string `json:"box,omitempty"`
	Boxes    int    `json:"boxes,omitempty"`
	Tokens   int    `json:"tokens,omitempty"`
	Crystals int    `json:"crystals,omitempty"`
	At       int64  `json:"at"`
	Claimed  bool   `json:"claimed"`
}

// inbox is the player's 🎒: the boxes they hold, with each box's prizes,
// and their newest letters.
func (h *handler) inbox(ctx context.Context, user miniapp.User) (any, failure) {
	held, err := h.Store.CardBoxes(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read a player's boxes", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	mail, err := h.Store.CardMailbox(ctx, user.ID, mailShown)
	if err != nil {
		h.Logger.Error("could not read a player's mail", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	boxes := []map[string]any{}
	for id, count := range held {
		if pack, ok := BoxByID(id); ok {
			boxes = append(boxes, map[string]any{"id": id, "count": count, "box": pack.Box})
		}
	}
	letters := make([]mailView, len(mail))
	for i, m := range mail {
		letters[i] = mailView{ID: m.ID, Note: m.Note, Box: m.Box, Boxes: m.Boxes, Tokens: m.Tokens, Crystals: m.Crystals,
			At: m.SentAt.UnixMilli(), Claimed: m.ClaimedAt != nil}
	}
	return map[string]any{"boxes": boxes, "mail": letters}, failure{}
}

// mailClaim takes a letter's gifts; one claimed before, or not the
// player's, answers "claimed".
func (h *handler) mailClaim(ctx context.Context, user miniapp.User, id int64) (any, failure) {
	if id <= 0 {
		return nil, errBadRequest
	}
	m, after, claimed, err := h.Store.ClaimCardMail(ctx, user.ID, name(user), id)
	if err != nil {
		h.Logger.Error("could not claim card mail", "user_id", user.ID, "mail", id, "error", err)
		return nil, errUnavailable
	}
	if !claimed {
		return map[string]any{"refused": "claimed"}, failure{}
	}
	return map[string]any{"boxes": m.Boxes, "box": m.Box, "tokens_given": m.Tokens, "crystals_given": m.Crystals,
		"tokens": after.Tokens, "shards": after.Shards}, failure{}
}

// boxOpen opens one of the player's boxes: its prize drawn as a bought
// one's is; "none" when they hold no such box.
func (h *handler) boxOpen(ctx context.Context, user miniapp.User, id string) (any, failure) {
	pack, ok := BoxByID(id)
	if !ok {
		return nil, errBadRequest
	}
	won := pack.Draw(h.boxRoll())
	shards, left, err := h.Store.OpenCardBox(ctx, user.ID, name(user), id, won)
	switch {
	case errors.Is(err, storage.ErrNoCardBox):
		return map[string]any{"refused": "none"}, failure{}
	case err != nil:
		h.Logger.Error("could not open a card box", "user_id", user.ID, "box", id, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{"won": won, "least": pack.Box[0].Crystals, "shards": shards, "left": left}, failure{}
}

func (h *handler) boxRoll() func(int) int {
	if h.BoxRoll != nil {
		return h.BoxRoll
	}
	return mrand.IntN
}
