package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// quest is a quest of arenaDay, Tashkent's 18 November.
func quest(id string, target int) CardQuest {
	return CardQuest{ID: id, Day: arenaDay, Target: target}
}

func mustQuests(t *testing.T, store *Store, userID int64, day time.Time) map[string]QuestProgress {
	t.Helper()
	quests, err := store.CardQuests(context.Background(), userID, day)
	if err != nil {
		t.Fatalf("CardQuests(%d): %v", userID, err)
	}
	return quests
}

func setWallet(t *testing.T, store *Store, userID int64, tokens, shards int) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `UPDATE card_players SET tokens = $2, shards = $3 WHERE user_id = $1`, userID, tokens, shards); err != nil {
		t.Fatal(err)
	}
}

// Each action counts toward its quest in its own transaction: cards opened
// with tokens or in a pack, event pulls, a star raised, an arena win as the
// attacker and a market trade, for both its players; progress stops at the
// target, and an action refused counts nothing.
func TestQuestsCountWithTheirActions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	setWallet(t, store, 1, 10, 100)
	open := quest("open", 5)

	if _, err := store.OpenCards(ctx, 1, "Alice", 1, rarity(3), 15, CardDay{}, arenaNow, open); err != nil {
		t.Fatal(err)
	}
	if got := mustQuests(t, store, 1, arenaDay)["open"]; got != (QuestProgress{Progress: 1}) {
		t.Errorf("after a card = %+v", got)
	}
	if _, err := store.OpenCardPack(ctx, 1, "Alice", 2, rarity(3), 10, arenaNow, open); err != nil {
		t.Fatal(err)
	}
	if got := mustQuests(t, store, 1, arenaDay)["open"]; got.Progress != 3 {
		t.Errorf("after a pack of two = %+v", got)
	}
	if _, err := store.OpenCards(ctx, 1, "Alice", 5, rarity(3), 15, CardDay{}, arenaNow, open); err != nil {
		t.Fatal(err)
	}
	if got := mustQuests(t, store, 1, arenaDay)["open"]; got.Progress != 5 {
		t.Errorf("after five more = %+v, want the target, 5", got)
	}
	// Bob has neither tokens nor crystals: nothing opens, nothing counts.
	var few *CardFundsError
	if _, err := store.OpenCards(ctx, 2, "Bob", 5, rarity(3), 15, CardDay{}, arenaNow, open); !errors.As(err, &few) {
		t.Fatalf("Bob's opening = %v", err)
	}
	if got, ok := mustQuests(t, store, 2, arenaDay)["open"]; ok {
		t.Errorf("a refused opening counted %+v", got)
	}

	ev := CardEvent{Key: "test", Cards: []int64{20}, Ends: arenaNow.Add(time.Hour), Pity: 1000, Price: 1, Roll: rarity(3), Pick: func() bool { return false }}
	if _, err := store.OpenEventCards(ctx, 1, "Alice", 2, ev, CardDay{}, arenaNow, quest("event", 5)); err != nil {
		t.Fatal(err)
	}
	if got := mustQuests(t, store, 1, arenaDay)["event"]; got.Progress != 2 {
		t.Errorf("after two event pulls = %+v", got)
	}
	if _, err := store.OpenEventCards(ctx, 1, "Alice", 1, ev, CardDay{}, ev.Ends, quest("event", 5)); !errors.Is(err, ErrCardEventOver) {
		t.Fatalf("a pull after the end = %v", err)
	}
	if got := mustQuests(t, store, 1, arenaDay)["event"]; got.Progress != 2 {
		t.Errorf("a pull refused counted: %+v", got)
	}

	// Bob draws nothing, so his card 11 stays at one copy.
	if _, err := store.FuseCard(ctx, 2, 11, fuseCost, quest("fuse", 1)); !errors.Is(err, ErrCardFewCopies) {
		t.Fatalf("a fusion with one copy = %v", err)
	}
	if got, ok := mustQuests(t, store, 2, arenaDay)["fuse"]; ok {
		t.Errorf("a refused fusion counted %+v", got)
	}
	if _, err := store.FuseCard(ctx, 2, 20, fuseCost, quest("fuse", 1)); err != nil {
		t.Fatal(err)
	}
	if got := mustQuests(t, store, 2, arenaDay)["fuse"]; got.Progress != 1 {
		t.Errorf("Bob after a fusion = %+v", got)
	}

	arena := quest("arena", 1)
	if _, err := store.RecordArenaBattle(ctx, 1, 2, 1, false, arenaRules, arena); err != nil {
		t.Fatal(err)
	}
	if got, ok := mustQuests(t, store, 1, arenaDay)["arena"]; ok {
		t.Errorf("a lost attack counted %+v", got)
	}
	if _, err := store.RecordArenaBattle(ctx, 1, 2, 2, true, arenaRules, arena); err != nil {
		t.Fatal(err)
	}
	if got := mustQuests(t, store, 1, arenaDay)["arena"]; got.Progress != 1 {
		t.Errorf("after a won attack = %+v", got)
	}
	if _, err := store.RecordArenaBattle(ctx, 2, 1, 3, true, arenaRules, arena); err != nil {
		t.Fatal(err)
	}
	if got := mustQuests(t, store, 1, arenaDay)["arena"]; got.Progress != 1 {
		t.Errorf("a defence counted for Alice: %+v", got)
	}

	market := CardMarketRules{Open: 5, Keep: 7 * 24 * time.Hour, Now: arenaNow}
	offer, err := store.OfferCard(ctx, 1, 2, 12, market)
	if err != nil {
		t.Fatal(err)
	}
	trade := quest("market", 1)
	if _, err := store.TakeCardOffer(ctx, offer, 1, CardTradeRules{Now: arenaNow}, market.Keep, trade); !errors.Is(err, ErrCardOfferOwn) {
		t.Fatalf("taking one's own offer = %v", err)
	}
	if got, ok := mustQuests(t, store, 1, arenaDay)["market"]; ok {
		t.Errorf("a refused trade counted %+v", got)
	}
	if _, err := store.TakeCardOffer(ctx, offer, 2, CardTradeRules{Now: arenaNow}, market.Keep, trade); err != nil {
		t.Fatal(err)
	}
	if a, b := mustQuests(t, store, 1, arenaDay)["market"], mustQuests(t, store, 2, arenaDay)["market"]; a.Progress != 1 || b.Progress != 1 {
		t.Errorf("after a trade: seller %+v, buyer %+v", a, b)
	}

	// The zero quest counts nothing.
	if _, err := store.OpenCards(ctx, 1, "Alice", 1, rarity(3), 15, CardDay{}, arenaNow, CardQuest{}); err != nil {
		t.Fatal(err)
	}
	if got := len(mustQuests(t, store, 1, arenaDay)); got != 4 {
		t.Errorf("Alice has %d quests, want open, event, arena and market", got)
	}
}

