package service

import (
	"errors"
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// /rights lists the commands a rank opens; with no rank it says so.
func TestRights(t *testing.T) {
	f := newFixture()
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{20: 2}, overrides: map[string]int{}}
	sent := f.run(from(20, "/rights", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "Darajangiz: 2") || !strings.Contains(last, "qadash") || strings.Contains(last, "Amnistiya") {
		t.Errorf("rank 2's rights = %q", last)
	}
	sent = f.run(from(21, "/rights", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "daraja yo'q") {
		t.Errorf("no rank's rights = %q", last)
	}
}

// A Telegram admin's /rights lists the commands their own admin rights
// open, those every admin has, and what their rank adds that the rights do
// not; when the rights cannot be read it says so in one line.
func TestRightsOfATelegramAdmin(t *testing.T) {
	const admin, ranked = int64(30), int64(31)
	f := newFixture()
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{ranked: 3}, overrides: map[string]int{}}
	members := &fakeMembers{
		admins: map[int64]bool{admin: true, ranked: true},
		rights: map[int64][]string{admin: {telegram.RightDeleteMessages, telegram.RightPinMessages}, ranked: {telegram.RightPinMessages}},
	}
	f.opts.Members = members

	last := lastText(f.run(from(admin, "/rights", 0)))
	for _, want := range []string{"Siz Telegram adminisiz. Sizga ochiq buyruqlar:", "/del", "/pin, /unpin", "/banlist", "/ignore"} {
		if !strings.Contains(last, want) {
			t.Errorf("admin's rights = %q, want %q", last, want)
		}
	}
	for _, not := range []string{"/ban,", "/mute,", "/promote", "/tag", "Darajangiz bilan"} {
		if strings.Contains(last, not) {
			t.Errorf("admin's rights = %q, has %q", last, not)
		}
	}

	last = lastText(f.run(from(ranked, "/rights", 0)))
	for _, want := range []string{"darajangiz: 3", "/pin, /unpin", "Darajangiz bilan yana:", "Ban va bandan chiqarish", "Xabarni o'chirish"} {
		if !strings.Contains(last, want) {
			t.Errorf("ranked admin's rights = %q, want %q", last, want)
		}
	}
	if strings.Contains(last, "• Xabarni qadash") {
		t.Errorf("ranked admin's rights = %q, repeat pinning, which their right opens", last)
	}

	members.rightsErr = errors.New("telegram is down")
	if last = lastText(f.run(from(admin, "/rights", 0))); !strings.Contains(last, "o'z huquqlaringiz doirasidagi") {
		t.Errorf("rights unread = %q, want the one line", last)
	}
}

func lastText(sent []sentMessage) string {
	if len(sent) == 0 {
		return ""
	}
	return sent[len(sent)-1].text
}

// "/help mod" is the moderation part of the help alone.
func TestHelpSections(t *testing.T) {
	f := newFixture()
	sent := f.run(textFrom(userID, "/help mod"))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "Moderatsiya") || strings.Contains(last, "/about") {
		t.Errorf("/help mod = %q", last)
	}
	sent = f.run(inGroup(groupMessage("/help@MyBot")))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "/help mod") {
		t.Errorf("/help = %q", last)
	}
}

// /myinvites counts the asker's own invites.
func TestMyInvites(t *testing.T) {
	f := newFixture()
	f.opts.Invites = &fakeInviteBook{}
	sent := f.run(from(1, "/myinvites", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "Jami: 17, guruhda qolgan: 15") {
		t.Errorf("/myinvites = %q", last)
	}
}
