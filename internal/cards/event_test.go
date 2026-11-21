package cards

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// firefly is the Firefly Wedding event, of the limited Satoko.
var firefly = Events[0]

var eventCard = storage.Card{ID: firefly.Cards[0], Name: "Satoko Kirigaya", Anime: "Firefly Wedding", ImageURL: "/kartalar/art/298761c.jpg", Rarity: 4, Value: 4000}

// fakeStore has no event; eventStore keeps one, with the player's pulls
// since Satoko.
func (f *fakeStore) OpenEventCards(context.Context, int64, string, int, storage.CardEvent, storage.CardDay, time.Time, storage.CardQuest) (storage.EventOpening, error) {
	return storage.EventOpening{}, storage.ErrCardEventOver
}
func (f *fakeStore) CardEventPulls(context.Context, int64, string, []int64) (int, []storage.Card, error) {
	return 0, []storage.Card{eventCard}, nil
}

type eventStore struct {
	*fakeStore
	eventPulls int
}

func (f *eventStore) OpenEventCards(_ context.Context, userID int64, _ string, n int, ev storage.CardEvent, _ storage.CardDay, now time.Time, quest storage.CardQuest) (storage.EventOpening, error) {
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
		if slices.Contains(ev.Cards, card.ID) {
			f.eventPulls = 0
		}
	}
	open.Pulls = f.eventPulls
	f.count(userID, quest, n)
	return open, nil
}

func (f *eventStore) CardEventPulls(context.Context, int64, string, []int64) (int, []storage.Card, error) {
	return f.eventPulls, []storage.Card{eventCard}, nil
}

// An event pull's odds are the usual ones.
func TestEventOdds(t *testing.T) {
	if got := EventOdds(); !slices.Equal(got, []int{630, 250, 90, 25, 5}) {
		t.Errorf("event odds = %v", got)
	}
	if EventRoll(4) != Mythic || EventRoll(5) != Legendary || EventRoll(999) != Common {
		t.Error("the event roll's edges moved")
	}
}

// The shop's event block reads the event and the player's pity; pulls cost
// a token each, the forced Mythic pick gives Satoko, too few tokens and the
// event's end refuse.
func TestTheCardEvent(t *testing.T) {
	now := firefly.Ends.Add(-50 * time.Hour)
	store := &eventStore{fakeStore: &fakeStore{day: PeriodStart(now), tokens: 5}}
	pick := true
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now },
		EventRoll: func() Rarity { return Mythic }, EventPick: func() bool { return pick }})
	initData := signedInit(7, now)
	pull := func(n int) map[string]any {
		body, _ := json.Marshal(map[string]any{"init_data": signedInit(7, now), "action": "event_open", "event": "firefly", "count": n})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		out["code"] = float64(rec.Code)
		return out
	}

	store.eventPulls = 37
	code, out := fireflyView(t, h, initData)
	card, _ := out["cards"].([]any)[0].(map[string]any)
	if code != 200 || out["active"] != true || out["pity"] != 37.0 || out["pity_max"] != float64(firefly.Pity) || out["ends_in"] != float64(50*time.Hour/time.Millisecond) || card["name"] != "Satoko Kirigaya" || card["limited"] != true {
		t.Fatalf("event = %d %v", code, out)
	}
	if odds := out["odds"].([]any); len(odds) != 5 || odds[4] != 5.0 || out["share"] != 0.8 || out["pity_max"] != 200.0 {
		t.Errorf("odds = %v", odds)
	}

	if out := pull(1); out["tokens"] != 4.0 || out["pity"] != 0.0 || out["drawn"].([]any)[0].(map[string]any)["card"].(map[string]any)["name"] != "Satoko Kirigaya" || out["drawn"].([]any)[0].(map[string]any)["serial"] != 7.0 {
		t.Fatalf("one pull = %v", out)
	}
	pick = false
	if out := pull(firefly.Batch); out["refused"] != "funds" || out["short"] != 6.0 || out["drawn"] != nil {
		t.Fatalf("ten pulls with four tokens = %v", out)
	}
	store.tokens = 10
	if out := pull(firefly.Batch); out["tokens"] != 0.0 || len(out["drawn"].([]any)) != firefly.Batch || out["pity"] != 10.0 {
		t.Fatalf("ten pulls = %v", out)
	}
	if out := pull(3); out["code"] != float64(http.StatusBadRequest) {
		t.Errorf("three pulls = %v", out)
	}

	now = firefly.Ends
	if out := pull(1); out["refused"] != "over" {
		t.Errorf("a pull after the end = %v", out)
	}
	if _, out := post(t, h, signedInit(7, now), "events"); len(out["events"].([]any)) != 0 {
		t.Errorf("the events after both ended = %v", out)
	}
}

