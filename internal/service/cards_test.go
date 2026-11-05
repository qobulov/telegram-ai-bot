package service

import (
	"context"
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
}

func newFakeCards(picture string) *fakeCards {
	return &fakeCards{
		card: storage.Card{ID: 1, Name: "Gojo *Satoru*", Anime: "Jujutsu Kaisen", ImageURL: picture, Rarity: 4, Attack: 5130, Health: 18020, Value: 5000},
		last: map[int64]time.Time{}, held: map[int64]int{},
	}
}

func (f *fakeCards) CardCatalogSize(context.Context) (int, error)               { return 200, nil }
func (f *fakeCards) SaveCardCatalog(context.Context, []storage.Card) error      { return nil }
func (f *fakeCards) CardTop(context.Context, int) ([]storage.CardPlayer, error) { return nil, nil }

func (f *fakeCards) DrawCard(_ context.Context, userID int64, _ string, _ int, every time.Duration, now time.Time) (storage.CardDraw, error) {
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
// the next asks to wait, and an owner never does.
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
	if sent = f.run(textFrom(userID, "/karta")); len(sent) != 3 || !sent[2].photo || !strings.Contains(sent[2].text, "×2") {
		t.Errorf("an owner's draw = %+v", sent)
	}
	if book.everys[len(book.everys)-1] != 0 {
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
