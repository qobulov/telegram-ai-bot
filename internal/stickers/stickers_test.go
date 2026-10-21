package stickers

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

type fakeTelegram struct {
	set        telegram.StickerSet
	mu         sync.Mutex
	downloaded []string
}

func (f *fakeTelegram) GetStickerSet(context.Context, string) (telegram.StickerSet, error) {
	return f.set, nil
}

func (f *fakeTelegram) DownloadFile(_ context.Context, fileID string, _ int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloaded = append(f.downloaded, fileID)
	return []byte("RIFF\x00\x00\x00\x00WEBPVP8 " + fileID), nil
}

type label struct {
	categories  []string
	description string
	enabled     bool
}

type fakeStore struct {
	mu     sync.Mutex
	saved  map[string]storage.PackSticker
	labels map[string]label
}

func (f *fakeStore) SaveStickers(_ context.Context, stickers []storage.PackSticker) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	added := 0
	for _, st := range stickers {
		if _, ok := f.saved[st.FileUniqueID]; !ok {
			added++
		}
		f.saved[st.FileUniqueID] = st
	}
	return added, nil
}

func (f *fakeStore) UnlabelledStickers(_ context.Context, pack string) ([]storage.PackSticker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []storage.PackSticker
	for id, st := range f.saved {
		if _, done := f.labels[id]; !done && st.Pack == pack {
			out = append(out, st)
		}
	}
	return out, nil
}

func (f *fakeStore) LabelSticker(_ context.Context, id string, categories []string, description string, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels[id] = label{categories, description, enabled}
	return nil
}

func (f *fakeStore) PackStickers(context.Context, string) (storage.StickerCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := storage.StickerCount{Total: len(f.saved), Labelled: len(f.labels)}
	for _, l := range f.labels {
		if l.enabled {
			c.Enabled++
		}
	}
	return c, nil
}

// fakeModel answers by what the picture says: its bytes end with the file ID.
type fakeModel struct{ answers map[string]string }

func (f fakeModel) Complete(_ context.Context, req ai.Request) (ai.Message, error) {
	url := req.Messages[0].Parts[0].ImageURL.URL
	for id, answer := range f.answers {
		if strings.HasSuffix(decoded(url), id) {
			if answer == "fail" {
				return ai.Message{}, errors.New("ai: 503")
			}
			return ai.Message{Content: answer}, nil
		}
	}
	return ai.Message{}, errors.New("no answer")
}

func TestPackName(t *testing.T) {
	for link, want := range map[string]string{
		"https://t.me/addstickers/xaa_by_qstickbot": "xaa_by_qstickbot",
		"t.me/addstickers/xaa_by_qstickbot/":        "xaa_by_qstickbot",
		"xaa_by_qstickbot":                          "xaa_by_qstickbot",
		"https://evil.example/addstickers/xaa":      "",
		"two words":                                 "",
	} {
		if got, ok := PackName(link); got != want || ok != (want != "") {
			t.Errorf("PackName(%q) = %q, %v; want %q", link, got, ok, want)
		}
	}
}

// The model looks at each sticker once: a static one itself, a moving one
// by its thumbnail. Unfit stickers, those that fit no category and those
// without a picture are kept but not enabled; one the model failed on waits
// for the next import.
func TestImportLabelsEachStickerOnce(t *testing.T) {
	tg := &fakeTelegram{set: telegram.StickerSet{Name: "xaa", Stickers: []telegram.Sticker{
		{FileID: "f1", FileUniqueID: "u1", Emoji: "🫥"},
		{FileID: "f2", FileUniqueID: "u2", IsVideo: true, Thumbnail: &telegram.PhotoSize{FileID: "t2"}},
		{FileID: "f3", FileUniqueID: "u3"},
		{FileID: "f4", FileUniqueID: "u4"},
		{FileID: "f5", FileUniqueID: "u5", IsAnimated: true},
		{FileID: "f6", FileUniqueID: "u6"},
	}}}
	store := &fakeStore{saved: map[string]storage.PackSticker{}, labels: map[string]label{}}
	im := &Importer{Telegram: tg, Store: store, Workers: 3, Model: fakeModel{answers: map[string]string{
		"f1": "```json\n{\"categories\": [\"Funny\", \"sarcasm\", \"dancing\"], \"description\": \"a  laughing cat\", \"fit\": true}\n```",
		"t2": `{"categories": ["celebration"], "description": "a man dancing", "fit": true}`,
		"f3": `{"categories": ["funny"], "description": "rude text", "fit": false}`,
		"f4": `{"categories": [], "description": "a chair", "fit": true}`,
		"f6": "fail",
	}}}

	report, err := im.Import(context.Background(), "xaa")

	if err != nil || report.Added != 6 || report.Total != 6 || report.Labelled != 5 || report.Enabled != 2 {
		t.Fatalf("report = %+v, err = %v", report, err)
	}
	if l := store.labels["u1"]; !l.enabled || strings.Join(l.categories, ",") != "funny,sarcasm" || l.description != "a laughing cat" {
		t.Errorf("u1 = %+v", l)
	}
	if l := store.labels["u2"]; !l.enabled || l.categories[0] != "celebration" {
		t.Errorf("the video sticker = %+v, want it labelled by its thumbnail", l)
	}
	for _, id := range []string{"u3", "u4", "u5"} {
		if store.labels[id].enabled {
			t.Errorf("%s is enabled", id)
		}
	}
	if _, ok := store.labels["u6"]; ok {
		t.Error("a sticker the model failed on was labelled")
	}
	im.Model = fakeModel{answers: map[string]string{"f6": `{"categories": ["shock"], "description": "wow", "fit": true}`}}
	if report, _ := im.Import(context.Background(), "xaa"); report.Added != 0 || report.Labelled != 6 {
		t.Errorf("second import = %+v, want only the one left labelled", report)
	}
}

// decoded is the bytes of a data URL, as text.
func decoded(url string) string {
	_, data, _ := strings.Cut(url, ",")
	raw, _ := base64Decode(data)
	return string(raw)
}

var base64Decode = base64.StdEncoding.DecodeString

// The persona lists the categories in this order; changing one means
// changing both.
func TestCategoriesMatchThePersona(t *testing.T) {
	if got := strings.Join(Categories, ", "); got != "funny, sarcasm, flirting, jealousy, sad, comfort, shock, celebration, facepalm, angry, wholesome, cool" {
		t.Errorf("Categories = %s; update the persona's sticker line too", got)
	}
}
