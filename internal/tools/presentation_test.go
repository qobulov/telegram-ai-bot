package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

// goodDeck is ten slides after the title, one of each layout and more
// points.
var goodSlides = []string{
	`{"layout":"points","eyebrow":"Kirish","title":"Muammo","intro":"Suv tanqisligi o'sib bormoqda.","icon":"💧","items":[{"title":"Orol","text":"90% qisqardi"},{"title":"Yerlar","text":"sho'rlangan"}]}`,
	`{"layout":"cards","title":"Asoslar","items":[{"title":"Qonun","text":"Suv to'g'risida"},{"title":"Nizom","text":"Vazirlik"}]}`,
	`{"layout":"steps","title":"Yechim","items":[{"title":"Reja"},{"title":"Ish"},{"title":"Baholash","text":"natija"}]}`,
	`{"layout":"grid","title":"Usullar","items":[{"icon":"🌱","title":"Tomchi"},{"title":"Yomg'irlatish"},{"title":"Qayta ishlash"},{"title":"Nazorat"}]}`,
	`{"layout":"table","title":"Hujjatlar","columns":["Tur","Vazifa"],"items":[{"title":"Reja","text":"yillik"},{"title":"Hisobot","text":"choraklik"}]}`,
	`{"layout":"statement","title":"G'oya","intro":"Suvni tejash — kelajakni asrash."}`,
	`{"layout":"points","title":"Fermerlar","items":[{"text":"subsidiya"},{"text":"o'qitish"}]}`,
	`{"layout":"cards","title":"Natijalar","items":[{"title":"Hosil","text":"+20%"},{"title":"Suv","text":"-40%"}]}`,
	`{"layout":"statement","title":"Xulosa","intro":"Har bir tomchi muhim.","items":[{"text":"Iqtisodiyot fani"}]}`,
	`{"layout":"numbers","title":"Raqamlar","items":[{"title":"90%","text":"Orol qisqardi"},{"title":"3 km³","text":"yillik tanqislik"}]}`,
	`{"layout":"split","eyebrow":"Yakun","title":"Savollar","intro":"Rahmat!","items":[{"text":"Savol bering"},{"text":"Fikr bildiring"}]}`,
}

// deckOfSlides is a deck's arguments with these slides.
func deckOfSlides(slides []string) string {
	return `{"title":"Suv resurslari","subtitle":"Iqtisodiyot fanidan","author":"Bajardi: Vali Aliyev","theme":"green","caption":"Mana taqdimotingiz 📊","slides":[` + strings.Join(slides, ",") + `]}`
}

var goodDeck = deckOfSlides(goodSlides)

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
	if len(documents) != 1 || documents[0].Name != "Suv resurslari.pptx" || !strings.Contains(string(env.Result), `"slides":12`) {
		t.Fatalf("%d documents, env %s", len(documents), env.Result)
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

// withSlides is goodDeck with its first slide replaced.
func withSlides(first string) string {
	return deckOfSlides(append([]string{first}, goodSlides[1:]...))
}

// A deck that would not fit is sent back to the model to fix, and no
// presentation of the day is used for it.
func TestPresentationChecksTheSlides(t *testing.T) {
	quota := &fakeQuota{}
	registry := presentationRegistry(t, quota)
	ctx := WithTurn(context.Background(), Caller{UserID: userID}, &Sink{})
	short := `{"title":"T","caption":"c","slides":[{"layout":"points","title":"a","items":[{"text":"b"},{"text":"c"}]}]}`
	for name, tc := range map[string]struct{ args, word string }{
		"too few slides":   {short, "9 to 14"},
		"unknown layout":   {withSlides(`{"layout":"chart","title":"a","items":[{"text":"b"},{"text":"c"}]}`), "layout must be"},
		"too many cards":   {withSlides(`{"layout":"cards","title":"a","items":[{"text":"1"},{"text":"2"},{"text":"3"},{"text":"4"},{"text":"5"}]}`), "2 to 4 items"},
		"empty statement":  {withSlides(`{"layout":"statement","title":"a"}`), "statement in intro"},
		"half a table row": {withSlides(`{"layout":"table","title":"a","items":[{"title":"x"},{"title":"y","text":"z"}]}`), "title and a text"},
		"split one point":  {withSlides(`{"layout":"split","title":"a","items":[{"text":"b"}]}`), "2 to 4 items"},
		"no caption":       {strings.Replace(goodDeck, `"caption":"Mana taqdimotingiz 📊"`, `"caption":""`, 1), "caption"},
	} {
		if env := makeDeck(registry, ctx, tc.args); env.OK || env.Error.Code != "invalid_arguments" || !strings.Contains(env.Error.Message, tc.word) {
			t.Errorf("%s: env = %+v", name, env)
		}
	}
	if len(quota.limits) != 0 {
		t.Errorf("a refused deck used the quota: %v", quota.limits)
	}
}

// A step's own number is dropped: the slide numbers its steps.
func TestStepsLoseTheirOwnNumbers(t *testing.T) {
	slide, err := slideOf(slideArgs{Layout: "steps", Title: "Bosqichlar", Items: []itemArgs{{Title: "1. Murojaat"}, {Title: "2) Tahlil"}, {Title: "2026-yil rejasi"}, {Title: "3-bosqich: Betonlash"}}})
	if err != nil || slide.Items[0].Title != "Murojaat" || slide.Items[1].Title != "Tahlil" || slide.Items[2].Title != "2026-yil rejasi" || slide.Items[3].Title != "Betonlash" {
		t.Errorf("items = %+v, %v", slide.Items, err)
	}
}

// PRESENTATION_JSON, when set, is make_presentation's arguments as a model
// wrote them, built into PRESENTATION_OUT to look at.
func TestBuildFromModelArguments(t *testing.T) {
	in, out := os.Getenv("PRESENTATION_JSON"), os.Getenv("PRESENTATION_OUT")
	if in == "" {
		t.Skip("no PRESENTATION_JSON")
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	sink := &Sink{}
	if env := makeDeck(presentationRegistry(t, &fakeQuota{}), WithTurn(context.Background(), Caller{UserID: userID}, sink), string(raw)); !env.OK {
		t.Fatalf("env = %+v", env)
	}
	if err := os.WriteFile(out, sink.TakeDocuments()[0].Data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Text a little too long is cut at a word rather than sent back, and a
// figure too long to show large makes the slide points.
func TestLongTextIsCutNotRefused(t *testing.T) {
	long := strings.Repeat("so'z ", 70)
	slide, err := slideOf(slideArgs{Layout: "numbers", Title: "Raqamlar", Intro: long,
		Items: []itemArgs{{Title: "bir yuz yigirma ming", Text: long}, {Title: "2", Text: "y"}}})
	if err != nil {
		t.Fatal(err)
	}
	if slide.Layout != "points" || !strings.HasSuffix(slide.Intro, "…") || utf8.RuneCountInString(slide.Items[0].Text) > 240 || strings.HasSuffix(slide.Items[0].Text, " …") {
		t.Errorf("slide = %+v", slide)
	}
	if got := cut("qisqa", 10); got != "qisqa" {
		t.Errorf("cut = %q", got)
	}
}
