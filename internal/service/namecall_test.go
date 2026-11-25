package service

import (
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// A message of nothing but the bot's name gets a short line with no model
// asked, saved, not recorded as a model's turn; a name with words, a call
// replying to a message, or a call with words after it, goes to the model.
func TestBareNameIsAnsweredWithoutTheModel(t *testing.T) {
	for _, text := range []string{"Mira", "mira?", "@MyBot", "Mira!!"} {
		f := newFixture()
		msg := groupMessage(text)
		if strings.HasPrefix(text, "@") {
			msg = groupMessage(text, text)
		}
		sent := f.run(inGroup(msg))
		recorded := f.replier.recorded()
		if len(sent) != 1 || strings.HasPrefix(sent[0].text, "echo:") || len(recorded) != 0 {
			t.Errorf("%q: sent %+v, recorded %+v", text, sent, recorded)
		}
	}
	for _, msg := range []*telegram.Message{groupMessage("Mira, qalaysan"), groupMessage("Mira 😂"), func() *telegram.Message {
		m := groupMessage("Mira")
		m.ReplyToMessage = &telegram.Message{MessageID: 7, From: &telegram.User{ID: 4242, FirstName: "Ann"}, Text: "kim bor?", Chat: m.Chat}
		return m
	}()} {
		f := newFixture()
		f.run(inGroup(msg))
		if recorded := f.replier.recorded(); len(recorded) != 1 || recorded[0].req.Fixed != nil {
			t.Errorf("%q: recorded %+v, want the model", msg.Text, recorded)
		}
	}
}

// A member's call name goes into the line after its first words.
func TestWithCallName(t *testing.T) {
	for _, c := range [][3]string{
		{"Labbay?", "qirolicham", "Labbay, qirolicham?"},
		{"Ha, nima bo'ldi?", "qirolicham", "Ha, qirolicham, nima bo'ldi?"},
		{"Eshitaman.", "", "Eshitaman."},
		{"Слушаю.", "Ustoz", "Слушаю, Ustoz."},
	} {
		if got := withCallName(c[0], c[1]); got != c[2] {
			t.Errorf("withCallName(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

// A bare name call in a group is answered in Uzbek, in Russian only for a
// Cyrillic name from a Russian app, whatever other app the member uses.
func TestBareNameLanguage(t *testing.T) {
	f := newFixture()
	msg := groupMessage("Mira")
	msg.From.LanguageCode = "en"
	sent := f.run(inGroup(msg))
	english := map[string]bool{}
	for _, l := range nameCallLines {
		english[l.en] = true
	}
	if len(sent) != 1 || english[sent[0].text] {
		t.Errorf("sent %+v, want Uzbek", sent)
	}
}
