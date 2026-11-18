package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/storage"
)

// fakeCards keeps one card, draws it once an hour, and counts what it was
// asked.
type fakeCards struct {
	mu     sync.Mutex
	card   storage.Card
	last   map[int64]time.Time
	held   map[int64]int
	used   map[int64]int
	days   []storage.CardDay
	top    []storage.CardPlayer
	shards map[int64]int
	duels  map[int64]bool
	named  map[int64][]storage.Card
	trades []*fakeTrade
}

func newFakeCards(picture string) *fakeCards {
	return &fakeCards{
		card: storage.Card{ID: 1, Name: "Gojo *Satoru*", Anime: "Jujutsu Kaisen", ImageURL: picture, Rarity: 4, Attack: 5130, Health: 18020, Value: 5000},
		last: map[int64]time.Time{}, held: map[int64]int{}, used: map[int64]int{},
		shards: map[int64]int{}, duels: map[int64]bool{},
		named: map[int64][]storage.Card{},
	}
}

func (f *fakeCards) CardCatalog(context.Context) ([]storage.Card, error) { return nil, nil }
func (f *fakeCards) CardPlayerOf(_ context.Context, userID int64) (storage.CardPlayer, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return storage.CardPlayer{UserID: userID, Name: "Bek", Cards: f.held[userID]}, f.last[userID], nil
}

func (f *fakeCards) CardWallet(_ context.Context, userID int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shards[userID], nil
}

func (f *fakeCards) DuelTeam(_ context.Context, userID int64, _ int) ([]storage.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held[userID] == 0 {
		return nil, nil
	}
	return []storage.Card{f.card}, nil
}

func (f *fakeCards) ResolveDuel(_ context.Context, _, messageID, winner, loser int64, stake int) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.duels[messageID] {
		return 0, false, nil
	}
	f.duels[messageID] = true
	moved := min(stake, f.shards[loser])
	if winner != 0 {
		f.shards[loser] -= moved
		f.shards[winner] += moved
	}
	return moved, true, nil
}

// trades are the fake's trades, by ID; named are the cards each player
// holds by name, which gifts and trades move.
type fakeTrade struct {
	from, to   int64
	give, take storage.Card
	open       bool
}

func (f *fakeCards) HeldCardsNamed(_ context.Context, userID int64, name string, limit int) ([]storage.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found []storage.Card
	for _, c := range f.named[userID] {
		if strings.Contains(strings.ToLower(c.Name), strings.ToLower(name)) && len(found) < limit {
			found = append(found, c)
		}
	}
	return found, nil
}

// moveNamed moves the card from one player to the other, false when the
// first does not hold it.
func (f *fakeCards) moveNamed(from, to, cardID int64) bool {
	for i, c := range f.named[from] {
		if c.ID == cardID {
			f.named[from] = append(f.named[from][:i:i], f.named[from][i+1:]...)
			f.named[to] = append(f.named[to], c)
			return true
		}
	}
	return false
}

func (f *fakeCards) OfferCardTrade(_ context.Context, _, from, to, give, take int64, _ time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	card := func(user, id int64) storage.Card {
		for _, c := range f.named[user] {
			if c.ID == id {
				return c
			}
		}
		return storage.Card{}
	}
	f.trades = append(f.trades, &fakeTrade{from: from, to: to, give: card(from, give), take: card(to, take), open: true})
	return int64(len(f.trades)), nil
}

func (f *fakeCards) AcceptCardTrade(_ context.Context, id, by int64, _ storage.CardTradeRules) (storage.CardTrade, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id < 1 || int(id) > len(f.trades) || !f.trades[id-1].open || f.trades[id-1].to != by {
		return storage.CardTrade{}, storage.ErrCardTradeOver
	}
	tr := f.trades[id-1]
	if !f.moveNamed(tr.from, tr.to, tr.give.ID) || !f.moveNamed(tr.to, tr.from, tr.take.ID) {
		return storage.CardTrade{}, storage.ErrCardNotHeld
	}
	tr.open = false
	return storage.CardTrade{ID: id, From: tr.from, To: tr.to, Give: tr.give, Take: tr.take}, nil
}

