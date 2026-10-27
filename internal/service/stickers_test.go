package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/stickers"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

type fakeStickers struct {
	mu     sync.Mutex
	asked  []string
	err    error
	sent   []string
	report stickers.Report
	pack   string
	// none says no pack was imported, and busy that the chat had a sticker
	// within the gap.
	none bool
	busy bool
}

func (f *fakeStickers) PickSticker(_ context.Context, _ int64, category string, gap time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, category)
	if f.err != nil {
		return "", f.err
	}
	if f.busy && gap > 0 {
		return "", storage.ErrNoSticker
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

// The owners asked for one or the other: a sticker alone is the answer, and
// words with a sticker are sent without it.
func TestStickerIsAnAnswerOfItsOwn(t *testing.T) {
	f := newFixture()
	st := withStickers(f)
	f.replier.sticker = "sarcasm"

	sent := f.run(inGroup(groupMessage("Mira, men eng zo'r o'yinchiman")))

	if len(sent) != 0 || len(st.sent) != 1 || st.sent[0] != "file-sarcasm" {
		t.Errorf("texts %+v, stickers %q; want the sticker alone", sent, st.sent)
	}
	if got := f.chatLog.texts(groupID); !strings.Contains(strings.Join(got, "\n"), "You: <sticker sarcasm>") {
		t.Errorf("log = %q", got)
	}

	f = newFixture()
	st = withStickers(f)
	f.replier.answer, f.replier.sticker = "Voy, maqtanish boshlandi", "sarcasm"
	if sent := f.run(inGroup(groupMessage("Mira, men eng zo'r o'yinchiman"))); len(sent) != 1 || len(st.sent) != 0 {
		t.Errorf("texts %+v, stickers %q; want the words alone", sent, st.sent)
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

func (f *fakeGIFs) ClaimMediaSlot(_ context.Context, _ int64, gap time.Duration) (bool, error) {
	// A slot taken is taken for any gap but none.
	return !f.slotTaken || gap == 0, nil
}

// A GIF of a topic is sent; when none is found, the asker is told so, as a
// topic has no sticker to fall back on.
func TestGIFOfATopic(t *testing.T) {
	for name, c := range map[string]struct {
		gifs *fakeGIFs
		want int
		log  string
	}{
		"sent":       {&fakeGIFs{links: []string{"https://g/1.mp4"}}, 1, "<gif pubg>"},
		"none found": {&fakeGIFs{}, 0, textNoGIF.uz},
		// Asked for, it does not wait out the gap after the last sticker.
		"right after a sticker": {&fakeGIFs{links: []string{"https://g/1.mp4"}, slotTaken: true}, 1, "<gif pubg>"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			st := withStickers(f)
			f.opts.GIFs, f.opts.AnimationSender, f.opts.MediaSlots = c.gifs, c.gifs, c.gifs
			f.replier.gif = "pubg"
			sent := f.run(inGroup(groupMessage("Mira, pubg haqida gif tasha")))
			if c.want == 0 && (len(sent) != 1 || sent[0].text != textNoGIF.uz) {
				t.Errorf("texts = %+v, want the asker told no GIF was found", sent)
			}
			if len(c.gifs.sent) != c.want || len(st.sent) != 0 {
				t.Errorf("GIFs %d, stickers %q; want %d GIFs and no sticker", len(c.gifs.sent), st.sent, c.want)
			}
			log := strings.Join(f.chatLog.texts(groupID), "\n")
			if c.log != "" && !strings.Contains(log, c.log) || c.log == "" && strings.Contains(log, "<gif") {
				t.Errorf("log = %q, want %q", log, c.log)
			}
		})
	}
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
			f.replier.gif = "facepalm"
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

type fakeAudioSender struct {
	mu   sync.Mutex
	fail bool
	sent []string
}

func (f *fakeAudioSender) SendAudio(_ context.Context, track telegram.Track) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return 0, errors.New("telegram is down")
	}
	f.sent = append(f.sent, track.Performer+" — "+track.Title+" "+string(track.Data))
	return 900, nil
}

// A song the music tool found is the answer alone: the words that came with
// it are not sent, and the log notes only the song. When it cannot be sent
// the words go instead.
func TestSongIsTheAnswerAlone(t *testing.T) {
	for name, fail := range map[string]bool{"sent": false, "failed": true} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			audio := &fakeAudioSender{fail: fail}
			f.opts.AudioSender = audio
			f.replier.answer = "Mana, Shohruhxonning «Yurak» qo'shig'i"
			f.replier.audios = []tools.Audio{{Data: []byte("MP3"), Title: "Yurak", Performer: "Shohruhxon"}}

			sent := f.run(inGroup(groupMessage("Mira, Shohruhxon Yurak qo'shig'ini top")))

			log := strings.Join(f.chatLog.texts(groupID), "\n")
			if !fail && (len(sent) != 0 || len(audio.sent) != 1 || audio.sent[0] != "Shohruhxon — Yurak MP3" || !strings.Contains(log, "<audio Shohruhxon — Yurak>") || strings.Contains(log, "Mana")) {
				t.Errorf("texts %+v, audio %q, log %q", sent, audio.sent, log)
			}
			if fail && (len(sent) != 1 || strings.Contains(log, "<audio")) {
				t.Errorf("texts %+v, log %q", sent, log)
			}
		})
	}
}

// A sticker that was the whole answer and is held back by the gap becomes
// a reaction of its mood; one asked for is not held back.
func TestStickerWithinTheGap(t *testing.T) {
	for name, c := range map[string]struct {
		text     string
		stickers int
		reaction string
	}{
		"unasked":   {"Mira, haha", 0, "🤣"},
		"asked for": {"Mira, endi stik", 1, ""},
		"stikerini": {"Mira, stikerini tashla", 1, ""},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			st := withStickers(f)
			st.busy = true
			f.replier.sticker = "funny"
			f.run(inGroup(groupMessage(c.text)))
			reactions := f.reactor.recorded()
			if len(st.sent) != c.stickers || (c.reaction == "") != (len(reactions) == 0) || (c.reaction != "" && reactions[0].emoji != c.reaction) {
				t.Errorf("stickers %q, reactions %+v", st.sent, reactions)
			}
		})
	}
}

// Every mood has a reaction, and Telegram takes each.
func TestMoodReactions(t *testing.T) {
	for _, mood := range stickers.Categories {
		if emoji, ok := moodReactions[mood]; !ok || !telegram.IsReactionEmoji(emoji) {
			t.Errorf("%s: %q is not a reaction Telegram takes", mood, emoji)
		}
	}
}

// A song found during a turn is sent even when the model, told the song is
// the answer, wrote nothing: "into you x bye mashup mari" got no answer at
// all, the song it found included.
func TestSongIsSentAfterAnEmptyAnswer(t *testing.T) {
	f := newFixture()
	audio := &fakeAudioSender{}
	f.opts.AudioSender = audio
	f.replier.err = fmt.Errorf("agent: %w", ai.ErrEmptyResponse)
	f.replier.audios = []tools.Audio{{Data: []byte("MP3"), Title: "Into You x Bye", Performer: "VellyCarl"}}

	sent := f.run(inGroup(groupMessage("into you x bye mashup mira")))

	if len(sent) != 0 || len(audio.sent) != 1 || audio.sent[0] != "VellyCarl — Into You x Bye MP3" {
		t.Errorf("texts %+v, audio %q", sent, audio.sent)
	}
}
