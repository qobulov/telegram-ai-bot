package cards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

var eventCard = storage.Card{ID: EventCard, Name: "Satoko Kirigaya", Anime: "Firefly Wedding", ImageURL: "/kartalar/art/298761c.jpg", Rarity: 4, Value: 4000}

// fakeStore has no event; eventStore keeps one, with the player's pulls
// since Satoko.
func (f *fakeStore) OpenEventCards(context.Context, int64, string, int, storage.CardEvent, storage.CardDay, time.Time) (storage.EventOpening, error) {
	return storage.EventOpening{}, storage.ErrCardEventOver
}
func (f *fakeStore) CardEventPulls(context.Context, int64, int64) (int, storage.Card, error) {
	return 0, eventCard, nil
}

type eventStore struct {
	*fakeStore
	eventPulls int
}

func (f *eventStore) OpenEventCards(_ context.Context, _ int64, _ string, n int, ev storage.CardEvent, _ storage.CardDay, now time.Time) (storage.EventOpening, error) {
	if !now.Before(ev.Ends) {
		return storage.EventOpening{}, storage.ErrCardEventOver
	}
	if n*ev.Price > f.tokens {
		return storage.EventOpening{}, &storage.CardTokensError{Tokens: f.tokens, Need: n * ev.Price}
	}
	f.tokens -= n * ev.Price
	open := storage.EventOpening{CardOpening: storage.CardOpening{Tokens: f.tokens, Points: 15000}}
	for range n {
		card := lockedCard
		if rarity, _ := ev.Roll(0, 0); rarity == int(Mythic) && ev.Pick() {
			card = eventCard
		}
		open.Draws = append(open.Draws, storage.CardDraw{Card: card, Count: 1, New: true, Serial: 7})
		f.eventPulls++
		if card.ID == ev.Card {
			f.eventPulls = 0
		}
	}
	open.Pulls = f.eventPulls
	return open, nil
}

func (f *eventStore) CardEventPulls(context.Context, int64, int64) (int, storage.Card, error) {
	return f.eventPulls, eventCard, nil
}

// An event pull's odds are the usual ones with the Mythic twice as
// likely, the extra taken from Common.
func TestEventOdds(t *testing.T) {
	if got := EventOdds(); !slices.Equal(got, []int{625, 250, 90, 25, 10}) {
		t.Errorf("event odds = %v", got)
	}
	if EventRoll(9) != Mythic || EventRoll(10) != Legendary || EventRoll(999) != Common {
		t.Error("the event roll's edges moved")
	}
}

// The shop's event block reads the event and the player's pity; pulls cost
// a token each, the forced Mythic pick gives Satoko, too few tokens and the
// event's end refuse.
func TestTheCardEvent(t *testing.T) {
	now := EventEnds.Add(-50 * time.Hour)
	store := &eventStore{fakeStore: &fakeStore{day: PeriodStart(now), tokens: 5}}
	pick := true
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now },
		EventRoll: func() Rarity { return Mythic }, EventPick: func() bool { return pick }})
	initData := signedInit(7, now)
	pull := func(n int) map[string]any {
		body, _ := json.Marshal(map[string]any{"init_data": signedInit(7, now), "action": "event_open", "count": n})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		out["code"] = float64(rec.Code)
		return out
	}

	store.eventPulls = 37
	code, out := post(t, h, initData, "event")
	card, _ := out["card"].(map[string]any)
	if code != 200 || out["active"] != true || out["pity"] != 37.0 || out["pity_max"] != float64(EventPity) || out["ends_in"] != float64(50*time.Hour/time.Millisecond) || card["name"] != "Satoko Kirigaya" || card["limited"] != true {
		t.Fatalf("event = %d %v", code, out)
	}
	if odds := out["odds"].([]any); len(odds) != 5 || odds[4] != 10.0 {
		t.Errorf("odds = %v", odds)
	}

	if out := pull(1); out["tokens"] != 4.0 || out["pity"] != 0.0 || out["drawn"].([]any)[0].(map[string]any)["card"].(map[string]any)["name"] != "Satoko Kirigaya" || out["drawn"].([]any)[0].(map[string]any)["serial"] != 7.0 {
		t.Fatalf("one pull = %v", out)
	}
	pick = false
	if out := pull(EventBatch); out["refused"] != "funds" || out["short"] != 6.0 || out["drawn"] != nil {
		t.Fatalf("ten pulls with four tokens = %v", out)
	}
	store.tokens = 10
	if out := pull(EventBatch); out["tokens"] != 0.0 || len(out["drawn"].([]any)) != EventBatch || out["pity"] != 10.0 {
		t.Fatalf("ten pulls = %v", out)
	}
	if out := pull(3); out["code"] != float64(http.StatusBadRequest) {
		t.Errorf("three pulls = %v", out)
	}

	now = EventEnds
	if out := pull(1); out["refused"] != "over" {
		t.Errorf("a pull after the end = %v", out)
	}
	if _, out := post(t, h, signedInit(7, now), "event"); out["active"] != false || out["ends_in"] != 0.0 {
		t.Errorf("the event after its end = %v", out)
	}
}

// Before it starts the event is shown as coming, with the time to its
// start, and a pull is refused as too soon.
func TestTheCardEventBeforeItStarts(t *testing.T) {
	now := EventStarts.Add(-90 * time.Minute)
	store := &eventStore{fakeStore: &fakeStore{day: PeriodStart(now), tokens: 5}}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	code, out := post(t, h, signedInit(7, now), "event")
	if code != 200 || out["active"] != false || out["soon"] != true || out["starts_in"] != float64(90*time.Minute/time.Millisecond) {
		t.Fatalf("event before its start = %d %v", code, out)
	}
	body := `{"init_data":` + strconv.Quote(signedInit(7, now)) + `,"action":"event_open","count":1}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(body)))
	if !strings.Contains(rec.Body.String(), `"refused":"soon"`) {
		t.Errorf("a pull before the start = %d %s", rec.Code, rec.Body.String())
	}
}
