package service

import (
	"strings"
	"testing"
	"time"
)

// An owner's /limit sets the group's answers a minute, which the quota then
// uses; /limit default goes back to the configured one.
func TestOwnerSetsTheGroupReplyLimit(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	rules, quota := &fakeRuleBook{}, &fakeGroupQuota{}
	f.opts.Rules, f.opts.GroupReplies, f.opts.GroupRatePerMinute = rules, quota, 6

	sent := f.run(inGroup(fromOwner(groupMessage("/limit@MyBot 10"))))
	expectOneReply(t, sent, "Endi bu guruhda bot daqiqasiga 10 ta javob beradi.")

	question := groupMessage("Mira, salom")
	question.MessageID = 31
	f.run(inGroup(question))
	if calls := quota.calls; len(calls) != 1 || calls[0].PerMinute != 10 {
		t.Errorf("quota calls = %+v, want one at 10 a minute", calls)
	}

	reset := fromOwner(groupMessage("/limit@MyBot default"))
	reset.MessageID = 32
	sent = f.run(inGroup(reset))
	if last := sent[len(sent)-1]; !strings.Contains(last.text, "umumiy limit qaytdi: daqiqasiga 6 ta") || len(rules.all()) != 0 {
		t.Errorf("sent %+v, rules %+v; want the default back", sent, rules.all())
	}
}

// /active off lets a quiet group be answered; /active 3 sets the bar there.
func TestOwnerTurnsTheActiveMembersRule(t *testing.T) {
	for name, c := range map[string]struct {
		command  string
		answered bool
	}{
		"off":         {"/active@MyBot off", true},
		"three":       {"/active@MyBot 3", true},
		"ten":         {"/active@MyBot 10", false},
		"not changed": {"/active@MyBot", false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.OwnerUsernames = []string{ownerUsername}
			f.opts.Rules = &fakeRuleBook{}
			f.opts.MinActiveMembers, f.opts.ActiveWindow, f.opts.ActiveCounter = 5, 72*time.Hour, &fakeActives{count: 4}

			f.run(inGroup(fromOwner(groupMessage(c.command))))
			question := groupMessage("Mira, salom")
			question.MessageID = 31
			f.run(inGroup(question))

			if calls := len(f.replier.recorded()); (calls == 1) != c.answered {
				t.Errorf("model calls = %d, want answered %v", calls, c.answered)
			}
		})
	}
}

// To anyone but an owner the commands do not exist, and nothing changes.
func TestSettingsAreForOwnersOnly(t *testing.T) {
	f := newFixture()
	rules := &fakeRuleBook{}
	f.opts.Rules = rules

	sent := f.run(inGroup(asAdmin(groupMessage("/limit@MyBot 50"))))

	if len(sent) != 1 || strings.Contains(sent[0].text, "50") || len(rules.all()) != 0 {
		t.Errorf("sent %+v, rules %+v; want the unknown-command reply and nothing saved", sent, rules.all())
	}
}

// A number out of range is refused with how to write it.
func TestSettingsRefuseABadNumber(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	rules := &fakeRuleBook{}
	f.opts.Rules = rules

	sent := f.run(inGroup(fromOwner(groupMessage("/limit@MyBot 500"))))

	if len(sent) != 1 || !strings.Contains(sent[0].text, "1 dan 60 gacha") || len(rules.all()) != 0 {
		t.Errorf("sent %+v, rules %+v", sent, rules.all())
	}
}
