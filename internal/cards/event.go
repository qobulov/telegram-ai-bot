package cards

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"time"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// Event is a time when the shop sells pulls of cards for tokens, at the
// usual odds: Share of the Mythics drawn are the event's Cards, the rest
// other Mythics, and the Pity-th pull in a row without one of Cards gives
// one, of those the player lacks if any. A Once event's card is limited: a
// player holds it once at most. Key names the event to the app and its
// pity count.
type Event struct {
	Key          string
	Cards        []int64
	Once         bool
	Share        float64
	Pity         int
	Price, Batch int
	Starts, Ends time.Time
}

// Events are the events, in the order the shop shows them.
//
// The Firefly Wedding event, as the owners asked (started again by them):
// Satoko Kirigaya, the limited card, 0.4% a pull, under the Mythic's 0.5%;
// the 200th pull in a row without her gives her.
//
// The Demon Slayer event, as the owners asked on 2026-11-21: for a week its
// six Mythics, which nothing else gives (storage.DemonSlayerCards), 0.4% a
// pull, and a sure one only on the 300th pull in a row without one.
var Events = []Event{
	{Key: "firefly", Cards: []int64{298761}, Once: true, Share: 0.8, Pity: 200, Price: 1, Batch: 10,
		Starts: time.Date(2026, 11, 18, 22, 0, 0, 0, Location), Ends: time.Date(2026, 12, 2, 0, 0, 0, 0, Location)},
	{Key: "demon-slayer", Cards: storage.DemonSlayerCards, Share: 0.8, Pity: 300, Price: 1, Batch: 10,
		Starts: time.Date(2026, 11, 21, 19, 30, 0, 0, Location), Ends: time.Date(2026, 11, 28, 19, 30, 0, 0, Location)},
}

// runs is whether the event is open at now.
func (e Event) runs(now time.Time) bool { return !now.Before(e.Starts) && now.Before(e.Ends) }

// eventRuns is whether any event is open at now.
func eventRuns(now time.Time) bool {
	return slices.ContainsFunc(Events, func(e Event) bool { return e.runs(now) })
}

// eventNamed is the event key names; "" names the first that runs at now,
// as an app from before there were two asks.
func eventNamed(key string, now time.Time) (Event, bool) {
	for _, e := range Events {
		if e.Key == key || key == "" && e.runs(now) {
			return e, true
		}
	}
	return Event{}, false
}

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

// eventPick says whether an event pull's Mythic is one of the event's
// cards, share of the time.
func (h *handler) eventPick(share float64) func() bool {
	return func() bool {
		if h.EventPick != nil {
			return h.EventPick()
		}
		return rand.Float64() < share
	}
}

// events are the events the shop shows, those running or coming, each as
// eventView gives it.
func (h *handler) events(ctx context.Context, user miniapp.User) (any, failure) {
	now := h.Now()
	views := []any{}
	for _, e := range Events {
		if !now.Before(e.Ends) {
			continue
		}
		view, fail := h.eventView(ctx, user, e)
		if fail.status != 0 {
			return nil, fail
		}
		views = append(views, view)
	}
	return map[string]any{"events": views}, failure{}
}

// eventView is an event as the shop shows it: its cards with how many the
// player holds, and the player's pity.
func (h *handler) eventView(ctx context.Context, user miniapp.User, e Event) (any, failure) {
	now := h.Now()
	pulls, cards, err := h.Store.CardEventPulls(ctx, user.ID, e.Key, e.Cards)
	if err != nil {
		h.Logger.Error("could not read the card event", "user_id", user.ID, "event", e.Key, "error", err)
		return nil, errUnavailable
	}
	held := map[int64]int{}
	if owned, err := h.Store.OwnedCards(ctx, user.ID, int(Mythic)); err == nil {
		for _, c := range owned {
			held[c.ID] = c.Count
		}
	}
	views := make([]cardView, len(cards))
	owned := false
	for i, c := range cards {
		views[i] = h.held(c, held[c.ID])
		owned = owned || held[c.ID] > 0
	}
	return map[string]any{
		"key": e.Key, "once": e.Once, "owned": e.Once && owned,
		"active": e.runs(now), "soon": now.Before(e.Starts),
		"starts_in": max(0, e.Starts.Sub(now).Milliseconds()),
		"ends_at":   e.Ends.UnixMilli(), "ends_in": max(0, e.Ends.Sub(now).Milliseconds()),
		"pity": pulls, "pity_max": e.Pity, "odds": EventOdds(), "share": e.Share,
		"price": e.Price, "batch": e.Batch, "cards": views,
	}, failure{}
}

// eventOpen pulls 1 or the event's batch of its cards for tokens. A pull
// refused says why: "soon" before it starts, "over" once it has ended or
// with no event, "funds" with too few tokens.
func (h *handler) eventOpen(ctx context.Context, user miniapp.User, key string, n int) (any, failure) {
	now := h.Now()
	e, ok := eventNamed(key, now)
	if !ok {
		return map[string]any{"refused": "over"}, failure{}
	}
	if n != 1 && n != e.Batch {
		return nil, errBadRequest
	}
	if now.Before(e.Starts) {
		return map[string]any{"refused": "soon"}, failure{}
	}
	if !now.Before(e.Ends) {
		return map[string]any{"refused": "over"}, failure{}
	}
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	ev := storage.CardEvent{
		Key: e.Key, Cards: e.Cards, Once: e.Once, Ends: e.Ends, Pity: e.Pity, Price: e.Price,
		Roll: ByOdds(h.eventRoll), Pick: h.eventPick(e.Share),
	}
	opened, err := h.Store.OpenEventCards(ctx, user.ID, name(user), n, ev, Today(now), now, questOf(QuestEvent, now))
	var few *storage.CardTokensError
	switch {
	case errors.Is(err, storage.ErrCardEventOver):
		return map[string]any{"refused": "over"}, failure{}
	case errors.As(err, &few):
		return map[string]any{"refused": "funds", "short": few.Need - few.Tokens, "tokens": few.Tokens}, failure{}
	case err != nil:
		h.Logger.Error("could not open event cards", "user_id", user.ID, "event", e.Key, "count", n, "error", err)
		return nil, errUnavailable
	}
	drawn := make([]map[string]any, len(opened.Draws))
	for i, d := range opened.Draws {
		drawn[i] = map[string]any{"card": h.held(d.Card, d.Count), "new": d.New, "sure": d.Guaranteed, "serial": d.Serial,
			"event": slices.Contains(e.Cards, d.Card.ID)}
	}
	return map[string]any{
		"key": e.Key, "drawn": drawn, "points": opened.Points, "tokens": opened.Tokens, "shards": opened.Shards, "given": opened.Given,
		"pity": opened.Pulls, "pity_max": e.Pity,
	}, failure{}
}
