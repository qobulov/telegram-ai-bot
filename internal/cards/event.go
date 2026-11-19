package cards

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// The Firefly Wedding event, as the owners asked: for two weeks the shop
// sells pulls for tokens at the usual odds (no Mythic boost since
// 2026-11-19), EventShare of the Mythic ones Satoko Kirigaya, the limited
// card: 0.4% a pull, under the Mythic's 0.5%. The EventPity-th pull in a
// row without her is sure to give her.
const (
	EventCard  int64 = 298761 // Satoko Kirigaya
	EventShare       = 0.8
	EventPity        = 200
	EventPrice       = 1 // tokens a pull
	EventBatch       = 10
)

// EventStarts is when the event (started again by the owners) opens:
// 2026-11-18 22:00 in Tashkent; before it the shop shows it as coming.
// EventEnds is when it ends: 2026-12-02 00:00 in Tashkent.
var (
	EventStarts = time.Date(2026, 11, 18, 22, 0, 0, 0, Location)
	EventEnds   = time.Date(2026, 12, 2, 0, 0, 0, 0, Location)
)

// EventRoll draws an event pull's rarity from a number in [0, 1000), at
// the usual odds.
func EventRoll(n int) Rarity {
	return Roll(n)
}

// EventOdds are an event pull's odds of each rarity in a thousand, the most
// common first.
func EventOdds() []int {
	odds := make([]int, len(Rarities))
	for n := range 1000 {
		odds[EventRoll(n)]++
	}
	return odds
}

func (h *handler) eventRoll() Rarity {
	if h.EventRoll != nil {
		return h.EventRoll()
	}
	return EventRoll(rand.IntN(1000))
}

func (h *handler) eventPick() bool {
	if h.EventPick != nil {
		return h.EventPick()
	}
	return rand.Float64() < EventShare
}

// eventView is the event as the shop shows it.
func (h *handler) eventView(ctx context.Context, user miniapp.User) (any, failure) {
	now := h.Now()
	pulls, card, err := h.Store.CardEventPulls(ctx, user.ID, EventCard)
	if err != nil {
		h.Logger.Error("could not read the card event", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	// A player holds the limited card once at most; one who has it is told so.
	owned := false
	if held, err := h.Store.OwnedCards(ctx, user.ID, int(Mythic)); err == nil {
		for _, c := range held {
			owned = owned || c.ID == EventCard
		}
	}
	return map[string]any{
		"owned":  owned,
		"active": eventRuns(now), "soon": now.Before(EventStarts),
		"starts_in": max(0, EventStarts.Sub(now).Milliseconds()),
		"ends_at":   EventEnds.UnixMilli(), "ends_in": max(0, EventEnds.Sub(now).Milliseconds()),
		"pity": pulls, "pity_max": EventPity, "odds": EventOdds(), "share": EventShare,
		"price": EventPrice, "batch": EventBatch, "card": h.held(card, 0),
	}, failure{}
}

// eventOpen pulls 1 or EventBatch event cards for tokens. A pull refused
// says why: "soon" before it starts, "over" once it has ended, "funds" with
// too few tokens.
func (h *handler) eventOpen(ctx context.Context, user miniapp.User, n int) (any, failure) {
	if n != 1 && n != EventBatch {
		return nil, errBadRequest
	}
	now := h.Now()
	if now.Before(EventStarts) {
		return map[string]any{"refused": "soon"}, failure{}
	}
	if !now.Before(EventEnds) {
		return map[string]any{"refused": "over"}, failure{}
	}
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	ev := storage.CardEvent{
		Card: EventCard, Ends: EventEnds, Pity: EventPity, Price: EventPrice,
		Roll: ByOdds(h.eventRoll), Pick: h.eventPick,
	}
	opened, err := h.Store.OpenEventCards(ctx, user.ID, name(user), n, ev, Today(now), now, questOf(QuestEvent, now))
	var few *storage.CardTokensError
	switch {
	case errors.Is(err, storage.ErrCardEventOver):
		return map[string]any{"refused": "over"}, failure{}
	case errors.As(err, &few):
		return map[string]any{"refused": "funds", "short": few.Need - few.Tokens, "tokens": few.Tokens}, failure{}
	case err != nil:
		h.Logger.Error("could not open event cards", "user_id", user.ID, "count", n, "error", err)
		return nil, errUnavailable
	}
	drawn := make([]map[string]any, len(opened.Draws))
	for i, d := range opened.Draws {
		drawn[i] = map[string]any{"card": h.held(d.Card, d.Count), "new": d.New, "sure": d.Guaranteed, "serial": d.Serial}
	}
	return map[string]any{
		"drawn": drawn, "points": opened.Points, "tokens": opened.Tokens, "shards": opened.Shards, "given": opened.Given,
		"pity": opened.Pulls, "pity_max": EventPity,
	}, failure{}
}
