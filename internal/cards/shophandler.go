package cards

import (
	"context"
	"errors"
	"fmt"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// buy buys a shop item for crystals: tokens, or a pack of cards with one
// sure of its rarity; with too few crystals it says how many are missing.
func (h *handler) buy(ctx context.Context, user miniapp.User, id string) (any, failure) {
	item, ok := ShopItemByID(id)
	if !ok {
		return nil, errBadRequest
	}
	var (
		after storage.CardOpening
		err   error
	)
	if item.Tokens > 0 {
		after, err = h.Store.BuyCardTokens(ctx, user.ID, name(user), item.Tokens, item.Price)
	} else {
		if err := h.catalog.Ensure(ctx); err != nil {
			h.Logger.Error("could not fill the card catalog", "error", err)
			return nil, errUnavailable
		}
		now := h.Now()
		after, err = h.Store.OpenCardPack(ctx, user.ID, name(user), item.Cards, PackRoll(h.Roll, item.Cards, item.Least), item.Price, now, questOf(QuestOpen, now))
	}
	var few *storage.CardFundsError
	switch {
	case errors.As(err, &few):
		return map[string]any{"short": few.Need - few.Shards, "shards": few.Shards}, failure{}
	case err != nil:
		h.Logger.Error("could not sell a shop item", "user_id", user.ID, "item", id, "error", err)
		return nil, errUnavailable
	}
	drawn := make([]map[string]any, len(after.Draws))
	for i, d := range after.Draws {
		drawn[i] = map[string]any{"card": h.held(d.Card, d.Count), "new": d.New, "serial": d.Serial}
	}
	return map[string]any{"drawn": drawn, "points": after.Points, "tokens": after.Tokens, "shards": after.Shards}, failure{}
}

// invoice makes the link to pay for a crystal pack in Stars, made out to
// the player, which the app opens; the crystals come when Telegram says
// the payment went through.
func (h *handler) invoice(ctx context.Context, user miniapp.User, id string) (any, failure) {
	pack, ok := CrystalPackByID(id)
	if !ok || h.Invoices == nil {
		return nil, errBadRequest
	}
	invoice := telegram.Invoice{
		Title:       fmt.Sprintf("💎 %d kristall", pack.Crystals),
		Description: fmt.Sprintf("Kartalar o'yini uchun %d kristall: token, karta paketlari va karta ochish uchun.", pack.Crystals),
		Payload:     CrystalPayload(user.ID, pack),
		Label:       fmt.Sprintf("%d kristall", pack.Crystals),
		Price:       pack.Stars,
	}
	if len(pack.Box) > 0 {
		most := pack.Box[len(pack.Box)-1].Crystals
		invoice.Title = "🎁 Almaz box"
		invoice.Description = fmt.Sprintf("Kartalar o'yini uchun %d dan %d gacha kristall: qancha ko'p bo'lsa, shuncha kam tushadi.", pack.Crystals, most)
		invoice.Label = "Almaz box"
	}
	link, err := h.Invoices.CreateInvoiceLink(ctx, invoice)
	if err != nil {
		h.Logger.Error("could not make a crystal invoice", "user_id", user.ID, "pack", id, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{"link": link}, failure{}
}
