package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

// searchedGIFs records what GIPHY was asked.
type searchedGIFs struct {
	fakeGIFs
	smu      sync.Mutex
	searched []string
}

func (f *searchedGIFs) Search(_ context.Context, subject string) ([]string, error) {
	f.smu.Lock()
	defer f.smu.Unlock()
	f.searched = append(f.searched, subject)
	return f.links, nil
}

func (f *searchedGIFs) ClaimMediaSlot(context.Context, int64, time.Duration) (bool, error) {
	return true, nil
}

// "Мари гиф" and its kind are answered with a GIF and no model, and take
// nothing from the group's quota; a topic is searched in English, and one
// only the model can put in English is left to it.
func TestGIFRequestsWithoutTheModel(t *testing.T) {
	for text, want := range map[string]string{
		"Мари гиф кулги":          "funny",
		"Maria gif pubg":          "pubg",
		"Мари, гиф кот ташла":     "cat",
		"мари, gif naruto please": "naruto",
	} {
		f := newFixture()
		f.opts.WakeWords = []string{"мари", "maria"}
		withStickers(f)
		quota := &fakeGroupQuota{}
		f.opts.GroupReplies = quota
		gifs := &searchedGIFs{fakeGIFs: fakeGIFs{links: []string{"https://g/1.mp4"}}}
		f.opts.GIFs, f.opts.AnimationSender, f.opts.MediaSlots = gifs, gifs, gifs
		f.run(inGroup(groupMessage(text)))
		if len(gifs.searched) != 1 || gifs.searched[0] != want || len(gifs.sent) != 1 || len(f.replier.recorded()) != 0 || len(quota.asked()) != 0 {
			t.Errorf("%q: searched %v, sent %d, model asked %d, quota asked %d", text, gifs.searched, len(gifs.sent), len(f.replier.recorded()), len(quota.asked()))
		}
	}
	f := newFixture()
	f.opts.WakeWords = []string{"мари"}
	gifs := &searchedGIFs{fakeGIFs: fakeGIFs{links: []string{"https://g/1.mp4"}}}
	f.opts.GIFs, f.opts.AnimationSender, f.opts.MediaSlots = gifs, gifs, gifs
	f.run(inGroup(groupMessage("мари гиф бахтли ҳаёт")))
	if len(f.replier.recorded()) != 1 {
		t.Errorf("a topic only the model can translate: model asked %d times", len(f.replier.recorded()))
	}
	f = newFixture()
	f.opts.WakeWords = []string{"мари"}
	gifs = &searchedGIFs{fakeGIFs: fakeGIFs{links: []string{"https://g/1.mp4"}}}
	f.opts.GIFs, f.opts.AnimationSender, f.opts.MediaSlots = gifs, gifs, gifs
	f.run(inGroup(groupMessage("мари гиф")))
	if len(gifs.searched) != 1 || len(gifs.sent) != 1 || len(f.replier.recorded()) != 0 {
		t.Errorf("no topic: searched %v, model asked %d", gifs.searched, len(f.replier.recorded()))
	}
}
