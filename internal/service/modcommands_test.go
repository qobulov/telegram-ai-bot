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
