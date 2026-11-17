package service

import (
	"strings"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// In a group without AI the model is never asked: talk gets one notice and
// then silence, while commands, /weather and the bot's owners still go.
func TestGroupWithoutAI(t *testing.T) {
	f := newFixture()
	f.opts.Extras = newMemoryExtras()
	f.opts.OwnerIDs = map[int64]bool{77: true}
	f.opts.Workers = 1
	sent := f.run(inGroup(groupMessage("Mira, salom")), inGroup(groupMessage("Mira, qalaysan")))
	if len(f.replier.recorded()) != 0 {
		t.Fatalf("the model was asked %d times", len(f.replier.recorded()))
	}
	if len(sent) != 1 || !strings.Contains(sent[0].text, "AI suhbat yoqilmagan") {
		t.Fatalf("sent = %+v, want one notice", sent)
	}
	sent = f.run(inGroup(groupMessage("/id@MyBot", "@MyBot")))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "ID") {
		t.Errorf("a command = %q", last)
	}
	before := len(f.replier.recorded())
	f.run(inGroup(groupMessage("/weather Toshkent")))
	if len(f.replier.recorded()) != before+1 {
		t.Errorf("/weather did not reach its tool")
	}
	owner := groupMessage("Mira, salom")
	owner.From = &telegram.User{ID: 77, FirstName: "Aziz"}
	f.run(inGroup(owner))
	if len(f.replier.recorded()) != before+2 {
		t.Errorf("an owner was not answered")
	}
}

// An owner turns AI on and off with /aichat; anyone else's is ignored;
// /aigroups lists the groups with it.
func TestAIChatCommand(t *testing.T) {
	f := newFixture()
	extras := newMemoryExtras()
	f.opts.Extras = extras
	f.opts.OwnerIDs = map[int64]bool{77: true}
	asOwner := func(text string) telegram.Update {
		msg := groupMessage(text)
		msg.From = &telegram.User{ID: 77, FirstName: "Aziz"}
		return inGroup(msg)
	}
	if sent := f.run(inGroup(groupMessage("/aichat"))); len(sent) != 0 || extras.settings[storage.SettingAI] != "" {
		t.Errorf("a member's /aichat = %+v, %v", sent, extras.settings)
	}
	sent := f.run(asOwner("/aichat"))
	if extras.settings[storage.SettingAI] != "on" || !strings.Contains(sent[len(sent)-1].text, "yoqildi") {
		t.Fatalf("on = %v, %+v", extras.settings, sent)
	}
	f.run(inGroup(groupMessage("Mira, salom")))
	if len(f.replier.recorded()) != 1 {
		t.Errorf("a member was not answered with AI on")
	}
	private := textFrom(77, "/aigroups")
	private.Message.From.ID = 77
	sent = f.run(private)
	if last := sent[len(sent)-1].text; !strings.Contains(last, "Devs") {
		t.Errorf("/aigroups = %q", last)
	}
	f.run(asOwner("/aichat off"))
	if extras.settings[storage.SettingAI] != "" {
		t.Errorf("off left %v", extras.settings)
	}
}
