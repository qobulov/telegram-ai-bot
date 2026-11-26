package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

type memoryFarosat struct {
	mu    sync.Mutex
	grams map[int64]int
	day   map[int64]string
}

func (m *memoryFarosat) TakeFarosat(_ context.Context, _, userID int64, _ string, day time.Time, gain int) (int, int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	taken := m.day[userID] != day.Format("2006-01-02")
	if taken {
		m.grams[userID] += gain
		m.day[userID] = day.Format("2006-01-02")
	}
	return m.grams[userID], 1, taken, nil
}

func (m *memoryFarosat) FarosatTop(context.Context, int64, int) ([]storage.FarosatRow, error) {
	return []storage.FarosatRow{{UserID: 1, Name: "Ali", Grams: 215}, {UserID: 2, Name: "Vali", Grams: 203}}, nil
}

// /farosat is taken once a day, in code; /topfarosat ranks the group.
func TestFarosat(t *testing.T) {
	f := newFixture()
	store := &memoryFarosat{grams: map[int64]int{}, day: map[int64]string{}}
	f.opts.Farosat = store

	sent := f.run(inGroup(groupMessage("/farosat")))
	if len(sent) != 1 || !strings.Contains(sent[0].text, "gram") || !strings.Contains(sent[0].text, "yarim tundan keyin") || strings.Contains(sent[0].text, "allaqachon") {
		t.Fatalf("first take = %+v", sent)
	}
	sent = f.run(inGroup(groupMessage("/farosat")))
	if !strings.Contains(sent[len(sent)-1].text, "allaqachon oldingiz") {
		t.Errorf("second take = %+v", sent)
	}
	sent = f.run(inGroup(groupMessage("/topfarosat")))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "TOP 10") || !strings.Contains(last, "🥇 Ali — **215** gram") || !strings.Contains(last, "/farosat") {
		t.Errorf("top = %+v", sent)
	}
	sent = f.run(inGroup(groupMessage("/topfarosat 30")))
	if !strings.Contains(sent[len(sent)-1].text, "TOP 30") {
		t.Errorf("top 30 = %q", sent[len(sent)-1].text)
	}
	for _, c := range []struct {
		gain  int
		taken bool
		want  []string
	}{
		{10, true, []string{"**+10 gram**", "charaqlayapti", "**25 gram**", "**3-o'rin**", "yarim tundan keyin"}},
		{2, true, []string{"**+2 gram**", "Oz-ozdan"}},
		{-3, true, []string{"**−3 gram**", "kamaydi"}},
		{0, false, []string{"allaqachon oldingiz", "**25 gram**"}},
	} {
		card := farosatCard(incoming{msg: groupMessage("/farosat"), text: "/farosat"}, "Ali", c.gain, c.taken, 25, 3)
		for _, want := range c.want {
			if !strings.Contains(card, want) {
				t.Errorf("card for %d lacks %q:\n%s", c.gain, want, card)
			}
		}
	}
	if n := len(f.replier.recorded()); n != 0 {
		t.Errorf("the model was asked %d times", n)
	}
	for range 200 {
		if g := farosatGain(); g == 0 || g < -farosatMostLoss || g > farosatMostGain {
			t.Fatalf("gain = %d", g)
		}
	}
}
