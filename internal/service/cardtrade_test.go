package service

import (
	"strings"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// toMember is a group message replying to the member a gift or trade is for.
func toMember(text string, member *telegram.User) telegram.Update {
	msg := groupMessage(text)
	msg.ReplyToMessage = &telegram.Message{MessageID: 29, From: member, Chat: msg.Chat, Text: "salom"}
	return inGroup(msg)
}

// /trade offers the member replied to a trade of cards named in part, one
// only they may accept, once.
func TestCardsAreTraded(t *testing.T) {
	f := newFixture()
	buttons := &fakeButtons{}
	f.opts.Buttons = buttons
	book := newFakeCards("")
	f.opts.Cards = book
	const rival = userID + 1
	vali := &telegram.User{ID: rival, FirstName: "Vali"}
	book.held[userID], book.held[rival] = 1, 1
	book.named[userID] = []storage.Card{
		{ID: 10, Name: "Gojo Satoru", Rarity: 4, Value: 5000},
		{ID: 20, Name: "Levi", Rarity: 3, Value: 1000},
		{ID: 30, Name: "Naruto Uzumaki", Rarity: 2, Value: 300},
	}
	last := func(sent []sentMessage) sentMessage { return sent[len(sent)-1] }

	if sent := f.run(textFrom(userID, "/trade Levi = Gojo")); !strings.Contains(last(sent).text, "guruhda") || !strings.Contains(last(sent).text, "/cards") {
		t.Fatalf("a trade in private = %+v", sent)
	}
	if sent := f.run(inGroup(groupMessage("/trade Levi = Gojo"))); !strings.Contains(last(sent).text, "javob qilib") {
		t.Fatalf("a trade with no one = %+v", sent)
	}
	if sent := f.run(toMember("/trade u = Levi", vali)); !strings.Contains(last(sent).text, "Qaysi biri") || !strings.Contains(last(sent).text, "Naruto Uzumaki") {
		t.Fatalf("a name of two cards = %+v", last(sent))
	}
	if sent := f.run(toMember("/trade Sasuke = Levi", vali)); !strings.Contains(last(sent).text, "«Sasuke» degan karta yo'q") {
		t.Fatalf("a card not held = %+v", last(sent))
	}
	book.named[rival] = []storage.Card{{ID: 40, Name: "Levi Ackerman", Rarity: 3, Value: 1000}}

	sent := f.run(toMember("/trade Gojo = Levi", vali))
	offer := last(sent)
	if len(offer.buttons) != 1 || !strings.Contains(offer.text, "Beradi: 👑 **Gojo Satoru**") || !strings.Contains(offer.text, "Oladi: 🔥 **Levi Ackerman**") {
		t.Fatalf("offer = %+v", offer)
	}
	accept := offer.buttons[0][0].Data
	group := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}
	s := New(f.opts)
	defer s.stop()
	s.Handle(doozPlayer(1, &telegram.User{ID: userID, FirstName: "Bek"}, group, offer.text, accept))
	if edits, _ := buttons.recordedMenus(); len(edits) != 0 || !strings.Contains(buttons.answers[0].text, "sizga emas") {
		t.Fatalf("the offerer's own accept = %q, %+v", edits, buttons.answers)
	}
	s.Handle(doozPlayer(2, vali, group, offer.text, accept))
	s.Handle(doozPlayer(3, vali, group, offer.text, accept))
	edits, _ := buttons.recordedMenus()
	if len(edits) != 1 || !strings.Contains(edits[0], "Almashildi! Vali qabul qildi") {
		t.Fatalf("trade = %q", edits)
	}
	if !strings.Contains(buttons.answers[2].text, "tugagan") {
		t.Errorf("the second accept = %+v", buttons.answers[2])
	}
	if len(book.named[rival]) != 1 || book.named[rival][0].Name != "Gojo Satoru" {
		t.Errorf("Vali holds %+v", book.named[rival])
	}
}