// A quest counts on its own Tashkent date: the next day starts from nothing,
// and the day before's progress is not claimed on it.
func TestQuestsStartAgainEachDay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	setWallet(t, store, 1, 10, 0)
	if _, err := store.OpenCards(ctx, 1, "Alice", 5, rarity(3), 15, CardDay{}, arenaNow, quest("open", 5)); err != nil {
		t.Fatal(err)
	}
	next := CardQuest{ID: "open", Day: arenaDay.AddDate(0, 0, 1), Target: 5}
	if got := mustQuests(t, store, 1, next.Day); len(got) != 0 {
		t.Errorf("the next day's quests = %+v", got)
	}
	if err := store.ClaimCardQuest(ctx, 1, next, 2, arenaNow); !errors.Is(err, ErrQuestNotDone) {
		t.Errorf("claiming yesterday's quest today = %v", err)
	}
	// The same instant in UTC is still the 18th in Tashkent.
	if got := mustQuests(t, store, 1, arenaDay)["open"]; got.Progress != 5 {
		t.Errorf("the day's quest = %+v", got)
	}
	if _, err := store.OpenCards(ctx, 1, "Alice", 2, rarity(3), 15, CardDay{}, arenaNow, next); err != nil {
		t.Fatal(err)
	}
	if got := mustQuests(t, store, 1, next.Day)["open"]; got.Progress != 2 {
		t.Errorf("the next day after two cards = %+v", got)
	}
}

// A quest done pays its tokens once; one short of its target, never
// counted, or claimed already is refused and pays nothing.
func TestQuestClaimedOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	setWallet(t, store, 1, 4, 0)
	open := quest("open", 5)
	if err := store.ClaimCardQuest(ctx, 1, quest("arena", 1), 3, arenaNow); !errors.Is(err, ErrQuestNotDone) {
		t.Errorf("claiming a quest never counted = %v", err)
	}
	if _, err := store.OpenCards(ctx, 1, "Alice", 4, rarity(3), 15, CardDay{}, arenaNow, open); err != nil {
		t.Fatal(err)
	}
	if err := store.ClaimCardQuest(ctx, 1, open, 2, arenaNow); !errors.Is(err, ErrQuestNotDone) {
		t.Errorf("claiming at 4/5 = %v", err)
	}
	setWallet(t, store, 1, 1, 0)
	if _, err := store.OpenCards(ctx, 1, "Alice", 1, rarity(3), 15, CardDay{}, arenaNow, open); err != nil {
		t.Fatal(err)
	}
	if err := store.ClaimCardQuest(ctx, 1, open, 2, arenaNow); err != nil {
		t.Fatalf("claiming at 5/5 = %v", err)
	}
	if err := store.ClaimCardQuest(ctx, 1, open, 2, arenaNow); !errors.Is(err, ErrQuestClaimed) {
		t.Errorf("claiming again = %v", err)
	}
	if got := cardTokens(t, store, 1); got != 2 {
		t.Errorf("tokens after = %d, want 2", got)
	}
	if got := mustQuests(t, store, 1, arenaDay)["open"]; got != (QuestProgress{Progress: 5, Claimed: true}) {
		t.Errorf("the quest after = %+v", got)
	}
}

// Many claims of one quest at once pay it once: the others are told it was
// claimed.
func TestQuestClaimsAreCountedUnderALock(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	arena := quest("arena", 1)
	if _, err := store.RecordArenaBattle(ctx, 1, 2, 1, true, arenaRules, arena); err != nil {
		t.Fatal(err)
	}
	before := cardTokens(t, store, 1)

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.ClaimCardQuest(ctx, 1, arena, 3, arenaNow)
		}()
	}
	wg.Wait()
	close(errs)
	paid, refused := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			paid++
		case errors.Is(err, ErrQuestClaimed):
			refused++
		default:
			t.Errorf("ClaimCardQuest: %v", err)
		}
	}
	if paid != 1 || refused != 19 {
		t.Errorf("%d claims paid, %d refused; want 1 and 19", paid, refused)
	}
	if got := cardTokens(t, store, 1); got != before+3 {
		t.Errorf("tokens = %d, want %d", got, before+3)
	}
}
