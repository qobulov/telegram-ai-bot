package cards

import (
	"context"
	"errors"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// Arena coins, as the owners asked on 2026-11-19: each attack gives the
// attacker ArenaCoinsWin won and ArenaCoinsLoss lost, spent in the arena's
// shop; a player buys at most ArenaShopTokensDaily tokens there a day, so
// that tokens stay scarce.
const (
	ArenaCoinsWin        = 10
	ArenaCoinsLoss       = 5
	ArenaShopTokensDaily = 10
)

// ArenaShopItems are the arena shop's goods, for coins: a token, an arena
// ticket (as the owners asked on 2026-11-21; it costs more than the coins
// an attack wins, so tickets bought never pay for themselves), and packs of
// five with a sure Epic or Legendary as the crystal shop sells.
var ArenaShopItems = []ShopItem{
	{ID: "token", Price: 30, Tokens: 1},
	{ID: "ticket", Price: 20, Tickets: 1},
	{ID: "epic", Price: 150, Cards: 5, Least: Epic},
	{ID: "legendary", Price: 450, Cards: 5, Least: Legendary},
}

func arenaShopItem(id string) (ShopItem, bool) {
	for _, item := range ArenaShopItems {
		if item.ID == id {
			return item, true
		}
	}
	return ShopItem{}, false
}

// arenaShop buys an arena shop item for coins: a token, or a pack opened as
// the crystal shop opens one, or an arena ticket. A purchase refused says
// why: "coins" (too few), "daily" (the day's tokens bought) or
// "tickets_full" (the tickets already full).
func (h *handler) arenaShop(ctx context.Context, user miniapp.User, id string) (any, failure) {
	item, ok := arenaShopItem(id)
	if !ok {
		return nil, errBadRequest
	}
	if item.Tickets > 0 {
		return h.arenaTicket(ctx, user, item.Price)
	}
	if item.Tokens > 0 {
		coins, tokens, err := h.Store.BuyArenaToken(ctx, user.ID, name(user), item.Price, ArenaShopTokensDaily, DayStart(h.Now()))
		if refused, ok := arenaShopRefusal(err); ok {
			return refused, failure{}
		}
		if err != nil {
			h.Logger.Error("could not sell an arena token", "user_id", user.ID, "error", err)
			return nil, errUnavailable
		}
		return map[string]any{"coins": coins, "tokens": tokens}, failure{}
	}
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	now := h.Now()
	opened, coins, err := h.Store.OpenArenaPack(ctx, user.ID, name(user), item.Cards, PackRoll(h.Roll, item.Cards, item.Least), item.Price, now, questOf(QuestOpen, now))
	if refused, ok := arenaShopRefusal(err); ok {
		return refused, failure{}
	}
	if err != nil {
		h.Logger.Error("could not open an arena pack", "user_id", user.ID, "item", id, "error", err)
		return nil, errUnavailable
	}
	drawn := make([]map[string]any, len(opened.Draws))
	for i, d := range opened.Draws {
		drawn[i] = map[string]any{"card": h.held(d.Card, d.Count), "new": d.New, "serial": d.Serial}
	}
	return map[string]any{"drawn": drawn, "points": opened.Points, "tokens": opened.Tokens, "shards": opened.Shards, "coins": coins}, failure{}
}

func (h *handler) arenaTicket(ctx context.Context, user miniapp.User, price int) (any, failure) {
	coins, tickets, next, err := h.Store.BuyArenaTicket(ctx, user.ID, price, arenaRules(h.Now()))
	if refused, ok := arenaShopRefusal(err); ok {
		return refused, failure{}
	}
	if err != nil {
		h.Logger.Error("could not sell an arena ticket", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{"coins": coins, "tickets": tickets, "next_ticket_in": next.Milliseconds()}, failure{}
}

func arenaShopRefusal(err error) (map[string]any, bool) {
	switch {
	case errors.Is(err, storage.ErrArenaCoins):
		return map[string]any{"refused": "coins"}, true
	case errors.Is(err, storage.ErrArenaShopDaily):
		return map[string]any{"refused": "daily"}, true
	case errors.Is(err, storage.ErrArenaTicketsFull):
		return map[string]any{"refused": "tickets_full"}, true
	}
	return nil, false
}
