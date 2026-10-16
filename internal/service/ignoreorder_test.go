package service

import (
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

func TestIsIgnoreOrder(t *testing.T) {
	cases := map[string]bool{
		"javob berma":                       true,
		"Bunga javob berma!":                true,
		"shu odamga javob berma":            true,
		"unga boshqa javob bermang":         true,
		"javob qaytarma":                    true,
		"gapirma unga":                      true,
		"ignor qil":                         true,
		"ignore":                            true,
		"Ignore him":                        true,
		"e'tibor berma":                     true,
		"eʼtibor berma bunga":               true,
		"bunga javob berma, Mira":           true,
		"жавоб берма":                       true,
		"бунга жавоб қайтарма":              true,
		"эътибор берма":                     true,
		"Не отвечай ему":                    true,
		"игнорируй его":                     true,
		"не разговаривай с ним":             true,
		"shu odamni ignor qil":              true,
		"bu odamni ignor qil":               true,
		"buni ignor qil":                    true,
		"uni ignore qil":                    true,
		"shuni ignor qil, Mira":             true,
		"odamni ignore qil":                 true,
		"ushbu odamga javob berma":          true,
		"шу одамни игнор қил":               true,
		"буни игнор кил":                    true,
		"уни игнор қил":                     true,
		"шуни игнор қил":                    true,
		"игнорируй этого":                   true,
		"buni ignor qil, keyin gaplashamiz": false,
		"uni ko'rib chiq":                   false,
		"javob ber":                         false,
		"bunga javob ber":                   false,
		"nega javob bermading?":             false,
		"javob berma deb aytdimmi?":         false,
		"unga javob berma, menga ber":       false,
		"почему не отвечаешь?":              false,
		"gapir":                             false,
		"":                                  false,
		"Mira":                              false,
		"/ignore@MyBot":                     false,
		"javob bermaslik kerak edi-da":      false,
	}
	for text, want := range cases {
		if got := isIgnoreOrder(text, []string{"mira"}); got != want {
			t.Errorf("isIgnoreOrder(%q) = %v, want %v", text, got, want)
		}
	}
}

// ignoreOrderFixture runs one group message, with owners and a rule book.
func ignoreOrderFixture(msg *telegram.Message) (*fixture, *fakeRuleBook) {
	f := newFixture()
	f.opts.Now = func() time.Time { return headerNow }
	f.opts.OwnerUsernames = []string{ownerUsername}
	book := &fakeRuleBook{}
	f.opts.Rules = book
	f.run(inGroup(msg))
	return f, book
}

func TestOwnersOrderNotToAnswerIsSavedInCode(t *testing.T) {
	msg := fromOwner(groupMessage("Mira, bunga javob berma"))
	msg.ReplyToMessage = &telegram.Message{MessageID: 29, From: bek, Text: "you're slow"}

	f, book := ignoreOrderFixture(msg)

	rule := storage.GroupRule{
		ID: 1, ChatID: groupID, Kind: storage.RuleIgnore, UserID: userID, UserName: "Bek @bek",
		SetBy: 9001, ExpiresAt: headerNow.Add(ignoreCommandFor),
	}
	if len(book.saved) != 1 || book.saved[0] != rule {
		t.Errorf("saved = %+v\nwant    [%+v]", book.saved, rule)
	}
	if calls := f.replier.recorded(); len(calls) != 0 {
		t.Errorf("model calls = %+v; the order is handled in code", calls)
	}
	want := []reaction{{chatID: groupID, messageID: 30, emoji: "👌"}}
	if got := f.reactor.recorded(); len(got) != 1 || got[0] != want[0] {
		t.Errorf("reactions = %+v, want %+v", got, want)
	}
	if sent := f.sender.messages(); len(sent) != 0 {
		t.Errorf("sent = %+v; the reaction is the whole answer", sent)
	}
}

// Anything else from an owner, or an owner's order about someone who may not
// be ignored, still goes to the model.
func TestOtherOrdersGoToTheModel(t *testing.T) {
	replyTo := func(msg *telegram.Message, author *telegram.User) *telegram.Message {
		msg.ReplyToMessage = &telegram.Message{MessageID: 29, From: author, Text: "you're slow"}
		return msg
	}
	owner := &telegram.User{ID: 9002, FirstName: "Ali", Username: ownerUsername}
	cases := map[string]*telegram.Message{
		"an owner without reply": fromOwner(groupMessage("Mira, bunga javob berma")),
		"an owner's question":    replyTo(fromOwner(groupMessage("Mira, nega javob bermading?")), bek),
		"about a group admin":    replyTo(fromOwner(groupMessage("Mira, bunga javob berma")), boss),
		"about an owner":         replyTo(fromOwner(groupMessage("Mira, bunga javob berma")), owner),
		"about the bot":          replyTo(fromOwner(groupMessage("Mira, javob berma")), &telegram.User{ID: botID, IsBot: true}),
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			f, book := ignoreOrderFixture(msg)
			if len(book.saved) != 0 {
				t.Errorf("saved = %+v", book.saved)
			}
			onlyRequest(t, f)
		})
	}
}

// Only owners decide whom the bot ignores. Anyone else's message that is
// nothing but such an order gets no answer at all, not even a refusal that
// would repeat it, and stays in the log as other chatter does.
func TestOthersOrderNotToAnswerIsPassedOver(t *testing.T) {
	cases := map[string]*telegram.User{
		"a member":      {ID: 4242, FirstName: "Ann"},
		"a group admin": {ID: groupAdminID, FirstName: "Boss"},
	}
	for name, sender := range cases {
		t.Run(name, func(t *testing.T) {
			order := groupMessage("Mira, shu odamni ignor qil")
			order.From = sender
			order.ReplyToMessage = &telegram.Message{MessageID: 29, From: bek, Text: "you're slow"}
			f := newFixture()
			f.opts.Workers = 1
			f.opts.OwnerUsernames = []string{ownerUsername}
			book := &fakeRuleBook{}
			f.opts.Rules = book

			sent := f.run(inGroup(order), inGroup(groupMessage("Mira, hi")))

			expectOneReply(t, sent, "echo: hi")
			call := onlyRequest(t, f)
			if len(book.saved) != 0 {
				t.Errorf("saved = %+v; only an owner's order is saved", book.saved)
			}
			if reactions := f.reactor.recorded(); len(reactions) != 0 {
				t.Errorf("reactions = %+v; the order gets nothing", reactions)
			}
			// Logged, the order would reach the model, which repeats it.
			if strings.Contains(call.req.Context, "ignor qil") {
				t.Errorf("context = %q; a member's order must not reach the model", call.req.Context)
			}
		})
	}
}