func (f *fakeCards) CallOffCardTrade(_ context.Context, id, by int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id < 1 || int(id) > len(f.trades) || !f.trades[id-1].open || (by != f.trades[id-1].from && by != f.trades[id-1].to) {
		return false, nil
	}
	f.trades[id-1].open = false
	return true, nil
}

func (f *fakeCards) CardCollections(context.Context) (map[string]int, error) {
	return map[string]int{"anime": cards.CatalogSize}, nil
}
func (f *fakeCards) SaveCardCatalog(context.Context, []storage.Card) error { return nil }
func (f *fakeCards) CardTop(_ context.Context, limit int) ([]storage.CardPlayer, error) {
	return f.top[:min(limit, len(f.top))], nil
}

func (f *fakeCards) CardCounts(_ context.Context, userID int64) (map[int]int, map[int]int, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	owned := map[int]int{}
	if f.held[userID] > 0 {
		owned[4] = 1
	}
	return map[int]int{0: 50, 1: 60, 2: 45, 3: 30, 4: 15}, owned, int64(f.held[userID]) * 5000, nil
}

func (f *fakeCards) OwnedCards(_ context.Context, userID int64, rarity int) ([]storage.OwnedCard, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held[userID] == 0 || (rarity >= 0 && rarity != 4) {
		return nil, nil
	}
	return []storage.OwnedCard{{Card: f.card, Count: f.held[userID]}}, nil
}

// /card, or the word alone in a private chat, opens no card in the chat: it
// answers with the app's button, where cards open.
func TestCardOpensOnlyInTheApp(t *testing.T) {
	f := newFixture()
	f.opts.Cards = newFakeCards("")
	f.opts.CardsURL = "https://example.test/kartalar/"
	sent := f.run(textFrom(userID, "/card"), textFrom(userID, "karta"))
	if len(sent) != 2 {
		t.Fatalf("sent = %+v", sent)
	}
	for _, m := range sent {
		if m.photo || len(m.buttons) != 1 || !strings.Contains(m.text, "ilovada") {
			t.Errorf("a card asked for in the chat = %+v, want the app's button", m)
		}
	}
}

func TestSpaced(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 950: "950", 1550: "1 550", 1234567: "1 234 567"} {
		if got := spaced(n); got != want {
			t.Errorf("spaced(%d) = %q; want %q", n, got, want)
		}
	}
}

// In a group the bot's own commands are answered plain too: its answers name
// them so, and a press of one sends it without the bot's name. Another bot's,
// or a command more than one bot has, is still left alone.
func TestOwnCommandsAreAnsweredPlainInAGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("\x89PNG")) }))
	defer server.Close()
	for text, want := range map[string]int{"/card": 1, "/Card": 1, "/kartalarim": 0, "/games": 1, "/card@OtherBot": 0, "/top": 0} {
		f := newFixture()
		f.opts.Cards = newFakeCards(server.URL + "/g.png")
		f.opts.CardsURL = "https://app/kartalar"
		f.opts.Buttons = &fakeButtons{}
		if sent := f.run(inGroup(groupMessage(text))); len(sent) != want {
			t.Errorf("%s: sent %d, want %d", text, len(sent), want)
		}
	}
}

// /card is not counted in the group's quota, which is for the model's talk:
// a member out of answers is still given the app's button.
func TestCardTakesNoGroupQuota(t *testing.T) {
	f := newFixture()
	f.opts.Cards = newFakeCards("")
	f.opts.CardsURL = "https://example.test/kartalar/"
	f.opts.Bot.Username = "testbot"
	f.opts.GroupReplies = &fakeGroupQuota{}
	f.opts.GroupRatePerMinute, f.opts.MemberRepliesPer10Min = 6, 1
	f.opts.Workers = 1
	sent := f.run(inGroup(groupMessage("Mira, salom")), inGroup(groupMessage("/card")))
	if len(sent) != 2 || len(sent[1].buttons) != 1 || sent[1].photo {
		t.Fatalf("sent = %+v, want the answer and then the app's button", sent)
	}
}

