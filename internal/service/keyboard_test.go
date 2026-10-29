package service

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// /start and /help bring the keyboard to a private chat, in the app's
// language, without a meme button, which the owners took off; other
// answers leave the keyboard as it is.
func TestStartAndHelpBringTheKeyboard(t *testing.T) {
	f := newFixture()
	russian := textFrom(userID, "/help")
	russian.Message.From.LanguageCode = "ru"

	sent := f.run(textFrom(userID, "/start"), russian, textFrom(userID, "/about"), textFrom(userID, "salom, qalaysan"))

	if len(sent) != 4 {
		t.Fatalf("sent = %+v", sent)
	}
	if got := sent[0].keyboard; len(got) != 3 || got[0][0] != "💵 Valyuta kursi" || got[1][0] != "🎮 MLBB" || got[2][1] != "❓ Yordam" || strings.Contains(fmt.Sprint(got), "Mem") {
		t.Errorf("/start keyboard = %q", got)
	}
	if got := sent[1].keyboard; len(got) != 3 || got[2][1] != "❓ Помощь" {
		t.Errorf("Russian /help keyboard = %q", got)
	}
	if sent[2].keyboard != nil || sent[3].keyboard != nil {
		t.Errorf("/about and an answer carry a keyboard: %q, %q", sent[2].keyboard, sent[3].keyboard)
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

// Every button fits Telegram, and no two say the same; one may say the
// same in every language, as "🎮 MLBB" does.
func TestKeyboardLabelsAreDistinct(t *testing.T) {
	var labels []string
	for _, row := range keyboardRows {
		for _, button := range row {
			var own []string
			for _, label := range []string{button.uz, button.ru, button.en} {
				if !slices.Contains(own, label) {
					own = append(own, label)
				}
			}
			for _, label := range own {
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
