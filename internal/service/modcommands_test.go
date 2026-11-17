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
		"замуть его на 5 минут": {"mute", 5, true}, "mute him for 2 hours": {"mute", 120, true}, "забань": {"ban", 0, true},
		"кикни": {"kick", 0, true}, "выгони пожалуйста": {"kick", 0, true},
		"chiqar": {"kick", 0, true}, "ҳайда": {"kick", 0, true}, "мут 1 соатга": {"mute", 60, true}, "разбань": {"unban", 0, true},
		"мут бериш керак эди": {"", 0, false}, "уни мут қил": {"", 0, false}, "мут 5 йил": {"", 0, false},
		"chiqar bu gapni yana bir marta": {"", 0, false},
		// adminship, which the model once refused one time in three
		"Админ кил мари": {"promote", 0, true}, "admin qil": {"promote", 0, true}, "админ бер": {"promote", 0, true},
		"сделай админом": {"promote", 0, true}, "Админ бекор кил мари": {"demote", 0, true}, "Админни бекор кил мари": {"demote", 0, true},
		"admin bekor qil": {"demote", 0, true}, "adminlikdan ol": {"demote", 0, true}, "сними админа": {"demote", 0, true},
		"admin emas": {"demote", 0, true}, "кто тут админ": {"", 0, false}, "admin qachon keladi": {"", 0, false},
		// warnings
		"varn ber": {"warn", 0, true}, "пред": {"warn", 0, true}, "Мари предупреждение": {"warn", 0, true},
		"ogohlantir": {"warn", 0, true}, "unwarn": {"unwarn", 0, true}, "варн нима": {"", 0, false},
		"Пин мари": {"pin", 0, true}, "qada": {"pin", 0, true}, "закрепи": {"pin", 0, true},
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

// A moderation command with no reply asks whom, to an admin, and stays
// silent to a member, whose command may be another bot's; a bare /mute asks
// too, and only "/mute bot" quiets the bot.
func TestModCommandsWithoutAReplyAskWhom(t *testing.T) {
	f := newFixture()
	f.members.admins = map[int64]bool{userID: true}
	withToolRunner(f)
	sent := f.run(from(userID, "/kick", 0))
	if len(sent) != 1 || !strings.Contains(sent[0].text, "reply qilib /kick") {
		t.Fatalf("an admin's bare /kick = %+v", sent)
	}
	if sent = f.run(from(55, "/ban", 0)); len(sent) != 1 {
		t.Errorf("a member's bare /ban was answered: %+v", sent)
	}
	if sent = f.run(from(userID, "/mute", 0)); !strings.Contains(sent[len(sent)-1].text, "Kimni mut qilay") {
		t.Errorf("a bare /mute = %q", sent[len(sent)-1].text)
	}
	if sent = f.run(from(userID, "/mute bot", 0)); strings.Contains(sent[len(sent)-1].text, "Kimni") {
		t.Errorf("/mute bot = %q", sent[len(sent)-1].text)
	}
}

// /promote Streamer makes the member replied to an admin and titles them;
// /title and /tag take their whole argument as the text.
func TestAdminCommands(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	runner.answers["moderate"] = `{"ok":true,"result":{"done":true,"said":"Bajarildi."}}`
	reply := func(text string) *telegram.Message {
		msg := groupMessage(text)
		msg.ReplyToMessage = &telegram.Message{MessageID: 9, From: &telegram.User{ID: 501, FirstName: "Sam"}, Chat: msg.Chat, Text: "salom"}
		return msg
	}
	f.run(inGroup(reply("/promote Sazeks streamer")))
	if len(runner.asked) != 2 || !strings.Contains(runner.asked[0], `"action":"promote"`) ||
		!strings.Contains(runner.asked[1], `"action":"admin_title"`) || !strings.Contains(runner.asked[1], `"text":"Sazeks streamer"`) {
		t.Fatalf("asked = %v", runner.asked)
	}
	f.run(inGroup(reply("/tag Pro player")), inGroup(reply("/admin")))
	if len(runner.asked) != 4 || !strings.Contains(runner.asked[2], `"action":"member_tag"`) || !strings.Contains(runner.asked[2], `"text":"Pro player"`) ||
		!strings.Contains(runner.asked[3], `"action":"promote"`) {
		t.Errorf("asked = %v", runner.asked)
	}
}
