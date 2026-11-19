package cards

import (
	"context"
	"errors"
	"time"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// Daily quests, as the owners asked: four a day, new at midnight in
// Tashkent, 10 tokens in all, each quest's tokens given when its player
// claims it. The fourth is an event pull while the event runs and a market
// trade, counted for both players, once it has ended.
const (
	QuestOpen   = "open"   // open cards, by a draw or a shop pack
	QuestArena  = "arena"  // win an arena battle as the attacker
	QuestFuse   = "fuse"   // raise a card's star
	QuestEvent  = "event"  // pull the event's cards
	QuestMarket = "market" // trade on the market

	QuestOpenTarget   = 5
	QuestOpenReward   = 2
	QuestArenaTarget  = 1
	QuestArenaReward  = 3
	QuestFuseTarget   = 1
	QuestFuseReward   = 2
	QuestEventTarget  = 1
	QuestEventReward  = 3
	QuestMarketTarget = 1
	QuestMarketReward = 3
)

// Quest is a daily quest: what counts toward it, the progress that
// completes it and the tokens it gives.
type Quest struct {
	ID             string
	Target, Reward int
}

// DayStart is the start of now's day in Tashkent: the day the arena's
// tokens and the quests count.
func DayStart(now time.Time) time.Time {
	local := now.In(Location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, Location)
}

// eventRuns is whether the event is open at now.
func eventRuns(now time.Time) bool {
	return !now.Before(EventStarts) && now.Before(EventEnds)
}

// Quests are the day's quests at now, in the order the app lists them.
func Quests(now time.Time) []Quest {
	last := Quest{QuestMarket, QuestMarketTarget, QuestMarketReward}
	if eventRuns(now) {
		last = Quest{QuestEvent, QuestEventTarget, QuestEventReward}
	}
	return []Quest{
		{QuestOpen, QuestOpenTarget, QuestOpenReward},
		{QuestArena, QuestArenaTarget, QuestArenaReward},
		{QuestFuse, QuestFuseTarget, QuestFuseReward},
		last,
	}
}

// todaysQuest is the quest id of the day at now; ok is false when it is not
// one of them.
func todaysQuest(id string, now time.Time) (q Quest, ok bool) {
	for _, q := range Quests(now) {
		if q.ID == id {
			return q, true
		}
	}
	return Quest{}, false
}

// questOf is what an action counts toward: the quest id of the day at now,
// or nothing when id is not one of the day's.
func questOf(id string, now time.Time) storage.CardQuest {
	q, ok := todaysQuest(id, now)
	if !ok {
		return storage.CardQuest{}
	}
	return storage.CardQuest{ID: q.ID, Day: DayStart(now), Target: q.Target}
}

// questView is a quest as the app lists it.
type questView struct {
	ID       string `json:"id"`
	Progress int    `json:"progress"`
	Target   int    `json:"target"`
	Reward   int    `json:"reward"`
	Done     bool   `json:"done"`
	Claimed  bool   `json:"claimed"`
}

// questList is the player's quests of the day at now, and how many are done
// and how many done but not claimed.
func (h *handler) questList(ctx context.Context, userID int64, now time.Time) (list []questView, done, ready int, err error) {
	progress, err := h.Store.CardQuests(ctx, userID, DayStart(now))
	if err != nil {
		return nil, 0, 0, err
	}
	for _, q := range Quests(now) {
		p := progress[q.ID]
		v := questView{ID: q.ID, Progress: min(p.Progress, q.Target), Target: q.Target, Reward: q.Reward, Claimed: p.Claimed}
		v.Done = v.Progress >= q.Target
		if v.Done {
			done++
		}
		if v.Done && !v.Claimed {
			ready++
		}
		list = append(list, v)
	}
	return list, done, ready, nil
}

// quests is the day's quests with the player's progress, and the time to
// the next midnight in Tashkent, when new ones come.
func (h *handler) quests(ctx context.Context, user miniapp.User) (any, failure) {
	now := h.Now()
	list, done, ready, err := h.questList(ctx, user.ID, now)
	if err != nil {
		h.Logger.Error("could not read card quests", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{
		"quests": list, "done": done, "ready": ready, "total": len(list),
		"reset_in": questsResetIn(now),
	}, failure{}
}

// questsResetIn is the time in milliseconds to the next midnight in
// Tashkent, when the next day's quests come.
func questsResetIn(now time.Time) int64 {
	return DayStart(now).AddDate(0, 0, 1).Sub(now).Milliseconds()
}

// claimQuest gives the player a quest's tokens, and the quests with
// "claimed", the tokens given, and "tokens", the player's now. A claim
// refused says why: "not_done" or "claimed".
func (h *handler) claimQuest(ctx context.Context, user miniapp.User, id string) (any, failure) {
	now := h.Now()
	q, ok := todaysQuest(id, now)
	if !ok {
		return nil, errBadRequest
	}
	err := h.Store.ClaimCardQuest(ctx, user.ID, questOf(id, now), q.Reward, now)
	switch {
	case errors.Is(err, storage.ErrQuestNotDone):
		return map[string]any{"refused": "not_done"}, failure{}
	case errors.Is(err, storage.ErrQuestClaimed):
		return map[string]any{"refused": "claimed"}, failure{}
	case err != nil:
		h.Logger.Error("could not claim a card quest", "user_id", user.ID, "quest", id, "error", err)
		return nil, errUnavailable
	}
	player, _, err := h.Store.CardPlayerOf(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read a card player", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	view, fail := h.quests(ctx, user)
	if fail != (failure{}) {
		return nil, fail
	}
	result := view.(map[string]any)
	result["claimed"] = q.Reward
	result["tokens"] = Tokens(player, now)
	return result, failure{}
}
