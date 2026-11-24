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
// one, of those the player lacks if any. Mythic, when not 0, is the
// event's odds of a Mythic in a thousand, taken from the Commons. A Once
// event's card is limited: a player holds it once at most. Key names the
// event to the app and its pity count. An AtRarity event's cards come at
// the rarity rolled, Share of the pulls of a rarity it has cards of, not
// only as Mythics; Sure, when given, are the cards its pity counts and
// gives, else Cards. Shown, when set, is when the shop begins to show it as
// coming, else eventShownBefore its start.
type Event struct {
	Key          string
	Cards        []int64
	Once         bool
	AtRarity     bool
	Sure         []int64
	Mythic       int
	Share        float64
	Pity         int
	Price, Batch int
	Starts, Ends time.Time
	Shown        time.Time
}

// Events are the events, in the order the shop shows them.
//
// The Firefly Wedding event, as the owners asked (started again by them):
// Satoko Kirigaya, the limited card, 0.4% a pull, under the Mythic's 0.5%;
// the 200th pull in a row without her gives her. It ends as Monday
// 2026-11-23 begins in Tashkent, as the owners asked on 2026-11-21, not on
// 2026-12-02.
//
// The Black Clover event, as the owners asked on 2026-11-22: for a week from
// Wednesday 2026-11-25 19:30 in Tashkent, its last three days beside Demon
// Slayer's, shown as coming from 2026-11-22 (not to be opened before it
// starts), set out as that one is, its six Mythics, Asta, Yuno,
// Julius, Mereoleona, Yami and Noelle, which nothing else gives
// (storage.BlackCloverCards), 0.6% a pull beside the usual 0.5% of other
// Mythics, and a sure one on the 300th pull in a row without one.
//
// The Demon Slayer event, as the owners asked on 2026-11-21: for a week its
// six Mythics, which nothing else gives (storage.DemonSlayerCards), 0.6% a
// pull beside the usual 0.5% of other Mythics (as the owners asked the same
// day: a Mythic is 11 in a thousand, 6 of them the event's), and a sure one
// only on the 300th pull in a row without one.
var Events = []Event{
	{Key: "firefly", Cards: []int64{298761}, Once: true, Share: 0.8, Pity: 200, Price: 1, Batch: 10,
		Starts: time.Date(2026, 11, 18, 22, 0, 0, 0, Location), Ends: time.Date(2026, 11, 23, 0, 0, 0, 0, Location)},
	{Key: "demon-slayer", Cards: storage.DemonSlayerCards, Mythic: 11, Share: 6.0 / 11, Pity: 300, Price: 1, Batch: 10,
		Starts: time.Date(2026, 11, 21, 19, 30, 0, 0, Location), Ends: time.Date(2026, 11, 28, 19, 30, 0, 0, Location)},
	{Key: "black-clover", Cards: storage.BlackCloverCards, Mythic: 11, Share: 6.0 / 11, Pity: 300, Price: 1, Batch: 10,
		Starts: time.Date(2026, 11, 25, 19, 30, 0, 0, Location), Ends: time.Date(2026, 12, 2, 19, 30, 0, 0, Location),
		Shown: time.Date(2026, 11, 22, 0, 0, 0, 0, Location)},
}

// sure are the cards the event's pity counts and gives.
func (e Event) sure() []int64 {
	if len(e.Sure) > 0 {
		return e.Sure
	}
	return e.Cards
}

