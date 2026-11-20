package service

import (
	"strings"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// In a group without AI the model is never asked: talk gets one notice and
// then silence, while commands and the bot's owners still go.
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
	shown := len(sent)
	if sent = f.run(inGroup(groupMessage("/weather Toshkent"))); len(sent) != shown || len(f.replier.recorded()) != before {
		t.Errorf("/weather, no command any more, got %+v", sent[shown:])
	}
	owner := groupMessage("Mira, salom")
	owner.From = &telegram.User{ID: 77, FirstName: "Aziz"}
	f.run(inGroup(owner))
	if len(f.replier.recorded()) != before+1 {
		t.Errorf("an owner was not answered")
	}
}

// /aichat is no command any more: in a group, an owner's too, it gets no
// answer and changes nothing; AI is turned on with /aigroups in private.
func TestAIChatIsSilentInAGroup(t *testing.T) {
	f := newFixture()
	extras := newMemoryExtras()
	f.opts.Extras = extras
	f.opts.OwnerIDs = map[int64]bool{77: true}
	owner := groupMessage("/aichat")
	owner.From = &telegram.User{ID: 77, FirstName: "Aziz"}
	if sent := f.run(inGroup(groupMessage("/aichat")), inGroup(owner)); len(sent) != 0 || extras.settings[storage.SettingAI] != "" {
		t.Errorf("/aichat in a group = %+v, %v; want no answer and nothing set", sent, extras.settings)
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
		return pressUpdate(int(from), &telegram.User{ID: from}, telegram.Chat{ID: from, Type: telegram.ChatTypePrivate}, "", last.buttons[0][0].Data)
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
