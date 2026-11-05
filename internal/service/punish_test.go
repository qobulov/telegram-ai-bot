package service

import (
	"errors"
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

// "Bu bola yana so'kinsa ban qil": the member's next swear at the bot bans
// them instead of the guard's warning.
func TestAMembersPunishmentBansASwearAtTheBot(t *testing.T) {
	f, restrictor, moderator := punishing(true, swearPunishment(swearerID, "ban"))
	f.opts.Judge = &fakeJudge{answer: "BOT"}

	sent := f.run(swearFrom(swearerID, "Mira, o'zing qo'tog'im"))

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
	f.opts.Judge = &fakeJudge{answer: "BOT"}
	start := time.Now()

	sent := f.run(swearFrom(swearerID, "Mira, o'zing qo'tog'im"))

	done := restrictor.recorded()
	if len(done) != 1 || len(moderator.banned) != 0 || done[0].Until.Sub(start) < 3*time.Hour-time.Minute || done[0].Until.Sub(start) > 3*time.Hour+time.Minute {
		t.Fatalf("restricted %+v, banned %v; want 3 hours' mute", done, moderator.banned)
	}
	if len(sent) != 1 || !strings.Contains(sent[0].text, "3 soat yozolmaydi") {
		t.Errorf("sent = %+v", sent)
	}
}

// Swearing at a person or at nobody is not what a punishment is for, and the
// model is not asked about it: with the guard on it gets the guard's warning.
func TestAPunishmentIsOnlyForSwearingAtTheBot(t *testing.T) {
	for _, order := range []int64{swearerID, 0} {
		for text, warning := range map[string]string{"bla yutqazdik": "so'kinmaylik", "o'zing jalab": "so'kinish mumkin emas"} {
			f, restrictor, moderator := punishing(true, swearPunishment(order, "ban"), swearPunishment(guardAdminID+1, "mute:1"))
			judge := &fakeJudge{answer: "BOT"}
			f.opts.Judge = judge
			sent := f.run(swearFrom(swearerID, text))
			if len(moderator.banned) != 0 || len(restrictor.recorded()) != 0 || len(judge.asked) != 0 || len(sent) != 1 || !strings.Contains(sent[0].text, warning) {
				t.Errorf("%s: banned %v, restricted %+v, asked %d, sent %+v; want the warning", text, moderator.banned, restrictor.recorded(), len(judge.asked), sent)
			}
		}
	}
}

// The chat this was written for: under "mute if they swear again", a member
// answers the bot's own message with a swear no list had. The mute falls in
// code, its notice tells the group, and the model, which said it had muted them
// when nothing had, is not asked.
func TestASwearInReplyToTheBotIsMuted(t *testing.T) {
	f, restrictor, _ := punishing(false, swearPunishment(swearerID, "mute:24"))
	f.opts.Judge = &fakeJudge{answer: "BOT"}
	update := swearFrom(swearerID, "Ybanmisan")
	update.Message.ReplyToMessage = &telegram.Message{MessageID: 29, From: &telegram.User{ID: f.opts.Bot.ID, IsBot: true}, Chat: update.Message.Chat, Text: "Bajarildi, master."}

	sent := f.run(update)

	if done := restrictor.recorded(); len(done) != 1 || done[0].UserID != swearerID {
		t.Fatalf("restrictions = %+v, want the swearer muted", done)
	}
	if len(sent) != 1 || !strings.Contains(sent[0].text, "24 soat yozolmaydi") {
		t.Errorf("sent = %+v, want the mute notice", sent)
	}
	if calls := f.replier.recorded(); len(calls) != 0 {
		t.Errorf("model calls = %d, want none", len(calls))
	}
}

// The model finds a swear at the bot, in a spelling no list has, and only a
// message to the bot from a member under an order is asked about, with the
// guard off; what it calls no swearing at the bot, or cannot judge, punishes
// no one.
func TestAPunishmentAsksTheModelOnlyAboutMessagesToTheBot(t *testing.T) {
	judged := func(judge *fakeJudge, from int64, text string) []telegram.Restriction {
		f, restrictor, _ := punishing(false, swearPunishment(swearerID, "mute:24"))
		f.opts.Judge = judge
		f.run(swearFrom(from, text))
		return restrictor.recorded()
	}
	if got := judged(&fakeJudge{answer: "BOT"}, swearerID, "Mira, ybanmisan"); len(got) != 1 {
		t.Errorf("restrictions = %+v, want the ordered mute", got)
	}
	for name, c := range map[string]struct {
		from int64
		text string
	}{
		"another member": {swearerID + 1, "Mira, ybanmisan"},
		"not to the bot": {swearerID, "ybanmisan"},
		"no words":       {swearerID, " "},
	} {
		judge := &fakeJudge{answer: "BOT"}
		if got := judged(judge, c.from, c.text); len(got) != 0 || len(judge.asked) != 0 {
			t.Errorf("%s: restrictions %+v, asked %d", name, got, len(judge.asked))
		}
	}
	for name, judge := range map[string]*fakeJudge{
		"no swearing":  {answer: "NONE"},
		"at a person":  {answer: "PERSON"},
		"model failed": {err: errors.New("ai: 503")},
	} {
		if got := judged(judge, swearerID, "Mira, arzimaydi"); len(got) != 0 {
			t.Errorf("%s: restrictions = %+v", name, got)
		}
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
