package service

import (
	"slices"
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// /start and /help bring the keyboard to a private chat, in the app's
// language, with the meme button only where memes can be made; other
// answers leave the keyboard as it is.
func TestStartAndHelpBringTheKeyboard(t *testing.T) {
	f := newFixture()
	f.opts.MemesEnabled = true
	russian := textFrom(userID, "/help")
	russian.Message.From.LanguageCode = "ru"

	sent := f.run(textFrom(userID, "/start"), russian, textFrom(userID, "/about"), textFrom(userID, "salom, qalaysan"))

	if len(sent) != 4 {
		t.Fatalf("sent = %+v", sent)
	}
	if got := sent[0].keyboard; len(got) != 3 || got[0][0] != "😂 Mem yasab ber" || got[2][1] != "❓ Yordam" {
		t.Errorf("/start keyboard = %q", got)
	}
	if got := sent[1].keyboard; len(got) != 3 || got[2][1] != "❓ Помощь" {
		t.Errorf("Russian /help keyboard = %q", got)
	}
	if sent[2].keyboard != nil || sent[3].keyboard != nil {
		t.Errorf("/about and an answer carry a keyboard: %q, %q", sent[2].keyboard, sent[3].keyboard)
	}

	f = newFixture()
	sent = f.run(textFrom(userID, "/start"))
	if got := sent[0].keyboard; len(got) != 3 || len(got[0]) != 1 || got[0][0] != "🎲 Zar tashla" {
		t.Errorf("keyboard without memes = %q", got)
	}
}

// A group never gets the keyboard, not even with /help.
func TestGroupsGetNoKeyboard(t *testing.T) {
	f := newFixture()
	sent := f.run(inGroup(groupMessage("/help@MyBot")))
	if len(sent) != 1 || sent[0].keyboard != nil {
		t.Errorf("sent = %+v", sent)
	}
}

// The help button is /help, in any language; any other button is a request
// like a typed one, for the model.
func TestKeyboardButtonsAreAnsweredAsTyped(t *testing.T) {
	f := newFixture()
	sent := f.run(textFrom(userID, "❓ Помощь"), textFrom(userID, "🎲 Zar tashla"))

	if len(sent) != 2 || !strings.Contains(sent[0].text, "/about") || sent[0].keyboard == nil {
		t.Fatalf("sent = %+v, want the help with the keyboard", sent)
	}
	calls := f.replier.recorded()
	if len(calls) != 1 || calls[0].text != "🎲 Zar tashla" || sent[1].text != "echo: 🎲 Zar tashla" {
		t.Errorf("model calls = %+v, sent %q", calls, sent[1].text)
	}
	if got := newFixture().run(inGroup(groupMessage("@MyBot ❓ Yordam", "@MyBot"))); len(got) != 1 || got[0].text != "echo: ❓ Yordam" {
		t.Errorf("a group's help button = %+v, want it answered as words", got)
	}
}

// Every button fits Telegram, and no two say the same.
func TestKeyboardLabelsAreDistinct(t *testing.T) {
	var labels []string
	for _, row := range keyboardRows {
		for _, button := range row {
			for _, label := range []string{button.text.uz, button.text.ru, button.text.en} {
				if label == "" || slices.Contains(labels, label) {
					t.Errorf("label %q is empty or used twice", label)
				}
				labels = append(labels, label)
			}
		}
	}
}

// A congratulation in a private chat comes with 🎉; anything else, and
// any answer in a group, without.
func TestCongratulationsComeWithAnEffect(t *testing.T) {
	cases := []struct {
		answer string
		group  bool
		want   string
	}{
		{"Tug'ilgan kuningiz bilan! 🎂", false, telegram.EffectParty},
		{"Tabriklayman, imtihondan o'tibsiz!", false, telegram.EffectParty},
		{"С днём рождения!", false, telegram.EffectParty},
		{"Happy birthday, Ali!", false, telegram.EffectParty},
		{"Bugun havo issiq.", false, ""},
		{"Tabriklayman!", true, ""},
	}
	for _, c := range cases {
		f := newFixture()
		f.replier.answer = c.answer
		update := textFrom(userID, "bugun tug'ilgan kunim")
		if c.group {
			update = inGroup(groupMessage("@MyBot bugun tug'ilgan kunim", "@MyBot"))
		}
		sent := f.run(update)
		if len(sent) != 1 || sent[0].effect != c.want {
			t.Errorf("%q (group %v): sent = %+v, want effect %q", c.answer, c.group, sent, c.want)
		}
	}
}
