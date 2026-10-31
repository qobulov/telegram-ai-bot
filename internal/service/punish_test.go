package service

import (
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// punishing is the swear guard's group with an owners' punishment as well.
func punishing(guardOn bool, punishments ...storage.GroupRule) (*fixture, *fakeRestrictor, *fakeModerator) {
	f, restrictor := guardFixture("")
	var rules []storage.GroupRule
	if guardOn {
		rules = append(rules, storage.GroupRule{ID: 1, ChatID: groupID, Kind: storage.RuleSwearGuard})
	}
	f.opts.Rules = &fakeRuleBook{rules: append(rules, punishments...)}
	moderator := &fakeModerator{}
	f.opts.GroupModerator = moderator
	return f, restrictor, moderator
}

func swearPunishment(userID int64, text string) storage.GroupRule {
	return storage.GroupRule{ID: 9, ChatID: groupID, Kind: storage.RuleSwearPunishment, UserID: userID, Text: text, ExpiresAt: time.Now().Add(time.Hour)}
}

// "Bu bola yana so'kinsa ban qil": the member's next swear, even an
// exclamation, bans them instead of the guard's warning.
func TestAMembersPunishmentBansTheirNextSwear(t *testing.T) {
	f, restrictor, moderator := punishing(true, swearPunishment(swearerID, "ban"))
	f.opts.Judge = &fakeJudge{answer: "EXCLAIM"}

	sent := f.run(swearFrom(swearerID, "bla yutqazdik"))

	if !slices.Equal(moderator.banned, []int64{swearerID}) || len(restrictor.recorded()) != 0 {
		t.Fatalf("banned %v, restricted %+v", moderator.banned, restrictor.recorded())
	}
	if len(sent) != 1 || !strings.Contains(sent[0].text, "guruhdan chiqarildi") {
		t.Errorf("sent = %+v", sent)
	}
}

// A punishment holds without the swear guard on, and a mute lasts as long
// as the owner said.
func TestAPunishmentMutesWithoutTheGuard(t *testing.T) {
	f, restrictor, moderator := punishing(false, swearPunishment(swearerID, "mute:3"))
	start := time.Now()

	sent := f.run(swearFrom(swearerID, "o'zing qo'tog'im"))

	done := restrictor.recorded()
	if len(done) != 1 || len(moderator.banned) != 0 || done[0].Until.Sub(start) < 3*time.Hour-time.Minute || done[0].Until.Sub(start) > 3*time.Hour+time.Minute {
		t.Fatalf("restricted %+v, banned %v; want 3 hours' mute", done, moderator.banned)
	}
	if len(sent) != 1 || !strings.Contains(sent[0].text, "3 soat yozolmaydi") {
		t.Errorf("sent = %+v", sent)
	}
}

// One for anyone in the group is for insults: an exclamation gets the
// guard's warning, and another member's punishment is not theirs.
func TestAGroupsPunishmentSparesExclamations(t *testing.T) {
	f, restrictor, moderator := punishing(true, swearPunishment(0, "ban"), swearPunishment(guardAdminID+1, "mute:1"))
	f.opts.Judge = &fakeJudge{answer: "EXCLAIM"}

	sent := f.run(swearFrom(swearerID, "бля"))

	if len(moderator.banned) != 0 || len(restrictor.recorded()) != 0 || len(sent) != 1 || !strings.Contains(sent[0].text, "so'kinmaylik") {
		t.Fatalf("banned %v, restricted %+v, sent %+v; want the warning", moderator.banned, restrictor.recorded(), sent)
	}
	f.opts.Judge = &fakeJudge{answer: "PERSON"}
	f.run(swearFrom(swearerID, "sen jalab"))
	if !slices.Equal(moderator.banned, []int64{swearerID}) {
		t.Errorf("banned %v after an insult, want the swearer", moderator.banned)
	}
}

// An admin may swear, as the owners asked: a punishment ordered for them
// is not carried out, and nothing is said.
func TestAnAdminIsNotPunishedForSwearing(t *testing.T) {
	f, _, moderator := punishing(true, swearPunishment(guardAdminID, "ban"))

	sent := f.run(swearFrom(guardAdminID, "dnx"))

	if len(moderator.banned) != 0 {
		t.Errorf("banned %v", moderator.banned)
	}
	for _, m := range sent {
		if strings.Contains(m.text, "admin") || strings.Contains(m.text, "so'kin") {
			t.Errorf("an admin's swear got %q", m.text)
		}
	}
}

// "18+ tashasa ban ber": a nude picture is deleted and its sender banned;
// an owner's is deleted only.
func TestAPicturePunishmentFollowsTheDeletion(t *testing.T) {
	f, guards, moderator := protectFixture(storage.RuleImageGuard)
	f.opts.Rules = &fakeRuleBook{rules: []storage.GroupRule{
		{ID: 1, ChatID: groupID, Kind: storage.RuleImageGuard},
		{ID: 2, ChatID: groupID, Kind: storage.RulePicturePunishment, Text: "ban", ExpiresAt: time.Now().Add(time.Hour)},
	}}
	guards.unsafe = "nudity"
	member := messageFrom(5454, "pic", "")
	member.Photo = []telegram.PhotoSize{{FileID: "photo-small", FileUniqueID: "p1", FileSize: 40 << 10}}
	owner := messageFrom(9001, ownerUsername, "")
	owner.MessageID = 31
	owner.Photo = []telegram.PhotoSize{{FileID: "photo-big", FileUniqueID: "p2", FileSize: 40 << 10}}

	sent := f.run(inGroup(member), inGroup(owner))

	if !slices.Equal(moderator.deleted, []int64{30, 31}) || !slices.Equal(moderator.banned, []int64{5454}) {
		t.Fatalf("deleted %v, banned %v", moderator.deleted, moderator.banned)
	}
	if len(sent) != 3 || !strings.Contains(sent[1].text, "guruhdan chiqarildi") {
		t.Errorf("sent = %+v", sent)
	}
}
