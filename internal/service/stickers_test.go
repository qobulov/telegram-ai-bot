package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/stickers"
	"telegram-ai-bot/internal/storage"
)

type fakeStickers struct {
	mu     sync.Mutex
	asked  []string
	err    error
	sent   []string
	report stickers.Report
	pack   string
	// none says no pack was imported.
	none bool
}

func (f *fakeStickers) PickSticker(_ context.Context, _ int64, category string, gap time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, category)
	if f.err != nil {
		return "", f.err
	}
	return "file-" + category, nil
}

func (f *fakeStickers) HasStickers(context.Context) (bool, error) {
	return !f.none, nil
}

func (f *fakeStickers) SendSticker(_ context.Context, _ int64, fileID string, _ int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, fileID)
	return 700, nil
}

func (f *fakeStickers) Import(_ context.Context, name string) (stickers.Report, error) {
	f.pack = name
	return f.report, nil
}

func withStickers(f *fixture) *fakeStickers {
	st := &fakeStickers{}
	f.opts.Stickers, f.opts.StickerSender, f.opts.StickerImporter = st, st, st
	return st
}

func TestStickerFollowsTheAnswer(t *testing.T) {
	f := newFixture()
	st := withStickers(f)
	f.replier.answer, f.replier.sticker = "Voy, maqtanish boshlandi", "sarcasm"

	sent := f.run(inGroup(groupMessage("Mira, men eng zo'r o'yinchiman")))

	if len(sent) != 1 || sent[0].text != "Voy, maqtanish boshlandi" {
		t.Errorf("sent = %+v", sent)
	}
	if len(st.sent) != 1 || st.sent[0] != "file-sarcasm" {
		t.Errorf("stickers sent = %q", st.sent)
	}
	if got := f.chatLog.texts(groupID); !strings.Contains(strings.Join(got, "\n"), "You: Voy, maqtanish boshlandi <sticker sarcasm>") {
		t.Errorf("log = %q", got)
	}
}

// A sticker alone is an answer; an unknown category, none of it or one
// sent too recently is no sticker, and never costs the text.
func TestStickerAloneOrNone(t *testing.T) {
	for name, c := range map[string]struct {
		text, sticker string
		err           error
		want          int
	}{
		"alone":            {"", "funny", nil, 1},
		"unknown category": {"ok", "dancing", nil, 0},
		"none to send":     {"ok", "funny", storage.ErrNoSticker, 0},
		"the store fails":  {"ok", "funny", errors.New("db down"), 0},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			st := withStickers(f)
			st.err = c.err
			f.replier.answer, f.replier.sticker = c.text, c.sticker
			sent := f.run(inGroup(groupMessage("Mira, xaxa")))
			if len(st.sent) != c.want {
				t.Errorf("stickers sent = %q, want %d", st.sent, c.want)
			}
			if c.text != "" && (len(sent) != 1 || sent[0].text != c.text) {
				t.Errorf("text = %+v, want %q kept", sent, c.text)
			}
		})
	}
}

func TestImportStickersIsForOwners(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	st := withStickers(f)
	st.report = stickers.Report{Pack: "xaa_by_qstickbot", StickerCount: storage.StickerCount{Total: 120, Labelled: 70, Enabled: 55}}

	stranger := f.run(textFrom(userID, "/import_stickers https://t.me/addstickers/xaa_by_qstickbot"))
	if len(stranger) != 1 || stranger[0].text != textForbidden || st.pack != "" {
		t.Fatalf("a stranger got %+v, import of %q", stranger, st.pack)
	}
	owner := textFrom(9001, "/import_stickers https://t.me/addstickers/xaa_by_qstickbot")
	owner.Message.From.Username = ownerUsername
	sent := f.run(owner)
	if st.pack != "xaa_by_qstickbot" || len(sent) != 2 || !strings.Contains(sent[1].text, "120 ta stiker, 70 tasi ko'rib chiqildi, 55 tasi") || !strings.Contains(sent[1].text, "Qolgan 50 tasi") {
		t.Errorf("import of %q, sent %+v", st.pack, sent)
	}
}

