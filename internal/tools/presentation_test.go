package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func (f *fakeQuota) ReservePresentation(ctx context.Context, userID, chatID int64, since time.Time, limit int) (int64, bool, error) {
	return f.ReserveImage(ctx, userID, chatID, since, limit)
}

func (f *fakeQuota) ReleasePresentation(ctx context.Context, id int64) error {
	return f.ReleaseImage(ctx, id)
}

func presentationRegistry(t *testing.T, quota *fakeQuota) *Registry {
	t.Helper()
	return mustRegistry(t, PresentationTool(PresentationOptions{
		Quota:      quota,
		DailyLimit: 3,
		Location:   tashkent,
		Now:        func() time.Time { return time.Date(2026, 10, 23, 15, 0, 0, 0, tashkent) },
	}))
}

const goodDeck = `{"title":"Suv resurslari","subtitle":"Vali Aliyev","theme":"green","caption":"Mana taqdimotingiz 📊",
	"slides":[{"title":"Muammo","bullets":["Orol qisqardi","Yerlar sho'rlangan"]},{"title":"Yechim","bullets":["  Tomchilatib   sug'orish ","",  "Subsidiya"]}]}`

func makeDeck(registry *Registry, ctx context.Context, args string) envelope {
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, userID, "make_presentation", args)), &env)
	return env
}

func TestPresentationIsSentAsTheWholeAnswer(t *testing.T) {
	quota := &fakeQuota{}
	sink := &Sink{}
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: -100}, sink)

	env := makeDeck(presentationRegistry(t, quota), ctx, goodDeck)

	if !env.OK {
		t.Fatalf("env = %+v", env)
	}
	documents := sink.TakeDocuments()
	if len(documents) != 1 || documents[0].Name != "Suv resurslari.pptx" {
		t.Fatalf("documents = %+v", documents)
	}
	archive, err := zip.NewReader(bytes.NewReader(documents[0].Data), int64(len(documents[0].Data)))
	if err != nil || len(archive.File) == 0 {
		t.Fatalf("not a pptx: %v", err)
	}
	if text, done := sink.Finished(); !done || text != "Mana taqdimotingiz 📊" {
		t.Errorf("finished = %q, %v", text, done)
	}
	if len(quota.limits) != 1 || quota.limits[0] != 3 || !quota.since.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, tashkent)) {
		t.Errorf("quota = %+v", quota)
	}
}

func TestPresentationLimits(t *testing.T) {
	registry := presentationRegistry(t, &fakeQuota{refuse: true})
	env := makeDeck(registry, WithTurn(context.Background(), Caller{UserID: userID}, &Sink{}), goodDeck)
	if env.OK || !strings.Contains(env.Error.Message, "3 presentations") {
		t.Errorf("over the limit = %+v", env)
	}

	owners := &fakeQuota{}
	makeDeck(presentationRegistry(t, owners), WithTurn(context.Background(), Caller{UserID: userID, Owner: true}, &Sink{}), goodDeck)
	if len(owners.limits) != 1 || owners.limits[0] != -1 {
		t.Errorf("owner's limit = %v, want none", owners.limits)
	}
}

// A deck that would not fit is sent back to the model to fix, and no
// presentation of the day is used for it.
func TestPresentationChecksTheSlides(t *testing.T) {
	quota := &fakeQuota{}
	registry := presentationRegistry(t, quota)
	ctx := WithTurn(context.Background(), Caller{UserID: userID}, &Sink{})
	for name, tc := range map[string]struct{ args, word string }{
		"one slide":   {`{"title":"T","caption":"c","slides":[{"title":"a","bullets":["b"]}]}`, "2 to 12"},
		"no title":    {`{"title":" ","caption":"c","slides":[{"title":"a","bullets":["b"]},{"title":"a","bullets":["b"]}]}`, "title"},
		"no bullets":  {`{"title":"T","caption":"c","slides":[{"title":"a","bullets":["b"]},{"title":"a","bullets":[" "]}]}`, "slide 2"},
		"long bullet": {`{"title":"T","caption":"c","slides":[{"title":"a","bullets":["b"]},{"title":"a","bullets":["` + strings.Repeat("x", 221) + `"]}]}`, "shorten"},
		"no caption":  {`{"title":"T","caption":"","slides":[{"title":"a","bullets":["b"]},{"title":"a","bullets":["b"]}]}`, "caption"},
	} {
		if env := makeDeck(registry, ctx, tc.args); env.OK || env.Error.Code != "invalid_arguments" || !strings.Contains(env.Error.Message, tc.word) {
			t.Errorf("%s: env = %+v", name, env)
		}
	}
	if len(quota.limits) != 0 {
		t.Errorf("a refused deck used the quota: %v", quota.limits)
	}
}
