package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeCards keeps one card, draws it once an hour, and counts what it was
// asked.
type fakeCards struct {
	mu     sync.Mutex
	card   storage.Card
	last   map[int64]time.Time
	held   map[int64]int
	everys []time.Duration
	top    []storage.CardPlayer
}

func newFakeCards(picture string) *fakeCards {
	return &fakeCards{
		card: storage.Card{ID: 1, Name: "Gojo *Satoru*", Anime: "Jujutsu Kaisen", ImageURL: picture, Rarity: 4, Attack: 5130, Health: 18020, Value: 5000},
		last: map[int64]time.Time{}, held: map[int64]int{},
	}
}

func (f *fakeCards) CardCollections(context.Context) (map[string]int, error) {
	return map[string]int{"anime": 200}, nil
}
func (f *fakeCards) SaveCardCatalog(context.Context, []storage.Card) error   { return nil }
func (f *fakeCards) CardSetVersions(context.Context) (map[string]int, error) { return nil, nil }
func (f *fakeCards) CardCatalog(context.Context) ([]storage.Card, error)     { return nil, nil }
func (f *fakeCards) FillCardSet(context.Context, string, int, []storage.Card, map[int64]int64) error {
	return nil
}
func (f *fakeCards) CardTop(_ context.Context, limit int) ([]storage.CardPlayer, error) {
	return f.top[:min(limit, len(f.top))], nil
}

func (f *fakeCards) DrawCard(_ context.Context, userID int64, _ string, _ storage.CardRoll, every time.Duration, now time.Time) (storage.CardDraw, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.everys = append(f.everys, every)
	if last, ok := f.last[userID]; ok && now.Sub(last) < every {
		return storage.CardDraw{}, &storage.CardWaitError{Wait: every - now.Sub(last)}
	}
	f.last[userID] = now
	f.held[userID]++
	return storage.CardDraw{Card: f.card, New: f.held[userID] == 1, Count: f.held[userID], Points: int64(f.held[userID]) * 5000}, nil
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

// A card is drawn with its picture and its numbers under it, once an hour:
// the next asks to wait, an owner's like anyone's.
func TestCardIsDrawnOnceAnHour(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("\x89PNG card")) }))
	defer server.Close()
	f := newFixture()
	book := newFakeCards(server.URL + "/gojo.png")
	f.opts.Cards = book

	sent := f.run(textFrom(userID, "/karta"))
	if len(sent) != 1 || !sent[0].photo {
		t.Fatalf("sent = %+v, want the card's picture", sent)
	}
	for _, want := range []string{"👑", "Gojo Satoru", "Jujutsu Kaisen", "5 130", "18 020", "+5 000", "🆕"} {
		if !strings.Contains(sent[0].text, want) {
			t.Errorf("caption lacks %q:\n%s", want, sent[0].text)
		}
	}
	// f.run gives every message sent so far: the last is this draw's.
	if sent = f.run(textFrom(userID, "karta")); len(sent) != 2 || sent[1].photo || !strings.Contains(sent[1].text, "⏳") {
		t.Errorf("second draw = %+v, want to wait", sent)
	}
	f.opts.OwnerIDs = map[int64]bool{userID: true}
	if sent = f.run(textFrom(userID, "/karta")); len(sent) != 3 || sent[2].photo || !strings.Contains(sent[2].text, "⏳") {
		t.Errorf("an owner's draw = %+v, want to wait", sent)
	}
	if book.everys[len(book.everys)-1] != time.Hour {
		t.Errorf("an owner waited %v", book.everys[len(book.everys)-1])
	}
}

