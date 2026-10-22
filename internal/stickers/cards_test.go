package stickers

import (
	"context"
	"slices"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

func (f *fakeStore) UnnamedCards(_ context.Context, pack string) ([]storage.PackSticker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []storage.PackSticker
	for id, st := range f.saved {
		l := f.labels[id]
		if st.Pack == pack && !slices.Contains(l.categories, storage.ItemCard) && !slices.Contains(l.categories, storage.NotItemCard) {
			out = append(out, st)
		}
	}
	return out, nil
}

func (f *fakeStore) PackCards(context.Context, string) (storage.CardCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := storage.CardCount{Total: len(f.saved)}
	for _, l := range f.labels {
		if slices.Contains(l.categories, storage.ItemCard) || slices.Contains(l.categories, storage.NotItemCard) {
			c.Looked++
		}
		if l.enabled && slices.Contains(l.categories, storage.ItemCard) {
			c.Named++
		}
	}
	return c, nil
}

// Each card is named once, by the item on it, even one labelled before as a
// mood; a sticker with no item, or no picture, is marked as no card.
func TestCardImportNamesTheItems(t *testing.T) {
	tg := &fakeTelegram{set: telegram.StickerSet{Name: "items", Stickers: []telegram.Sticker{
		{FileID: "f1", FileUniqueID: "u1"},
		{FileID: "f2", FileUniqueID: "u2"},
		{FileID: "f3", FileUniqueID: "u3", IsAnimated: true},
		{FileID: "f4", FileUniqueID: "u4"},
	}}}
	store := &fakeStore{saved: map[string]storage.PackSticker{}, labels: map[string]label{"u1": {[]string{"funny"}, "item stats", true}}}
	im := &CardImporter{Telegram: tg, Store: store, Workers: 2, Model: fakeModel{answers: map[string]string{
		"f1": "```json\n{\"item\": \"  Athena's   Shield \"}\n```",
		"f2": `{"item": ""}`,
		"f4": "fail",
	}}}

	report, err := im.Import(context.Background(), "items")

	if err != nil || report.Total != 4 || report.Looked != 3 || report.Named != 1 {
		t.Fatalf("report = %+v, err = %v", report, err)
	}
	if l := store.labels["u1"]; !l.enabled || l.description != "Athena's Shield" || !slices.Equal(l.categories, []string{storage.ItemCard}) {
		t.Errorf("u1 = %+v, want named and no longer a mood", l)
	}
	for _, id := range []string{"u2", "u3"} {
		if l := store.labels[id]; l.enabled || !slices.Equal(l.categories, []string{storage.NotItemCard}) {
			t.Errorf("%s = %+v, want marked as no card", id, l)
		}
	}
	im.Model = fakeModel{answers: map[string]string{"f4": `{"item": "Blade of Despair"}`}}
	if report, _ := im.Import(context.Background(), "items"); report.Looked != 4 || report.Named != 2 {
		t.Errorf("second import = %+v, want only the one left named", report)
	}
}
