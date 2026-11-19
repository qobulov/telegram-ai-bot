package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"telegram-ai-bot/internal/storage"
)

type fakeMailer struct {
	sent  []storage.CardMail
	names map[string]int64
}

func (f *fakeMailer) SendCardMail(_ context.Context, m storage.CardMail) (int64, error) {
	f.sent = append(f.sent, m)
	return int64(len(f.sent)), nil
}

func (f *fakeMailer) MemberByUsername(_ context.Context, username string) (int64, error) {
	if id, ok := f.names[strings.ToLower(strings.TrimPrefix(username, "@"))]; ok {
		return id, nil
	}
	return 0, storage.ErrUnknownAuthor
}

// An owner's /gift mails boxes, tokens or crystals to a player found by
// @username or ID and tells them; nobody else may, and a bad order teaches
// the usage.
func TestOwnersGiftToTheMailbox(t *testing.T) {
	f := newFixture()
	f.opts.OwnerIDs = map[int64]bool{premiumOwner: true}
	mail := &fakeMailer{names: map[string]int64{"unbrokeb": 5278507664}}
	f.opts.Mail = mail

	sent := f.run(ownerWrites("/gift @Unbrokeb box 2 Tabriklaymiz!"), ownerWrites("/gift 42 crystals 500"),
		ownerWrites("/gift @nobody box 1"), ownerWrites("/gift @Unbrokeb box 0"), textFrom(userID, "/gift @Unbrokeb box 2"))

	if len(mail.sent) != 2 {
		t.Fatalf("mailed = %+v", mail.sent)
	}
	if m := mail.sent[0]; m.UserID != 5278507664 || m.Box != "box" || m.Boxes != 2 || m.Note != "Tabriklaymiz!" || m.SentBy != premiumOwner {
		t.Errorf("first letter = %+v", m)
	}
	if m := mail.sent[1]; m.UserID != 42 || m.Crystals != 500 || m.Boxes != 0 {
		t.Errorf("second letter = %+v", m)
	}
	var notices, replies []string
	for _, msg := range sent {
		if msg.chatID == premiumOwner {
			replies = append(replies, msg.text)
		} else if strings.Contains(msg.text, "Sizga sovg'a keldi") {
			notices = append(notices, msg.text)
		}
	}
	if len(notices) != 2 || !strings.Contains(notices[0], "2 ta Almaz box") {
		t.Errorf("notices = %q", notices)
	}
	if len(replies) != 4 || !strings.Contains(replies[0], "yuborildi") || replies[2] != fmt.Sprintf(textGiftUnknownUser, "@nobody") || replies[3] != textGiftUsage {
		t.Errorf("owner replies = %q", replies)
	}
}
