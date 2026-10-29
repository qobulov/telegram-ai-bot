package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/telegram"
)

// --- Albums -----------------------------------------------------------------

// Several pictures go as one album, replying to the request, with the
// answer as its caption.
func TestPicturesGoAsOneAlbum(t *testing.T) {
	f := newFixture()
	f.replier.draws = 3

	sent := f.run(inGroup(groupMessage("Mira, draw three cats")))

	if len(sent) != 1 || sent[0].album != 3 || sent[0].text != "echo: draw three cats" || sent[0].replyTo != 30 {
		t.Fatalf("sent = %+v, want one album of 3 with the answer as caption", sent)
	}
	if got := f.recorder.calls; len(got) != 1 || got[0].messageID != 1001 {
		t.Errorf("recorded = %+v, want the album's first message", got)
	}
	if log := f.chatLog.texts(groupID); len(log) != 2 || log[1] != "You: <photo> echo: draw three cats" {
		t.Errorf("log = %q", log)
	}
}

// Telegram takes ten pictures an album: more go in another, and the
// caption only under the first.
func TestMoreThanTenPicturesGoInSeveralAlbums(t *testing.T) {
	f := newFixture()
	f.replier.draws = 12

	sent := f.run(textFrom(userID, "draw"))

	if len(sent) != 2 || sent[0].album != 10 || sent[0].text != "echo: draw" || sent[1].album != 2 || sent[1].text != "" {
		t.Errorf("sent = %+v, want albums of 10 and 2, the caption on the first", sent)
	}
}

// An album Telegram refuses goes picture by picture; one it did not answer
// may be in the chat, and is not sent again.
func TestRefusedAlbumGoesPictureByPicture(t *testing.T) {
	f := newFixture()
	f.replier.draws = 2
	f.sender.albumErr = &telegram.APIError{Code: 400, Description: "Bad Request: IMAGE_PROCESS_FAILED"}

	sent := f.run(textFrom(userID, "draw"))

	if len(sent) != 2 || !sent[0].photo || sent[0].album != 0 || sent[0].text != "echo: draw" || sent[1].text != "" {
		t.Errorf("sent = %+v, want two photos, the caption on the first", sent)
	}

	f = newFixture()
	f.replier.draws = 2
	f.sender.albumErr = errors.New("connection reset")
	if sent := f.run(textFrom(userID, "draw")); len(sent) != 0 {
		t.Errorf("sent = %+v after an unanswered album, want nothing more", sent)
	}
}

// --- Voice answers ----------------------------------------------------------

type fakeSpeaker struct {
	mu    sync.Mutex
	texts []string
	err   error
}

func (f *fakeSpeaker) Speak(_ context.Context, text string) (ai.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
	if f.err != nil {
		return ai.Speech{}, f.err
	}
	return ai.Speech{Data: []byte("OggS"), Format: "ogg"}, nil
}

func (f *fakeSpeaker) spoken() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.texts...)
}

type fakeVoiceSender struct {
	mu     sync.Mutex
	voices []telegram.VoiceNote
	err    error
}

func (f *fakeVoiceSender) SendVoice(_ context.Context, voice telegram.VoiceNote) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	f.voices = append(f.voices, voice)
	return 4000, nil
}

func (f *fakeVoiceSender) sent() []telegram.VoiceNote {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]telegram.VoiceNote(nil), f.voices...)
}

func withVoice(f *fixture) (*fakeSpeaker, *fakeVoiceSender) {
	speaker, voices := &fakeSpeaker{}, &fakeVoiceSender{}
	f.opts.Speaker, f.opts.VoiceSender = speaker, voices
	return speaker, voices
}

// Asked to answer by voice, the bot tells the model so and sends its
// answer read aloud instead of as text, recording that message.
func TestVoiceAnswerIsSentReadAloud(t *testing.T) {
	f := newFixture()
	speaker, voices := withVoice(f)
	f.replier.answer = "Salom, yaxshiman!"

	sent := f.run(textFrom(userID, "ovozli javob ber, qalaysan"))

	if len(sent) != 0 {
		t.Errorf("text sent = %+v, want none", sent)
	}
	if got := voices.sent(); len(got) != 1 || got[0].ChatID != userID || got[0].Format != "ogg" || string(got[0].Data) != "OggS" {
		t.Errorf("voices = %+v", got)
	}
	if got := speaker.spoken(); len(got) != 1 || got[0] != "Salom, yaxshiman!" {
		t.Errorf("spoken = %q", got)
	}
	if req := f.replier.recorded()[0].req; !strings.Contains(req.Context, voiceNote) {
		t.Errorf("context = %q, want the voice note", req.Context)
	}
	if got := f.recorder.calls; len(got) != 1 || got[0].messageID != 4000 {
		t.Errorf("recorded = %+v, want the voice message", got)
	}
	if actions := f.chatActor.recorded(); !strings.Contains(strings.TrimSpace(actionsOf(actions)), "record_voice") {
		t.Errorf("chat actions = %+v, want record_voice", actions)
	}
}