// Before a pack is imported the model is told it has no stickers: it once
// said "here you are" with none to send.
func TestModelIsToldWhenThereAreNoStickers(t *testing.T) {
	for name, none := range map[string]bool{"no pack yet": true, "a pack": false} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			st := withStickers(f)
			st.none = none
			f.run(inGroup(groupMessage("Mira, stiker tasha")))
			if got := strings.Contains(onlyRequest(t, f).req.Context, noStickers); got != none {
				t.Errorf("told there are no stickers = %v, want %v", got, none)
			}
		})
	}
}

// The log records a sticker only when one was sent.
func TestLogRecordsOnlyASentSticker(t *testing.T) {
	f := newFixture()
	st := withStickers(f)
	st.err = storage.ErrNoSticker
	f.replier.answer, f.replier.sticker = "Mana", "cool"
	f.run(inGroup(groupMessage("Mira, stiker tasha")))
	if got := strings.Join(f.chatLog.texts(groupID), "\n"); strings.Contains(got, "<sticker") {
		t.Errorf("log = %q, want no sticker recorded", got)
	}
}

type fakeGIFs struct {
	mu        sync.Mutex
	links     []string
	searchErr error
	sendErr   error
	slotTaken bool
	sent      []string
}

func (f *fakeGIFs) Search(context.Context, string) ([]string, error) { return f.links, f.searchErr }

func (f *fakeGIFs) SendAnimation(_ context.Context, _ int64, link string, _ int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return 0, f.sendErr
	}
	f.sent = append(f.sent, link)
	return 800, nil
}

func (f *fakeGIFs) ClaimMediaSlot(context.Context, int64, time.Duration) (bool, error) {
	return !f.slotTaken, nil
}

// A GIF the model asks for is sent; one that cannot be becomes a sticker of
// the same category; one past the chat's slot is nothing; and without GIPHY
// it is a sticker.
func TestGIFOrItsSticker(t *testing.T) {
	for name, c := range map[string]struct {
		gifs              *fakeGIFs
		wantGIF, wantStkr int
		log               string
	}{
		"sent":           {&fakeGIFs{links: []string{"https://g/1.mp4"}}, 1, 0, "<gif facepalm>"},
		"search fails":   {&fakeGIFs{searchErr: errors.New("429")}, 0, 1, "<sticker facepalm>"},
		"none found":     {&fakeGIFs{}, 0, 1, "<sticker facepalm>"},
		"send fails":     {&fakeGIFs{links: []string{"https://g/1.mp4"}, sendErr: errors.New("400")}, 0, 1, "<sticker facepalm>"},
		"slot taken":     {&fakeGIFs{links: []string{"https://g/1.mp4"}, slotTaken: true}, 0, 0, ""},
		"no GIPHY wired": {nil, 0, 1, "<sticker facepalm>"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			st := withStickers(f)
			if c.gifs != nil {
				f.opts.GIFs, f.opts.AnimationSender, f.opts.MediaSlots = c.gifs, c.gifs, c.gifs
			}
			f.replier.answer, f.replier.gif = "Yana shu ahvol", "facepalm"
			f.run(inGroup(groupMessage("Mira, yana yutqazdik")))
			gifsSent := 0
			if c.gifs != nil {
				gifsSent = len(c.gifs.sent)
			}
			if gifsSent != c.wantGIF || len(st.sent) != c.wantStkr {
				t.Errorf("GIFs %d, stickers %q; want %d and %d", gifsSent, st.sent, c.wantGIF, c.wantStkr)
			}
			log := strings.Join(f.chatLog.texts(groupID), "\n")
			if c.log != "" && !strings.Contains(log, c.log) || c.log == "" && (strings.Contains(log, "<gif") || strings.Contains(log, "<sticker")) {
				t.Errorf("log = %q, want %q", log, c.log)
			}
		})
	}
}
