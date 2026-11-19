package cards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// fakeQuests keeps the quests of fakeStore: each player's progress by day
// and quest, as the store counts it.
type fakeQuests struct {
	progress map[string]storage.QuestProgress
}

func questKey(userID int64, day time.Time, id string) string {
	return fmt.Sprint(userID, "/", day.Format(time.DateOnly), "/", id)
}

func (f *fakeStore) count(userID int64, q storage.CardQuest, n int) {
	if q.ID == "" {
		return
	}
	if f.quests.progress == nil {
		f.quests.progress = map[string]storage.QuestProgress{}
	}
	key := questKey(userID, q.Day, q.ID)
	p := f.quests.progress[key]
	p.Progress = min(p.Progress+n, q.Target)
	f.quests.progress[key] = p
}

func (f *fakeStore) CardQuests(_ context.Context, userID int64, day time.Time) (map[string]storage.QuestProgress, error) {
	out := map[string]storage.QuestProgress{}
	for _, id := range []string{QuestOpen, QuestArena, QuestFuse, QuestEvent, QuestMarket} {
		if p, ok := f.quests.progress[questKey(userID, day, id)]; ok {
			out[id] = p
		}
	}
	return out, nil
}

func (f *fakeStore) ClaimCardQuest(_ context.Context, userID int64, q storage.CardQuest, reward int, _ time.Time) error {
	key := questKey(userID, q.Day, q.ID)
	p := f.quests.progress[key]
	switch {
	case p.Claimed:
		return storage.ErrQuestClaimed
	case p.Progress < q.Target:
		return storage.ErrQuestNotDone
	}
	p.Claimed = true
	f.quests.progress[key] = p
	f.tokens += reward
	return nil
}

// questCaller posts to a handler on store, its clock at *now.
func questCaller(store Store, now *time.Time) func(user int64, fields map[string]any) map[string]any {
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return *now },
		EventRoll: func() Rarity { return Common }, EventPick: func() bool { return false }})
	return func(user int64, fields map[string]any) map[string]any {
		fields["init_data"] = signedInit(user, *now)
		body, _ := json.Marshal(fields)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		out["code"] = float64(rec.Code)
		return out
	}
}

// progressOf is each quest's progress in a "quests" answer, by its ID.
func progressOf(t *testing.T, out map[string]any) map[string]float64 {
	t.Helper()
	list, ok := out["quests"].([]any)
	if !ok {
		t.Fatalf("no quests in %v", out)
	}
	got := map[string]float64{}
	for _, q := range list {
		q := q.(map[string]any)
		got[q["id"].(string)] = q["progress"].(float64)
	}
	return got
}

// weakCards are five cards no team survives, so that an attack on them is
// sure to win.
func weakCards() []storage.OwnedCard {
	cards := arenaCards()
	for i := range cards {
		cards[i].Attack, cards[i].Health = 1, 1
	}
	return cards
}

