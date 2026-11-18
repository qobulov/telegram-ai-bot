package service

import (
	"strings"
	"testing"
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

// "/help mod" is the moderation part of the help alone.
func TestHelpSections(t *testing.T) {
	f := newFixture()
	sent := f.run(textFrom(userID, "/help mod"))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "Moderatsiya") || strings.Contains(last, "tictactoe") {
		t.Errorf("/help mod = %q", last)
	}
	sent = f.run(textFrom(userID, "/help"))
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