func actionsOf(actions []chatAction) string {
	var names []string
	for _, a := range actions {
		names = append(names, a.action)
	}
	return strings.Join(names, " ")
}

// In a group the voice answer replies to the request, and the log says it
// was spoken.
func TestVoiceAnswerInAGroup(t *testing.T) {
	f := newFixture()
	_, voices := withVoice(f)

	f.run(inGroup(groupMessage("Mira, голосом ответь: привет")))

	if got := voices.sent(); len(got) != 1 || got[0].ChatID != groupID || got[0].ReplyToMessageID != 30 {
		t.Fatalf("voices = %+v", got)
	}
	if log := f.chatLog.texts(groupID); len(log) != 2 || !strings.HasPrefix(log[1], "You: <voice> echo:") {
		t.Errorf("log = %q", log)
	}
}

// Whatever keeps the answer from being heard, the text goes instead: the
// speech failing, Telegram refusing it, or an answer too long to read.
func TestVoiceAnswerFallsBackToText(t *testing.T) {
	cases := map[string]func(f *fixture, speaker *fakeSpeaker, voices *fakeVoiceSender){
		"speech failed": func(_ *fixture, speaker *fakeSpeaker, _ *fakeVoiceSender) {
			speaker.err = errors.New("404 no /audio/speech")
		},
		"refused":         func(_ *fixture, _ *fakeSpeaker, voices *fakeVoiceSender) { voices.err = &telegram.APIError{Code: 400} },
		"too long":        func(f *fixture, _ *fakeSpeaker, _ *fakeVoiceSender) { f.replier.answer = strings.Repeat("so'z ", 200) },
		"nobody asked":    nil,
		"no speech model": nil,
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.MaxInputChars = 100
			speaker, voices := withVoice(f)
			ask := "ovoz bilan ayt: salom"
			switch name {
			case "nobody asked":
				ask = "salom, qalaysan"
			case "no speech model":
				f.opts.Speaker = nil
			}
			if setup != nil {
				setup(f, speaker, voices)
			}

			sent := f.run(textFrom(userID, ask))

			if len(sent) != 1 || sent[0].text == "" || len(voices.sent()) != 0 {
				t.Errorf("sent = %+v, voices = %d; want the text alone", sent, len(voices.sent()))
			}
			asked := strings.Contains(f.replier.recorded()[0].req.Context, voiceNote)
			if want := name != "nobody asked" && name != "no speech model"; asked != want {
				t.Errorf("voice note given = %t, want %t", asked, want)
			}
			if name == "too long" && len(speaker.spoken()) != 0 {
				t.Error("an answer too long was read aloud")
			}
		})
	}
}

func TestWantsVoice(t *testing.T) {
	cases := map[string]bool{
		"ovozli javob ber":              true,
		"Mari, ovoz bilan ayt qalaysan": true,
		"ovozli qilib javob ber":        true,
		"javob ber ovozli":              true,
		"ovozingiz bilan gapiring":      true,
		"ovozli xabar yubor":            true,
		"голосом ответь":                true,
		"скажи голосом, как дела":       true,
		"отправь голосовое":             true,
		"озвучь это":                    true,
		"send me a voice message":       true,
		"reply by voice please":         true,
		"ovozli xabarni eshit":          false,
		"salom, qalaysan":               false,
		"сделай голосование и отправь всем": false,
		"his voice is nice": false,
	}
	for words, want := range cases {
		if got := wantsVoice(words); got != want {
			t.Errorf("wantsVoice(%q) = %t, want %t", words, got, want)
		}
	}
}

// --- Inline mode ------------------------------------------------------------

type fakeInline struct {
	mu      sync.Mutex
	answers []telegram.InlineAnswer
}

func (f *fakeInline) AnswerInlineQuery(_ context.Context, answer telegram.InlineAnswer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, answer)
	return nil
}

func (f *fakeInline) recorded() []telegram.InlineAnswer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]telegram.InlineAnswer(nil), f.answers...)
}

func inlineQuery(fromID int64, query string) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, InlineQuery: &telegram.InlineQuery{
		ID: "q" + query, From: &telegram.User{ID: fromID, FirstName: "Ann"}, Query: query, ChatType: "supergroup",
	}}
}

// askInline answers each query as the webhook does, and returns the one
// article of each answer.
func (f *fixture) askInline(updates ...telegram.Update) (*fakeInline, []telegram.InlineArticle) {
	inline := &fakeInline{}
	f.opts.Inline = inline
	s := New(f.opts)
	for _, u := range updates {
		s.HandleNow(u)
	}
	s.stop()
	var articles []telegram.InlineArticle
	for _, a := range inline.recorded() {
		articles = append(articles, a.Articles...)
	}
	return inline, articles
}

