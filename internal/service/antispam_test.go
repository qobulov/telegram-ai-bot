package service

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// floodOf is n messages from the flooder, numbered from first, each its
// own text.
func floodOf(n int, first int64) []telegram.Update {
	var updates []telegram.Update
	for i := range n {
		msg := messageFrom(flooderID, "tez", fmt.Sprintf("xabar %d", i))
		msg.MessageID = first + int64(i)
		updates = append(updates, inGroup(msg))
	}
	return updates
}

// logAgo logs a message from a member as sent a while ago.
func logAgo(f *fixture, userID, messageID int64, text string, ago time.Duration) {
	f.chatLog.mu.Lock()
	defer f.chatLog.mu.Unlock()
	f.chatLog.messages = append(f.chatLog.messages, storage.GroupMessage{
		ChatID: groupID, MessageID: messageID, AuthorID: userID, AuthorName: "Sam", Text: text, SentAt: time.Now().Add(-ago),
	})
}

// In a group with no rules at all, the flood guard and the spam lists are
// on.
func TestAntispamIsOnWithoutAnyRule(t *testing.T) {
	f, _, moderator := protectFixture()
	restrictor := f.opts.Restrictor.(*fakeRestrictor)

	sent := f.run(append(floodOf(6, 100), inGroup(messageFrom(spammerID, "promo", "salom")))...)

	if len(restrictor.recorded()) != 1 || !slices.Equal(moderator.banned, []int64{spammerID}) {
		t.Errorf("restrictions %+v, banned %v; want the flooder muted and the spammer banned", restrictor.recorded(), moderator.banned)
	}
	if len(sent) != 2 || !strings.Contains(sent[0].text, "(flood) 10 daqiqa") {
		t.Errorf("sent = %+v", sent)
	}
}

// /antispam off, from an admin, turns them off; /antispam on back on.
func TestAntispamOffAndOn(t *testing.T) {
	f, _, moderator := protectFixture()
	extras := f.opts.Extras.(*memoryExtras)

	sent := f.run(from(groupAdminID, "/antispam off", 0))
	if extras.settings[storage.SettingAntispamOff] != "flood,spam,newcomers" || len(sent) != 1 || strings.Count(sent[0].text, "❌") != 3 {
		t.Fatalf("after off: setting %q, sent %+v", extras.settings[storage.SettingAntispamOff], sent)
	}
	f.run(append(floodOf(7, 100), inGroup(messageFrom(spammerID, "promo", "salom")))...)
	if len(moderator.deleted)+len(moderator.banned)+len(f.opts.Restrictor.(*fakeRestrictor).recorded()) != 0 {
		t.Errorf("guards acted while off: deleted %v, banned %v", moderator.deleted, moderator.banned)
	}

	f.run(from(groupAdminID, "/antispam on", 0))
	if extras.settings[storage.SettingAntispamOff] != "" {
		t.Errorf("after on: setting %q", extras.settings[storage.SettingAntispamOff])
	}
	f.run(inGroup(messageFrom(spammerID, "promo", "salom yana")))
	if !slices.Equal(moderator.banned, []int64{spammerID}) {
		t.Errorf("banned %v after on", moderator.banned)
	}
}

// A member who is no admin may not set it.
func TestAntispamRefusesMembers(t *testing.T) {
	f, _, _ := protectFixture()
	sent := f.run(from(5151, "/antispam off", 0))
	if len(sent) != 1 || !strings.HasPrefix(sent[0].text, "⛔") || f.opts.Extras.(*memoryExtras).settings[storage.SettingAntispamOff] != "" {
		t.Errorf("sent %+v", sent)
	}
}

// Ten messages within thirty seconds are a flood, even when fewer than six
// fell within ten.
func TestFloodGuardCatchesTenInThirtySeconds(t *testing.T) {
	f, _, moderator := protectFixture()
	for i := range 5 {
		logAgo(f, flooderID, int64(90+i), fmt.Sprintf("eski %d", i), 20*time.Second)
	}
	sent := f.run(floodOf(5, 100)...)

	if len(sent) != 1 || !strings.Contains(sent[0].text, "(flood)") {
		t.Fatalf("sent = %+v, want a mute at the tenth", sent)
	}
	if !slices.Contains(moderator.deleted, 90) || !slices.Contains(moderator.deleted, 104) {
		t.Errorf("deleted %v, want the older ones too", moderator.deleted)
	}
}