// shownFrom is when the shop begins to show the event.
func (e Event) shownFrom() time.Time {
	if !e.Shown.IsZero() {
		return e.Shown
	}
	return e.Starts.Add(-eventShownBefore)
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

// roll draws a pull's rarity of the event from a number in [0, 1000): its
// own odds of a Mythic first, if it has them, the rest as usual but for the
// Commons they came from.
func (e Event) roll(n int) Rarity {
	if e.Mythic == 0 {
		return EventRoll(n)
	}
	if n < e.Mythic {
		return Mythic
	}
	return Roll(n - e.Mythic + rules[Mythic].odds)
}

// EventOdds are an event pull's odds of each rarity in a thousand at the
// usual odds, the most common first.
func EventOdds() []int { return Event{}.odds() }

// odds are a pull's odds of each rarity of the event in a thousand.
func (e Event) odds() []int {
	odds := make([]int, len(Rarities))
	for n := range 1000 {
		odds[e.roll(n)]++
	}
	return odds
}

// eventRoll draws a pull's rarity of the event.
func (h *handler) eventRoll(e Event) func() Rarity {
	return func() Rarity {
		if h.EventRoll != nil {
			return h.EventRoll()
		}
		return e.roll(rand.IntN(1000))
	}
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

// eventShownBefore is how long before it starts the shop shows an event
// as coming: a week ahead it would stand over the one running.
const eventShownBefore = 24 * time.Hour

// events are the events the shop shows, those running or starting within
// eventShownBefore, each as eventView gives it.
func (h *handler) events(ctx context.Context, user miniapp.User) (any, failure) {
	now := h.Now()
	views := []any{}
	for _, e := range Events {
		if !now.Before(e.Ends) || now.Before(e.shownFrom()) {
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
	// Only the event's own Mythic cards are counted, as when the whole
	// collection was read for them.
	held := map[int64]int{}
	ids := make([]int64, 0, len(cards))
	for _, c := range cards {
		if c.Rarity == int(Mythic) {
			ids = append(ids, c.ID)
		}
	}
	if counts, err := h.Store.OwnedCounts(ctx, user.ID, ids); err == nil {
		held = counts
	}
	views := make([]cardView, len(cards))
	owned := false
	odds := e.odds()
	// eventOdds and sureOdds are a pull's odds in a thousand of one of the
	// event's cards and of one of its sure ones, for an AtRarity event.
	ofRarity := map[int]int{}
	for _, c := range cards {
		ofRarity[c.Rarity]++
	}
	eventOdds, sureOdds := 0.0, 0.0
	for r := range ofRarity {
		eventOdds += float64(odds[r]) * e.Share
	}
	for i, c := range cards {
		views[i] = h.held(c, held[c.ID])
		owned = owned || held[c.ID] > 0
		if slices.Contains(e.sure(), c.ID) {
			sureOdds += float64(odds[c.Rarity]) * e.Share / float64(ofRarity[c.Rarity])
		}
	}
	return map[string]any{
		"key": e.Key, "once": e.Once, "owned": e.Once && owned,
		"at_rarity": e.AtRarity, "event_odds": eventOdds, "sure_odds": sureOdds,
		"active": e.runs(now), "soon": now.Before(e.Starts),
		"starts_in": max(0, e.Starts.Sub(now).Milliseconds()),
		"ends_at":   e.Ends.UnixMilli(), "ends_in": max(0, e.Ends.Sub(now).Milliseconds()),
		"pity": pulls, "pity_max": e.Pity, "odds": e.odds(), "share": e.Share,
		"price": e.Price, "batch": e.Batch, "big_batch": BigBatch, "cards": views,
	}, failure{}
}

// eventOpen pulls 1, the event's batch or BigBatch of its cards for tokens. A pull
// refused says why: "soon" before it starts, "over" once it has ended or
// with no event, "funds" with too few tokens.
func (h *handler) eventOpen(ctx context.Context, user miniapp.User, key string, n int) (any, failure) {
	now := h.Now()
	e, ok := eventNamed(key, now)
	if !ok {
		return map[string]any{"refused": "over"}, failure{}
	}
	if n != 1 && n != e.Batch && n != BigBatch {
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
		Key: e.Key, Cards: e.Cards, Once: e.Once, AtRarity: e.AtRarity, Sure: e.Sure, Ends: e.Ends, Pity: e.Pity, Price: e.Price,
		Roll: ByOdds(h.eventRoll(e)), Pick: h.eventPick(e.Share),
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
			"event": slices.Contains(e.Cards, d.Card.ID), "prize": slices.Contains(e.sure(), d.Card.ID)}
	}
	return map[string]any{
		"key": e.Key, "drawn": drawn, "points": opened.Points, "tokens": opened.Tokens, "shards": opened.Shards, "given": opened.Given,
		"pity": opened.Pulls, "pity_max": e.Pity,
	}, failure{}
}