// A collection shows each rarity's cards held as buttons; its player opens
// a rarity's list and goes back, and anyone else is told it is not theirs.
func TestCardCollectionIsItsPlayers(t *testing.T) {
	f := newFixture()
	buttons := &fakeButtons{}
	f.opts.Buttons = buttons
	book := newFakeCards("")
	f.opts.Cards = book
	book.held[userID] = 2

	sent := f.run(textFrom(userID, "/kartalarim"))
	if len(sent) != 1 || len(sent[0].buttons) != 6 || sent[0].buttons[4][0].Text != "👑 Mifik - 1/15" || sent[0].buttons[5][0].Text != "🗂 Hammasi - 1/200" {
		t.Fatalf("collection = %+v", sent)
	}
	mythic := sent[0].buttons[4][0].Data
	private := telegram.Chat{ID: userID, Type: telegram.ChatTypePrivate}

	s := New(f.opts)
	defer s.stop()
	s.Handle(doozPlayer(1, &telegram.User{ID: userID, FirstName: "Bek"}, private, sent[0].text, mythic))
	edits, menus := buttons.recordedMenus()
	if len(edits) != 1 || !strings.Contains(edits[0], "Gojo Satoru ×2") || !strings.HasSuffix(menus[0][0][0].Data, ":b") {
		t.Fatalf("mythic list = %q %+v", edits, menus)
	}
	s.Handle(doozPlayer(2, &telegram.User{ID: userID + 1, FirstName: "Vali"}, private, sent[0].text, mythic))
	if edits, _ = buttons.recordedMenus(); len(edits) != 1 || !buttons.answers[1].alert || !strings.Contains(buttons.answers[1].text, "emas") {
		t.Errorf("a stranger's press = %q, answers %+v", edits, buttons.answers)
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
	for text, want := range map[string]int{"/kartalarim": 1, "/Kartalarim": 1, "/kartatop": 1, "/kartalarim@OtherBot": 0, "/top": 0} {
		f := newFixture()
		f.opts.Cards = newFakeCards(server.URL + "/g.png")
		f.opts.Buttons = &fakeButtons{}
		if sent := f.run(inGroup(groupMessage(text))); len(sent) != want {
			t.Errorf("%s: sent %d, want %d", text, len(sent), want)
		}
	}
}

// The table shows the top ten, the first three bold with medals, a long name
// cut, and the asker's own place under it when they are not among them.
func TestCardTopShowsTheAskersPlace(t *testing.T) {
	f := newFixture()
	book := newFakeCards("")
	for i := range 12 {
		book.top = append(book.top, storage.CardPlayer{UserID: int64(100 + i), Name: fmt.Sprintf("P%d", i), Points: int64(40000 - i*1000), Cards: 30 - i})
	}
	book.top[0].Name = "Стр.Лнт. Турсунов—͟͟͞͞𖣘 ✨ juda uzun ism bilan"
	book.top[11].UserID = userID
	f.opts.Cards = book

	sent := f.run(textFrom(userID, "/kartatop"))
	if len(sent) != 1 {
		t.Fatalf("sent = %+v", sent)
	}
	text := sent[0].text
	t.Log("\n" + text)
	for _, want := range []string{"🥇", "💠 40 000 · 🎴 30", "10. P9", "📍 Siz: 12-o'rin · 💠 29 000 · 🎴 19"} {
		if !strings.Contains(text, want) {
			t.Errorf("table lacks %q", want)
		}
	}
	if strings.Contains(text, "P10") || strings.Contains(text, "juda uzun ism bilan") {
		t.Errorf("table shows too much:\n%s", text)
	}
}

// /karta is not counted in the group's quota, as it has its own limit: a
// member out of answers still draws. Being told to wait is counted, so that
// pressing it cannot flood the group.
func TestCardDrawsTakeNoGroupQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("\x89PNG")) }))
	defer server.Close()
	f := newFixture()
	f.opts.Cards = newFakeCards(server.URL + "/g.png")
	quota := &fakeGroupQuota{}
	f.opts.GroupReplies = quota
	f.opts.GroupRatePerMinute, f.opts.MemberRepliesPer10Min = 6, 1
	f.opts.Workers = 1

	sent := f.run(inGroup(groupMessage("Mira, salom")), inGroup(groupMessage("/karta")))
	if len(sent) != 2 || !sent[1].photo {
		t.Fatalf("sent = %+v, want the answer and then the card", sent)
	}
	if sent = f.run(inGroup(groupMessage("/karta"))); len(sent) != 2 {
		t.Errorf("a wait over the quota sent %+v, want nothing more", sent[2:])
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

// A drawn card says in how many draws a Legendary and a Mythic are sure,
// and that it was one when the wait raised it.
func TestCardCaptionShowsTheSureCards(t *testing.T) {
	in := incoming{msg: &telegram.Message{From: &telegram.User{ID: 1, FirstName: "Bek"}}, text: "/karta"}
	draw := storage.CardDraw{Card: storage.Card{Name: "Gojo", Rarity: 4, Value: 5000}, New: true, Count: 1, Points: 5000, Guaranteed: true, SinceLegendary: 0, SinceMythic: 0}
	caption := cardCaption(in, draw)
	if !strings.Contains(caption, "✨ Kafolatlangan karta!") || !strings.Contains(caption, "🔥 30 · 👑 100") {
		t.Errorf("a sure Mythic's caption:\n%s", caption)
	}
	draw.Guaranteed, draw.SinceLegendary, draw.SinceMythic = false, 12, 70
	if caption = cardCaption(in, draw); strings.Contains(caption, "✨") || !strings.Contains(caption, "🔥 18 · 👑 30") {
		t.Errorf("a card's caption:\n%s", caption)
	}
}
