package service

import (
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// A short praise replying to the bot, or calling it, is answered with one
// of its GIFs without the model; a longer message, a thanks alone or praise
// of someone else goes to the model.
func TestShortPraiseGetsTheBotsGIF(t *testing.T) {
	toBot := func(text string) *telegram.Message {
		m := groupMessage(text)
		m.ReplyToMessage = &telegram.Message{MessageID: 9, From: &telegram.User{ID: botID, IsBot: true}, Text: "hazil", Chat: m.Chat}
		return m
	}
	for _, text := range []string{"Молодец", "zo'rsan", "Apa siz eng zo'risiz 👀", "good bot", "умница!"} {
		f := newFixture()
		f.opts.PraiseGIFs = []string{"https://site/gifs/a.mp4"}
		f.run(inGroup(toBot(text)))
		if recorded := f.replier.recorded(); len(recorded) != 0 {
			t.Errorf("%q went to the model", text)
		}
	}
	for _, text := range []string{"zo'r gap aytding, endi menga kino top", "rahmat", "bot"} {
		f := newFixture()
		f.opts.PraiseGIFs = []string{"https://site/gifs/a.mp4"}
		f.run(inGroup(toBot(text)))
		if recorded := f.replier.recorded(); len(recorded) != 1 {
			t.Errorf("%q did not go to the model", text)
		}
	}
}
