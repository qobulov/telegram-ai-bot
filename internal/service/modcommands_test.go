package service

import (
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// A length is read in minutes from "4h", "2d" or "30", the rest being the
// reason.
func TestModArgs(t *testing.T) {
	for arg, want := range map[string]struct {
		minutes int
		reason  string
	}{
		"4h spam": {240, "spam"}, "30": {30, ""}, "2d": {2880, ""}, "1w": {10080, ""}, "3soat": {180, ""}, "": {0, ""}, "flood bo'ldi": {0, "flood bo'ldi"},
	} {
		if minutes, reason := modArgs(arg); minutes != want.minutes || reason != want.reason {
			t.Errorf("modArgs(%q) = %d %q", arg, minutes, reason)
		}
	}
}

// Replying to a member's message, a plain /mute 4h is done through the
// moderate tool without the model, and what it says is sent; a refusal is
// passed on. /mute replying to no one still quiets the bot.
func TestModCommandsWithoutTheModel(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	runner.answers["moderate"] = `{"ok":true,"result":{"done":true,"said":"Bajarildi: Sam 4 soat yoza olmaydi."}}`
	reply := func(text string) *telegram.Message {
		msg := groupMessage(text)
		msg.ReplyToMessage = &telegram.Message{MessageID: 9, From: &telegram.User{ID: 501, FirstName: "Sam"}, Chat: msg.Chat, Text: "Ginni ami"}
		return msg
	}
	sent := f.run(inGroup(reply("/mute 4h spam")))
	if len(runner.asked) != 1 || !strings.Contains(runner.asked[0], `"action":"mute"`) || !strings.Contains(runner.asked[0], `"minutes":240`) || !strings.Contains(runner.asked[0], `"reason":"spam"`) {
		t.Fatalf("asked = %v", runner.asked)
	}
	if len(sent) != 1 || sent[0].text != "Bajarildi: Sam 4 soat yoza olmaydi." || len(f.replier.recorded()) != 0 {
		t.Fatalf("sent = %+v, model asked %d times", sent, len(f.replier.recorded()))
	}
	runner.answers["moderate"] = `{"ok":false,"error":{"code":"forbidden","message":"Buni faqat guruh adminlari so'ray oladi."}}`
	if sent = f.run(inGroup(reply("/ban"))); len(sent) != 2 || !strings.Contains(sent[1].text, "faqat guruh adminlari") || !strings.Contains(runner.asked[1], `"action":"ban"`) {
		t.Errorf("a refused ban = %+v", sent)
	}
	if f.run(inGroup(groupMessage("/mute"))); len(runner.asked) != 2 {
		t.Errorf("/mute alone reached the tool: %v", runner.asked)
	}
}

// A short order in words, replying to a member, is done as a command is:
// "Мут мари 1 минутга" mutes for a minute; talk about muting goes to the
// model.
func TestModOrdersInWords(t *testing.T) {
	f := newFixture()
	f.opts.WakeWords = []string{"мари", "maria"}
	s := New(f.opts)
	for text, want := range map[string]struct {
		action  string
		minutes int
		ok      bool
	}{
		"Мут мари 1 минутга": {"mute", 1, true}, "мут 4 soat": {"mute", 240, true}, "ban": {"ban", 0, true},
		"Maria, кик!": {"kick", 0, true}, "мут 2kun": {"mute", 2880, true}, "размут": {"unmute", 0, true},
		"мут бериш керак эди": {"", 0, false}, "уни мут қил": {"", 0, false}, "мут 5 йил": {"", 0, false},
	} {
		action, minutes, ok := s.modWords(text)
		if action != want.action || minutes != want.minutes || ok != want.ok {
			t.Errorf("modWords(%q) = %q %d %v", text, action, minutes, ok)
		}
	}
	runner := withToolRunner(f)
	runner.answers["moderate"] = `{"ok":true,"result":{"done":true,"said":"Bajarildi."}}`
	msg := groupMessage("Мут мари 1 минутга")
	msg.ReplyToMessage = &telegram.Message{MessageID: 9, From: &telegram.User{ID: 501, FirstName: "Sam"}, Chat: msg.Chat, Text: "Yo bob turila"}
	sent := f.run(inGroup(msg))
	if len(runner.asked) != 1 || !strings.Contains(runner.asked[0], `"minutes":1`) || len(sent) != 1 || sent[0].text != "Bajarildi." || len(f.replier.recorded()) != 0 {
		t.Fatalf("asked %v, sent %+v, model asked %d times", runner.asked, sent, len(f.replier.recorded()))
	}
}

// An order replying to the bot's answer about a member reaches that member:
// "Мут бер" after the bot answered "mute him" with a question.
func TestModOrderReplyingToTheBotsAnswer(t *testing.T) {
	f := newFixture()
	f.opts.WakeWords = []string{"мари"}
	runner := withToolRunner(f)
	runner.answers["moderate"] = `{"ok":true,"result":{"done":true,"said":"Bajarildi."}}`
	first := groupMessage("@MyBot shu bolani tinchlantir", "@MyBot")
	first.ReplyToMessage = &telegram.Message{MessageID: 9, From: &telegram.User{ID: 501, FirstName: "Sam"}, Chat: first.Chat, Text: "Yo bob turila"}
	order := groupMessage("Мут бер")
	order.MessageID = 31
	order.ReplyToMessage = &telegram.Message{MessageID: 1001, From: &telegram.User{ID: botID, IsBot: true}, Chat: order.Chat, Text: "Kimni?"}
	sent := f.run(inGroup(first), inGroup(order))
	if len(runner.asked) != 1 || !strings.Contains(runner.asked[0], `"action":"mute"`) || len(sent) != 2 || sent[1].text != "Bajarildi." {
		t.Fatalf("asked %v, sent %+v", runner.asked, sent)
	}
}
