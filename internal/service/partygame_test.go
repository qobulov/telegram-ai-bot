package service

import (
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// /tod is answered in code with its three buttons; a press asks the model as
// if the presser had written the button's command, in reply to /tod.
func TestPartyGameButtons(t *testing.T) {
	f := newFixture()
	f.opts.Buttons = &fakeButtons{}

	sent := f.run(inGroup(groupMessage("/tod")))
	if len(sent) != 1 || !strings.Contains(sent[0].text, "Truth or Dare") || len(sent[0].buttons) != 2 || sent[0].buttons[0][0].Data != "tod:/haqiqat" || sent[0].buttons[1][0].Data != "tod:/tanla" {
		t.Fatalf("sent = %+v", sent)
	}
	if n := len(f.replier.recorded()); n != 0 {
		t.Fatalf("/tod asked the model %d times", n)
	}

	presser := &telegram.User{ID: 77, FirstName: "Vali"}
	f.run(pressUpdate(1, presser, telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}, "Truth or Dare", "tod:/jurat"))
	req := onlyRequest(t, f).req
	if !strings.Contains(req.Text, "Vali") || !strings.Contains(req.Text, "jur'at ber") || req.UserID != 77 {
		t.Errorf("press asked %q for user %d, want Vali's dare", req.Text, req.UserID)
	}
}

// The commands behind the buttons work when typed too.
func TestPartyCommandsAskTheModel(t *testing.T) {
	for command, want := range map[string]string{"/haqiqat": "haqiqat ber", "/jurat@MyBot": "jur'at ber", "/tanla": "would you rather"} {
		f := newFixture()
		f.run(inGroup(groupMessage(command)))
		if req := onlyRequest(t, f).req; !strings.Contains(req.Text, want) {
			t.Errorf("%s asked %q, want it to say %q", command, req.Text, want)
		}
	}
}