// A finished question gets Maria's answer, as a private message would, but
// as an inline turn: no history, nothing sent to any chat.
func TestInlineQuestionIsAnswered(t *testing.T) {
	f := newFixture()

	inline, articles := f.askInline(inlineQuery(userID, " Toshkent qayerda? "))

	answers := inline.recorded()
	if len(answers) != 1 || answers[0].QueryID != "q Toshkent qayerda? " || answers[0].CacheSeconds != inlineAnswerCache {
		t.Fatalf("answers = %+v", answers)
	}
	if len(articles) != 1 || articles[0].ID != "answer" || articles[0].Title != "Mira" || articles[0].Text != "echo: Toshkent qayerda?" {
		t.Errorf("article = %+v", articles)
	}
	req := f.replier.recorded()[0].req
	if !req.Inline || req.Scope.ChatID != userID || req.Scope.UserID != userID || req.Text != "Toshkent qayerda?" || !strings.Contains(req.Context, "[Answer in") {
		t.Errorf("request = %+v", req)
	}
	if sent := f.sender.messages(); len(sent) != 0 {
		t.Errorf("sent = %+v, want nothing in any chat", sent)
	}
}

// An empty or unfinished question gets a hint, and no model call: the app
// sends a query at nearly every keystroke.
func TestUnfinishedInlineQuestionGetsAHint(t *testing.T) {
	f := newFixture()

	_, articles := f.askInline(inlineQuery(userID, ""), inlineQuery(userID, "Toshkent qayer"))

	if len(articles) != 2 || articles[0].ID != "hint" || articles[1].ID != "hint" || !strings.Contains(articles[0].Text, "@"+botUsername) {
		t.Errorf("articles = %+v", articles)
	}
	if calls := f.replier.recorded(); len(calls) != 0 {
		t.Errorf("the model was asked %d times", len(calls))
	}
}

// Inline answers count against the private chats' daily answers; past them
// the article says so, and owners are not counted.
func TestInlineQuestionsCountAgainstTheDailyLimit(t *testing.T) {
	f := newFixture()
	f.opts.OwnerIDs = map[int64]bool{9001: true}
	quota := withPrivateLimit(f, 1)

	_, articles := f.askInline(inlineQuery(userID, "bir?"), inlineQuery(userID, "ikki?"), inlineQuery(9001, "uch?"))

	if len(articles) != 3 || articles[0].ID != "answer" || articles[1].ID != "notice" || !strings.Contains(articles[1].Text, "1 ta") || articles[2].ID != "answer" {
		t.Errorf("articles = %+v", articles)
	}
	if asked, _ := quota.recorded(); len(asked) != 2 {
		t.Errorf("quota asked %d times, want twice: not for the owner", len(asked))
	}
	if calls := f.replier.recorded(); len(calls) != 2 {
		t.Errorf("the model was asked %d times, want twice", len(calls))
	}
}

// A question needing a tool, or one not answered in time, gets a notice,
// not cached, and its answer back.
func TestInlineFailuresGiveTheAnswerBack(t *testing.T) {
	cases := map[string]struct {
		setup func(f *fixture)
		want  string
	}{
		"needs tools": {func(f *fixture) { f.replier.err = agent.ErrNeedsTools }, textInlineNeedsTools.uz},
		"too slow":    {func(f *fixture) { f.replier.waitForDeadline = true }, textInlineRetry.uz},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			quota := withPrivateLimit(f, 5)
			c.setup(f)
			f.opts.Inline = &fakeInline{}
			s := New(f.opts)
			s.inlineDeadline = 50 * time.Millisecond
			s.HandleNow(inlineQuery(userID, "Toshkentda havo qanday?"))
			s.stop()

			answers := f.opts.Inline.(*fakeInline).recorded()
			if len(answers) != 1 || answers[0].CacheSeconds != 0 || answers[0].Articles[0].Text != c.want {
				t.Errorf("answers = %+v", answers)
			}
			if _, released := quota.recorded(); len(released) != 1 {
				t.Errorf("released = %v, want the answer given back", released)
			}
		})
	}
}

// With private chats closed, inline questions are not answered either.
func TestInlineFollowsPrivateChats(t *testing.T) {
	f := newFixture()
	f.opts.PrivateChats = "off"

	_, articles := f.askInline(inlineQuery(userID, "salom?"))

	if len(articles) != 1 || articles[0].Text != textInlineClosed.uz || len(f.replier.recorded()) != 0 {
		t.Errorf("articles = %+v", articles)
	}
}

// While polling, an inline question is answered off the polling loop.
func TestInlineQuestionWhilePolling(t *testing.T) {
	f := newFixture()
	inline := &fakeInline{}
	f.opts.Inline = inline

	f.run(inlineQuery(userID, "salom!"))

	if answers := inline.recorded(); len(answers) != 1 || answers[0].Articles[0].ID != "answer" {
		t.Errorf("answers = %+v", answers)
	}
}