// The day's quests are counted by the actions themselves: cards opened by a
// draw and in a shop pack, an arena win as the attacker, a star raised and
// event pulls, a 5x pull counting 5; progress stops at the target.
func TestQuestsCountTheirActions(t *testing.T) {
	now := EventEnds.Add(-50 * time.Hour)
	store := &eventStore{fakeStore: &fakeStore{day: PeriodStart(now), tokens: 20, shards: 500,
		arena: fakeArena{held: map[int64][]storage.OwnedCard{7: arenaCards(), 8: weakCards()}}}}
	store.arena.held[7] = append(store.arena.held[7], storage.OwnedCard{Card: heldCard, Count: 2, Stars: 1})
	call := questCaller(store, &now)

	out := call(7, map[string]any{"action": "quests"})
	if out["code"] != 200.0 || out["total"] != 4.0 || out["done"] != 0.0 || out["ready"] != 0.0 {
		t.Fatalf("quests at first = %v", out)
	}
	list := out["quests"].([]any)
	want := []map[string]any{
		{"id": "open", "target": 5.0, "reward": 2.0},
		{"id": "arena", "target": 1.0, "reward": 3.0},
		{"id": "fuse", "target": 1.0, "reward": 2.0},
		{"id": "event", "target": 1.0, "reward": 3.0},
	}
	total := 0.0
	for i, w := range want {
		q := list[i].(map[string]any)
		if q["id"] != w["id"] || q["target"] != w["target"] || q["reward"] != w["reward"] || q["progress"] != 0.0 || q["done"] != false || q["claimed"] != false {
			t.Errorf("quest %d = %v, want %v", i, q, w)
		}
		total += q["reward"].(float64)
	}
	if total != 10 {
		t.Errorf("the quests give %v tokens, want 10", total)
	}

	call(7, map[string]any{"action": "open", "count": 1})
	if got := progressOf(t, call(7, map[string]any{"action": "quests"})); got["open"] != 1 {
		t.Errorf("after a card = %v", got)
	}
	call(7, map[string]any{"action": "buy", "item": "epic"})
	if got := progressOf(t, call(7, map[string]any{"action": "quests"})); got["open"] != 5 {
		t.Errorf("after a pack of five = %v, want the target", got)
	}
	call(7, map[string]any{"action": "buy", "item": "t5"})
	call(7, map[string]any{"action": "open", "count": Batch})
	if got := progressOf(t, call(7, map[string]any{"action": "quests"})); got["open"] != 5 {
		t.Errorf("past the target = %v", got)
	}

	call(7, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	call(8, map[string]any{"action": "arena_team", "front": arenaFront, "back": arenaBack})
	if out := call(7, map[string]any{"action": "arena_attack", "opponent": 8}); out["won"] != true {
		t.Fatalf("the sure win = %v", out)
	}
	if out := call(8, map[string]any{"action": "arena_attack", "opponent": 7}); out["won"] != false {
		t.Fatalf("the sure loss = %v", out)
	}
	if got := progressOf(t, call(8, map[string]any{"action": "quests"})); got["arena"] != 0 {
		t.Errorf("a lost attack counted: %v", got)
	}

	call(7, map[string]any{"action": "fuse", "card": heldCard.ID})
	call(7, map[string]any{"action": "event_open", "count": EventBatch})
	out = call(7, map[string]any{"action": "quests"})
	if got := progressOf(t, out); got["open"] != 5 || got["arena"] != 1 || got["fuse"] != 1 || got["event"] != 1 {
		t.Errorf("after every action = %v", got)
	}
	if out["done"] != 4.0 || out["ready"] != 4.0 {
		t.Errorf("done %v, ready %v; want 4 and 4", out["done"], out["ready"])
	}
	if s := call(7, map[string]any{"action": "state"})["quests"].(map[string]any); s["done"] != 4.0 || s["ready"] != 4.0 || s["total"] != 4.0 || s["reset_in"] != float64(DayStart(now).AddDate(0, 0, 1).Sub(now)/time.Millisecond) {
		t.Errorf("the state's quests = %v", s)
	}
}

// While the event runs the fourth quest is an event pull and a trade counts
// nothing; once it has ended it is a market trade, counted for both players,
// and pulls are refused.
func TestQuestsTurnToTheMarketWhenTheEventEnds(t *testing.T) {
	now := EventEnds.Add(-time.Minute)
	store := &eventStore{fakeStore: &fakeStore{day: PeriodStart(now), tokens: 5}}
	call := questCaller(store, &now)
	ids := func(user int64) []string {
		var out []string
		for _, q := range call(user, map[string]any{"action": "quests"})["quests"].([]any) {
			out = append(out, q.(map[string]any)["id"].(string))
		}
		return out
	}
	if got := strings.Join(ids(7), ","); got != "open,arena,fuse,event" {
		t.Errorf("quests while the event runs = %s", got)
	}
	call(8, map[string]any{"action": "offer", "card": heldCard.ID, "want": lockedCard.ID})
	call(7, map[string]any{"action": "take", "offer": 1})
	if got, ok := store.quests.progress[questKey(7, DayStart(now), QuestMarket)]; ok {
		t.Errorf("a trade while the event runs counted %+v", got)
	}
	if out := call(7, map[string]any{"action": "quest_claim", "quest": QuestMarket}); out["code"] != 400.0 {
		t.Errorf("claiming the market quest while the event runs = %v", out)
	}

	now = EventEnds
	if got := strings.Join(ids(7), ","); got != "open,arena,fuse,market" {
		t.Errorf("quests after the event = %s", got)
	}
	call(8, map[string]any{"action": "offer", "card": heldCard.ID, "want": lockedCard.ID})
	call(7, map[string]any{"action": "take", "offer": 1})
	if got := progressOf(t, call(7, map[string]any{"action": "quests"})); got["market"] != 1 {
		t.Errorf("the taker after a trade = %v", got)
	}
	if got := progressOf(t, call(8, map[string]any{"action": "quests"})); got["market"] != 1 {
		t.Errorf("the offerer after a trade = %v", got)
	}
	if out := call(7, map[string]any{"action": "event_open", "count": 1}); out["refused"] != "over" {
		t.Errorf("a pull after the event = %v", out)
	}
	if out := call(7, map[string]any{"action": "quest_claim", "quest": QuestEvent}); out["code"] != 400.0 {
		t.Errorf("claiming the event quest after it = %v", out)
	}
}

// The quests start again at midnight in Tashkent: the day before's progress
// is gone, and cannot be claimed; reset_in counts down to that midnight.
func TestQuestsStartAgainAtTashkentMidnight(t *testing.T) {
	// 23:59 in Tashkent is 18:59 UTC, as a server's clock reads it: a day
	// counted by the UTC date would not end.
	now := time.Date(2026, 12, 5, 18, 59, 0, 0, time.UTC)
	store := &fakeStore{day: PeriodStart(now), tokens: 5}
	call := questCaller(store, &now)
	call(7, map[string]any{"action": "open", "count": Batch})
	out := call(7, map[string]any{"action": "quests"})
	if got := progressOf(t, out); got["open"] != 5 || out["reset_in"] != float64(time.Minute/time.Millisecond) || out["ready"] != 1.0 {
		t.Fatalf("before midnight = %v", out)
	}

	now = now.Add(time.Minute)
	store.day = PeriodStart(now)
	out = call(7, map[string]any{"action": "quests"})
	if got := progressOf(t, out); got["open"] != 0 || out["reset_in"] != float64(24*time.Hour/time.Millisecond) || out["ready"] != 0.0 {
		t.Errorf("after midnight = %v", out)
	}
	if out := call(7, map[string]any{"action": "quest_claim", "quest": QuestOpen}); out["refused"] != "not_done" {
		t.Errorf("claiming yesterday's quest = %v", out)
	}
}

// A quest done is claimed once for its tokens; one not done or claimed
// already is refused with the reason, and an unknown quest is a bad request.
func TestQuestClaim(t *testing.T) {
	now := time.Date(2026, 12, 5, 12, 0, 0, 0, Location)
	store := &fakeStore{day: PeriodStart(now), tokens: 1}
	call := questCaller(store, &now)
	if out := call(7, map[string]any{"action": "quest_claim", "quest": QuestFuse}); out["refused"] != "not_done" {
		t.Errorf("claiming a quest not done = %v", out)
	}
	call(7, map[string]any{"action": "fuse", "card": heldCard.ID})
	out := call(7, map[string]any{"action": "quest_claim", "quest": QuestFuse})
	if out["claimed"] != float64(QuestFuseReward) || out["tokens"] != 3.0 || out["done"] != 1.0 || out["ready"] != 0.0 {
		t.Fatalf("claiming the fusion quest = %v", out)
	}
	if q := out["quests"].([]any)[2].(map[string]any); q["id"] != QuestFuse || q["claimed"] != true || q["done"] != true {
		t.Errorf("the fusion quest after = %v", q)
	}
	if out := call(7, map[string]any{"action": "quest_claim", "quest": QuestFuse}); out["refused"] != "claimed" || out["claimed"] != nil {
		t.Errorf("claiming it again = %v", out)
	}
	if store.tokens != 3 {
		t.Errorf("tokens = %d, want 3", store.tokens)
	}
	for _, id := range []string{"", "steal", QuestEvent} {
		if out := call(7, map[string]any{"action": "quest_claim", "quest": id}); out["code"] != 400.0 {
			t.Errorf("claiming %q = %v", id, out)
		}
	}
}