// Before it starts the event is shown as coming, with the time to its
// start, and a pull is refused as too soon.
func TestTheCardEventBeforeItStarts(t *testing.T) {
	now := firefly.Starts.Add(-90 * time.Minute)
	store := &eventStore{fakeStore: &fakeStore{day: PeriodStart(now), tokens: 5}}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	code, out := fireflyView(t, h, signedInit(7, now))
	if code != 200 || out["active"] != false || out["soon"] != true || out["starts_in"] != float64(90*time.Minute/time.Millisecond) {
		t.Fatalf("event before its start = %d %v", code, out)
	}
	body := `{"init_data":` + strconv.Quote(signedInit(7, now)) + `,"action":"event_open","event":"firefly","count":1}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(body)))
	if !strings.Contains(rec.Body.String(), `"refused":"soon"`) {
		t.Errorf("a pull before the start = %d %s", rec.Code, rec.Body.String())
	}
}

// fireflyView is the Firefly event as the shop's list of events shows it.
func fireflyView(t *testing.T, h http.Handler, initData string) (int, map[string]any) {
	t.Helper()
	code, out := post(t, h, initData, "events")
	for _, e := range out["events"].([]any) {
		if view := e.(map[string]any); view["key"] == "firefly" {
			return code, view
		}
	}
	t.Fatalf("no Firefly event in %v", out)
	return 0, nil
}

// The Demon Slayer event runs a week from 2026-11-21 19:30 in Tashkent with
// its six cards, 0.6% a pull beside other Mythics' usual 0.5% (a Mythic 11 in
// a thousand, the Commons 624) and a sure one on the 300th; while both run the
// shop lists both, and a pull names its event. No anime's collection needs
// its cards.
func TestTheDemonSlayerEvent(t *testing.T) {
	ds := Events[1]
	if ds.Key != "demon-slayer" || ds.Pity != 300 || ds.Once || len(ds.Cards) != 6 || ds.Price != 1 ||
		ds.Ends.Sub(ds.Starts) != 7*24*time.Hour || !ds.Starts.Equal(time.Date(2026, 11, 21, 19, 30, 0, 0, Location)) {
		t.Fatalf("the Demon Slayer event = %+v", ds)
	}
	if got := ds.odds(); !slices.Equal(got, []int{624, 250, 90, 25, 11}) || ds.roll(10) != Mythic || ds.roll(11) != Legendary || ds.roll(999) != Common {
		t.Errorf("the Demon Slayer odds = %v", got)
	}
	if own := math.Round(float64(ds.odds()[Mythic]) * ds.Share); own != 6 {
		t.Errorf("the Demon Slayer cards are %v in a thousand, want 6", own)
	}
	if got := firefly.odds(); !slices.Equal(got, EventOdds()) {
		t.Errorf("the Firefly odds = %v, want the usual", got)
	}
	now := ds.Starts.Add(time.Hour)
	if !eventRuns(now) || !firefly.runs(now) {
		t.Fatal("both events should run")
	}
	if e, ok := eventNamed("demon-slayer", now); !ok || e.Key != "demon-slayer" {
		t.Errorf("eventNamed = %+v %v", e, ok)
	}
	if e, ok := eventNamed("", now); !ok || e.Key != "firefly" {
		t.Errorf("an unnamed pull = %+v %v, want the first running", e, ok)
	}
	store := &eventStore{fakeStore: &fakeStore{day: PeriodStart(now), tokens: 5}}
	var picked storage.CardEvent
	h := NewHandler(Options{BotToken: testToken, Store: &pickStore{eventStore: store, picked: &picked}, Now: func() time.Time { return now },
		EventRoll: func() Rarity { return Common }, EventPick: func() bool { return false }})
	if _, out := post(t, h, signedInit(7, now), "events"); len(out["events"].([]any)) != 2 {
		t.Fatalf("the shop's events = %v", out)
	}
	body := `{"init_data":` + strconv.Quote(signedInit(7, now)) + `,"action":"event_open","event":"demon-slayer","count":10}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(body)))
	if rec.Code != 200 || picked.Key != "demon-slayer" || picked.Pity != 300 || len(picked.Cards) != 6 || picked.Once {
		t.Fatalf("a Demon Slayer pull = %d %s, event %+v", rec.Code, rec.Body.String(), picked)
	}
	catalog := []storage.Card{{ID: ds.Cards[0], Anime: "Demon Slayer: Kimetsu no Yaiba"}, {ID: 126071, Anime: "Demon Slayer: Kimetsu no Yaiba"}}
	if got := AnimeCards(catalog, "Demon Slayer"); !slices.Equal(got, []int64{126071}) {
		t.Errorf("the Demon Slayer collection needs %v, want Tanjiro alone", got)
	}
}

// pickStore keeps the event of the last pull.
type pickStore struct {
	*eventStore
	picked *storage.CardEvent
}

func (f *pickStore) OpenEventCards(ctx context.Context, userID int64, name string, n int, ev storage.CardEvent, day storage.CardDay, now time.Time, quest storage.CardQuest) (storage.EventOpening, error) {
	*f.picked = ev
	return f.eventStore.OpenEventCards(ctx, userID, name, n, ev, day, now, quest)
}