// The same text three times within a minute is a flood; so is the same
// sticker. Short words such as "ha", and commands, are not.
func TestFloodGuardCatchesRepeats(t *testing.T) {
	f, _, _ := protectFixture()
	logAgo(f, flooderID, 90, "(to Ann) Salom   HAMMAGA", 40*time.Second)
	logAgo(f, flooderID, 91, "salom hammaga", 30*time.Second)
	if sent := f.run(inGroup(messageFrom(flooderID, "tez", "salom hammaga"))); len(sent) != 1 || !strings.Contains(sent[0].text, "(flood)") {
		t.Errorf("text repeat: sent = %+v", sent)
	}

	f, _, _ = protectFixture()
	var stickers []telegram.Update
	for i := range 3 {
		msg := messageFrom(5252, "st", "")
		msg.MessageID = int64(200 + i)
		msg.Sticker = &telegram.Sticker{FileID: "s", FileUniqueID: "s1", Emoji: "😂"}
		stickers = append(stickers, inGroup(msg))
	}
	if sent := f.run(stickers...); len(sent) != 1 || !strings.Contains(sent[0].text, "(flood)") {
		t.Errorf("sticker repeat: sent = %+v", sent)
	}

	// Three shares of a card through an inline bot, alike in the log, are
	// no repeat: a player trading or showing cards is not flooding.
	f, _, moderator := protectFixture()
	logAgo(f, flooderID, 92, "(to Nur @nur) Naruto Uzumaki", 20*time.Second)
	logAgo(f, flooderID, 93, "(to Nur @nur) Naruto Uzumaki", 10*time.Second)
	share := messageFrom(flooderID, "tez", "Naruto Uzumaki")
	share.ViaBot = &telegram.User{ID: 77, IsBot: true, Username: "gemini_max1bot"}
	if sent := f.run(inGroup(share)); len(sent) != 0 || len(moderator.deleted) != 0 {
		t.Errorf("an inline bot's share three times: sent %+v, deleted %v", sent, moderator.deleted)
	}

	f, _, moderator = protectFixture()
	var short []telegram.Update
	for i := range 4 {
		msg := messageFrom(5353, "ha", "ha")
		if i%2 == 1 {
			msg.Text = "/top"
		}
		msg.MessageID = int64(300 + i)
		short = append(short, inGroup(msg))
	}
	if sent := f.run(short...); len(sent) != 0 || len(moderator.deleted) != 0 {
		t.Errorf("\"ha\" and /top twice each: sent %+v, deleted %v", sent, moderator.deleted)
	}
}

// Flood mutes grow within a day: 10 minutes, then an hour, then a day.
func TestFloodMutesEscalate(t *testing.T) {
	for past, want := range []string{"10 daqiqa", "1 soat", "1 kun", "1 kun"} {
		f, _, _ := protectFixture()
		strikes := f.opts.Strikes.(*fakeStrikes)
		for range past {
			strikes.tookAt(storage.ModerationAction{ChatID: groupID, Action: storage.ActionFloodMute, TargetUserID: flooderID, UpdateID: -int64(past)}, time.Now().Add(-2*time.Hour))
		}
		// One a day and a half ago no longer counts.
		strikes.tookAt(storage.ModerationAction{ChatID: groupID, Action: storage.ActionFloodMute, TargetUserID: flooderID, UpdateID: -99}, time.Now().Add(-36*time.Hour))
		start := time.Now()

		sent := f.run(floodOf(6, 100)...)

		done := f.opts.Restrictor.(*fakeRestrictor).recorded()
		if len(sent) != 1 || !strings.Contains(sent[0].text, "(flood) "+want+" yozolmaydi") || len(done) != 1 {
			t.Errorf("after %d mutes: sent %+v, restrictions %+v; want %s", past, sent, done, want)
			continue
		}
		if got := done[0].Until.Sub(start); got < floodMuteFor(past)-time.Minute || got > floodMuteFor(past)+time.Minute {
			t.Errorf("after %d mutes: muted for %v", past, got)
		}
	}
}

// A newcomer's link is deleted with a warning, and a second within the hour
// mutes them; the same link from a member of the group stays, and so does a
// link to the group itself.
func TestNewcomerLinksAreDeleted(t *testing.T) {
	f, _, moderator := protectFixture()
	link := func(userID, messageID int64, text, url string) telegram.Update {
		msg := messageFrom(userID, "yangi", text)
		msg.MessageID = messageID
		msg.Chat.Username = "devs_uz"
		msg.Entities = []telegram.MessageEntity{entityAt("url", text, url)}
		return inGroup(msg)
	}
	established(f, 5454)

	sent := f.run(
		link(5353, 100, "kanalim: t.me/promo_kanal", "t.me/promo_kanal"),
		link(5454, 101, "kanalim: t.me/promo_kanal", "t.me/promo_kanal"),
		link(5353, 102, "guruh: https://t.me/devs_uz/5", "https://t.me/devs_uz/5"),
		link(5353, 103, "yana https://spam.example", "https://spam.example"),
	)

	if !slices.Equal(moderator.deleted, []int64{100, 103}) {
		t.Errorf("deleted %v, want the newcomer's two outside links", moderator.deleted)
	}
	if len(sent) != 2 || !strings.Contains(sent[0].text, "yangi a'zolar havola") || !strings.Contains(sent[1].text, "1 soat yozolmaydi") {
		t.Errorf("sent = %+v, want a warning then a mute", sent)
	}
	if done := f.opts.Restrictor.(*fakeRestrictor).recorded(); len(done) != 1 || done[0].UserID != 5353 {
		t.Errorf("restrictions = %+v", done)
	}
}

