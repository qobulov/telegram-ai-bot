package service

import (
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// /ship bare gives the random match's button, whose press asks for one;
// with a member replied to, the model matches them with the sender.
func TestShipCommand(t *testing.T) {
	partyPressed.Clear()
	f := newFixture()
	f.opts.Buttons = &fakeButtons{}

	sent := f.run(inGroup(groupMessage("/ship")))
	if len(sent) != 1 || len(sent[0].buttons) != 1 || sent[0].buttons[0][0].Data != "tod:/shipme" || len(f.replier.recorded()) != 0 {
		t.Fatalf("sent = %+v", sent)
	}
	f.run(pressUpdate(2, &telegram.User{ID: 77, FirstName: "Vali"}, telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}, "Moslik", "tod:/shipme"))
	if req := onlyRequest(t, f).req; !strings.Contains(req.Text, "random") || !strings.Contains(req.Text, "tg://user?id=77") || req.UserID != 77 {
		t.Errorf("press asked %q for %d, want the presser's own tag", req.Text, req.UserID)
	}
	// One game a message: a second press asks nothing.
	f.run(pressUpdate(3, &telegram.User{ID: 78, FirstName: "Gani"}, telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}, "Moslik", "tod:/shipme"))
	if n := len(f.replier.recorded()); n != 1 {
		t.Errorf("model calls after a second press = %d, want 1", n)
	}

	f = newFixture()
	msg := groupMessage("/ship")
	msg.ReplyToMessage = &telegram.Message{MessageID: 5, From: &telegram.User{ID: 88, FirstName: "Ali"}, Chat: msg.Chat, Text: "salom"}
	f.run(inGroup(msg))
	if req := onlyRequest(t, f).req; !strings.Contains(req.Text, "ship qil") {
		t.Errorf("reply asked %q, want ship qil", req.Text)
	}
}
