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
	f.run(asOwner("/aichat off"))
	if extras.settings[storage.SettingAI] != "" {
		t.Errorf("off left %v", extras.settings)
	}
}

// /aigroups lists the bot's groups as buttons, ✅ or ❌; an owner's press
// turns AI on or off in that group, anyone else's is refused.
func TestAIGroupsButtons(t *testing.T) {
	f := newFixture()
	extras, buttons := newMemoryExtras(), &fakeButtons{}
	f.opts.Extras, f.opts.Buttons = extras, buttons
	f.opts.OwnerIDs = map[int64]bool{77: true}
	sent := f.run(textFrom(77, "/aigroups"))
	last := sent[len(sent)-1]
	if len(last.buttons) != 1 || last.buttons[0][0].Text != "❌ Devs" {
		t.Fatalf("menu = %+v", last.buttons)
	}
	press := func(from int64) telegram.Update {
		return doozPlayer(int(from), &telegram.User{ID: from}, telegram.Chat{ID: from, Type: telegram.ChatTypePrivate}, "", last.buttons[0][0].Data)
	}
	f.run(press(5))
	if extras.settings[storage.SettingAI] != "" {
		t.Errorf("a stranger turned AI on")
	}
	f.run(press(77))
	if extras.settings[storage.SettingAI] != "on" {
		t.Fatalf("the owner's press left %v", extras.settings)
	}
	edits, menus := buttons.recordedMenus()
	if len(menus) == 0 || menus[len(menus)-1][0][0].Text != "✅ Devs" || !strings.Contains(edits[len(edits)-1], "1 ta guruhda") {
		t.Errorf("menu after = %+v %q", menus, edits)
	}
}