// A newcomer's channel post or message with buttons goes too; an admin's
// link stays.
func TestNewcomerForwardsAndButtonsAreDeleted(t *testing.T) {
	f, _, moderator := protectFixture()
	forward := messageFrom(5353, "yangi", "zo'r post")
	forward.ForwardOrigin = &telegram.MessageOrigin{Type: "channel", Chat: &telegram.Chat{ID: -100777, Title: "Promo"}}
	buttons := messageFrom(5252, "b", "bosing")
	buttons.MessageID = 31
	buttons.ReplyMarkup = &telegram.MessageButtons{InlineKeyboard: [][]json.RawMessage{{json.RawMessage(`{"text":"ok","url":"https://x.example"}`)}}}
	admin := messageFrom(guardAdminID, "adm", "https://partner.example")
	admin.MessageID = 32
	admin.Entities = []telegram.MessageEntity{entityAt("url", admin.Text, "https://partner.example")}

	f.run(inGroup(forward), inGroup(buttons), inGroup(admin))

	if !slices.Equal(moderator.deleted, []int64{30, 31}) {
		t.Errorf("deleted %v, want the forward and the buttons", moderator.deleted)
	}
}

// Where the bot may not delete or restrict, the guards do nothing.
func TestAntispamNeedsTheBotsRights(t *testing.T) {
	f, _, moderator := protectFixture()
	f.opts.BotRights = &fakeBotRights{rights: []string{telegram.RightPinMessages}}
	link := messageFrom(5353, "yangi", "https://spam.example")
	link.MessageID = 200
	link.Entities = []telegram.MessageEntity{entityAt("url", link.Text, "https://spam.example")}

	sent := f.run(append(floodOf(7, 100), inGroup(messageFrom(spammerID, "promo", "salom")), inGroup(link))...)

	if len(sent)+len(moderator.deleted)+len(moderator.banned)+len(f.opts.Restrictor.(*fakeRestrictor).recorded()) != 0 {
		t.Errorf("sent %+v, deleted %v, banned %v; want nothing done", sent, moderator.deleted, moderator.banned)
	}
}

// /antispam's buttons switch one guard each, for an admin; anyone else gets
// a box and changes nothing.
func TestAntispamButtons(t *testing.T) {
	f, _, _ := protectFixture()
	buttons := &fakeButtons{}
	f.opts.Buttons = buttons
	extras := f.opts.Extras.(*memoryExtras)
	sent := f.run(from(groupAdminID, "/antispam", 0))
	if len(sent) != 1 || len(sent[0].buttons) != 1 || len(sent[0].buttons[0]) != 3 || sent[0].buttons[0][0].Text != "✅ Flood" {
		t.Fatalf("menu = %+v", sent)
	}
	chat := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}
	press := func(user int64) telegram.Update {
		return doozPlayer(int(user), &telegram.User{ID: user, FirstName: "U"}, chat, "", sent[0].buttons[0][0].Data)
	}

	f.run(press(5151))
	if extras.settings[storage.SettingAntispamOff] != "" || len(buttons.answers) != 1 || !buttons.answers[0].alert {
		t.Fatalf("a member's press: setting %q, answers %+v", extras.settings[storage.SettingAntispamOff], buttons.answers)
	}
	f.run(press(groupAdminID))
	if extras.settings[storage.SettingAntispamOff] != "flood" {
		t.Fatalf("after the admin's press: %q", extras.settings[storage.SettingAntispamOff])
	}
	_, menus := buttons.recordedMenus()
	if len(menus) == 0 || menus[len(menus)-1][0][0].Text != "❌ Flood" {
		t.Errorf("menu after = %+v", menus)
	}
	f.run(press(groupAdminID))
	if extras.settings[storage.SettingAntispamOff] != "" {
		t.Errorf("after the second press: %q", extras.settings[storage.SettingAntispamOff])
	}
}