// In a group /boss goes through send_game, whose two games in ten minutes it
// keeps, and takes none of the group's quota; one too soon gets only a reaction.
func TestGameCommandKeepsTheGamesLimitInAGroup(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	table := &fakeGameTable{}
	f.opts.Game, f.opts.GameTable = fakeGameLinks{}, table
	quota := &fakeGroupQuota{}
	f.opts.GroupReplies = quota
	f.opts.GroupRatePerMinute, f.opts.MemberRepliesPer10Min = 6, 1
	runner.answers["send_game"] = succeeded(`{"sent":true}`)

	if sent := f.run(inGroup(groupMessage("/boss"))); len(sent) != 0 || len(runner.recorded()) != 1 || runner.recorded()[0] != `send_game {"game":"boss"}` {
		t.Fatalf("sent %+v, tools %q", sent, runner.recorded())
	}
	if asked := quota.asked(); len(asked) != 0 {
		t.Errorf("quota asked %d times for a game", len(asked))
	}
	runner.answers["send_game"] = `{"ok":false,"error":{"code":"invalid_arguments","message":"two games in ten minutes"}}`
	if sent := f.run(inGroup(groupMessage("/boss"))); len(sent) != 0 {
		t.Errorf("a game too soon sent %+v", sent)
	}
	if got := f.reactor.recorded(); len(got) != 1 || got[0].emoji != overQuotaReaction {
		t.Errorf("reactions = %+v", got)
	}
}

// /games lists every game to tap, in a private chat and plain in a group,
// the page games only when they are served.
func TestGamesMenuListsTheGames(t *testing.T) {
	f := newFixture()
	f.opts.Cards = newFakeCards("")
	sent := f.run(textFrom(userID, "/games"))
	if len(sent) != 1 || !strings.Contains(sent[0].text, "/tictactoe") || strings.Contains(sent[0].text, "/boss") || !strings.Contains(sent[0].text, "/card") || strings.Contains(sent[0].text, "/duel") {
		t.Fatalf("games without pages = %+v", sent)
	}
	f.opts.Game, f.opts.GameTable = fakeGameLinks{}, &fakeGameTable{}
	sent = f.run(inGroup(groupMessage("/games")))
	if len(sent) != 2 || !strings.Contains(sent[1].text, "/boss") || !strings.Contains(sent[1].text, "/checkers") {
		t.Errorf("games in a group = %+v", sent)
	}
}

// The card game's old commands are gone: /card alone opens the app. In a
// group they get no answer, named for the bot or not; in a private chat the
// answer for a command the bot does not know.
func TestRetiredCardCommands(t *testing.T) {
	for _, command := range []string{"/cards", "/mycards", "/cardtop", "/market", "/duel", "/profile"} {
		f := newFixture()
		f.opts.Cards = newFakeCards("")
		f.opts.CardsURL = "https://app/kartalar"
		f.opts.Buttons = &fakeButtons{}
		f.opts.Bot.Username = "testbot"
		if sent := f.run(inGroup(groupMessage(command)), inGroup(groupMessage(command+"@testbot"))); len(sent) != 0 {
			t.Errorf("%s in a group = %+v, want no answer", command, sent)
		}
		if sent := f.run(textFrom(userID, command)); len(sent) != 1 || sent[0].text != textUnknown.uz || len(sent[0].buttons) != 0 {
			t.Errorf("%s in private = %+v, want the unknown command's answer", command, sent)
		}
	}
}

// "/start bozor", which the market's old buttons send, still opens the Mini
// App at its market.
func TestStartBozorOpensTheMarket(t *testing.T) {
	f := newFixture()
	f.opts.Cards = newFakeCards("")
	f.opts.CardsURL = "https://app/kartalar"
	sent := f.run(textFrom(userID, "/start bozor"))
	if b := sent[len(sent)-1].buttons; len(b) != 1 || b[0][0].WebApp == nil || !strings.HasSuffix(b[0][0].WebApp.URL, "?tab=market") {
		t.Errorf("/start bozor = %+v", sent[len(sent)-1])
	}
}
