package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// autoBook is fakeCards with players whose auto opener is on.
type autoBook struct {
	*fakeCards
	players []storage.CardAuto
	stopped []int64
}

func (b *autoBook) DueCardAutos(_ context.Context, now time.Time, every time.Duration, limit int) ([]storage.CardAuto, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var due []storage.CardAuto
	for _, p := range b.players {
		if last, ok := b.last[p.UserID]; (!ok || now.Sub(last) >= every) && len(due) < limit {
			due = append(due, p)
		}
	}
	return due, nil
}

func (b *autoBook) StopCardAuto(_ context.Context, userID int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = append(b.stopped, userID)
	for i, p := range b.players {
		if p.UserID == userID {
			b.players = append(b.players[:i], b.players[i+1:]...)
			break
		}
	}
	return nil
}

// autoSender records what it sent and refuses the chats in refused.
type autoSender struct {
	mu      sync.Mutex
	photos  []telegram.Photo
	texts   []telegram.Outgoing
	refused map[int64]bool
}

func (s *autoSender) SendPhoto(_ context.Context, photo telegram.Photo) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refused[photo.ChatID] {
		return 0, &telegram.APIError{Code: 403, Description: "Forbidden: bot was blocked by the user"}
	}
	s.photos = append(s.photos, photo)
	return 1, nil
}

func (s *autoSender) SendMessage(_ context.Context, msg telegram.Outgoing) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, msg)
	return 1, nil
}

type autoPictures struct{ fail bool }

func (p autoPictures) FetchPicture(context.Context, string) ([]byte, string, error) {
	if p.fail {
		return nil, "", errors.New("no picture")
	}
	return []byte("PNG"), "image/png", nil
}

// A player with the auto opener on has each card opened when it may be and
// sent quietly to their private chat in their language, once; a player the
// bot may not write to keeps the card and has the opener turned off.
func TestDueCardsAreOpenedForThePlayers(t *testing.T) {
	book := &autoBook{fakeCards: newFakeCards("https://img/gojo.png"), players: []storage.CardAuto{
		{UserID: 1, Name: "Bek", Language: "uz"},
		{UserID: 2, Name: "Ivan", Language: "ru"},
		{UserID: 3, Name: "Gone", Language: "uz"},
	}}
	sender := &autoSender{refused: map[int64]bool{3: true}}
	now := time.Now()
	opts := CardAutoOptions{Store: book, Sender: sender, Pictures: autoPictures{}, CardsURL: "https://app/kartalar", Now: func() time.Time { return now }}

	opened, err := OpenDueCards(context.Background(), opts)
	if err != nil || opened != 3 {
		t.Fatalf("opened %d, %v", opened, err)
	}
	if len(sender.photos) != 2 {
		t.Fatalf("photos = %+v", sender.photos)
	}
	for _, p := range sender.photos {
		if !p.Silent || string(p.Data) != "PNG" || len(p.Buttons) != 1 || p.Buttons[0][0].WebApp == nil {
			t.Errorf("photo = %+v", p)
		}
	}
	if c := sender.photos[0].Caption; sender.photos[0].ChatID != 1 || !strings.HasPrefix(c, "🤖 Avto-ochildi") || !strings.Contains(c, "Gojo") {
		t.Errorf("Bek's card:\n%s", c)
	}
	if c := sender.photos[1].Caption; !strings.HasPrefix(c, "🤖 Открыто автоматически") {
		t.Errorf("Ivan's card:\n%s", c)
	}
	if book.held[3] != 1 || len(book.stopped) != 1 || book.stopped[0] != 3 {
		t.Errorf("the blocked player held %d, stopped %v", book.held[3], book.stopped)
	}

	// A minute on, no card is ready; an hour on, the next one is opened.
	now = now.Add(time.Minute)
	if opened, _ := OpenDueCards(context.Background(), opts); opened != 0 {
		t.Errorf("a minute later opened %d", opened)
	}
	now = now.Add(time.Hour)
	if opened, _ := OpenDueCards(context.Background(), opts); opened != 2 || book.held[1] != 2 {
		t.Errorf("an hour later opened %d, Bek holds %d", opened, book.held[1])
	}
}

// A card whose picture cannot be had is sent as its words, quietly.
func TestAnOpenedCardWithoutItsPicture(t *testing.T) {
	book := &autoBook{fakeCards: newFakeCards(""), players: []storage.CardAuto{{UserID: 1, Name: "Bek", Language: "en"}}}
	sender := &autoSender{}
	if opened, err := OpenDueCards(context.Background(), CardAutoOptions{Store: book, Sender: sender, Pictures: autoPictures{fail: true}}); err != nil || opened != 1 {
		t.Fatalf("opened %d, %v", opened, err)
	}
	if len(sender.texts) != 1 || !sender.texts[0].Silent || !strings.HasPrefix(sender.texts[0].Text, "🤖 Opened for you") {
		t.Errorf("texts = %+v", sender.texts)
	}
}

// /avto turns the auto opener on, and again off.
func TestAvtoTogglesTheAutoOpener(t *testing.T) {
	f := newFixture()
	f.opts.Cards = newFakeCards("")
	if sent := f.run(textFrom(userID, "/avto")); len(sent) != 1 || !strings.Contains(sent[0].text, "Avto-ochish yoqildi") {
		t.Fatalf("on = %+v", sent)
	}
	if sent := f.run(textFrom(userID, "/avto")); len(sent) != 2 || !strings.Contains(sent[1].text, "o'chirildi") {
		t.Fatalf("off = %+v", sent)
	}
}
