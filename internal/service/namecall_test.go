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
