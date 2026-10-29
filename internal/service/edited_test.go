package service

import (
	"context"
	"slices"
	"strings"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeSworn knows the messages the swear guard caught, as the store does.
type fakeSworn struct{ caught map[int64]bool }

func (f fakeSworn) SwornIn(_ context.Context, _, messageID int64) (bool, error) {
	return f.caught[messageID], nil
}

func edited(msg *telegram.Message) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, EditedMessage: msg}
}

// A picture swapped for a nude one in an edit is deleted as a new one is.
func TestImageGuardCatchesAPictureSwappedInByAnEdit(t *testing.T) {
	f, guards, moderator := protectFixture(storage.RuleImageGuard)
	guards.unsafe = "nudity"
	msg := messageFrom(5454, "pic", "")
	msg.Photo = []telegram.PhotoSize{{FileID: "photo-big", FileUniqueID: "p-new", FileSize: 40 << 10}}

	sent := f.run(edited(msg))

	if len(sent) != 1 || !strings.Contains(sent[0].text, "nomaqbul rasm o'chirildi (nudity)") {
		t.Fatalf("sent = %+v", sent)
	}
	if !slices.Equal(moderator.deleted, []int64{30}) {
		t.Errorf("deleted %v, want the edited message", moderator.deleted)
	}
}

// A swear edited into a message is warned about, and the model is never
// asked about the edit.
func TestSwearGuardCatchesASwearEditedIn(t *testing.T) {
	f, _ := guardFixture("")

	sent := f.run(edited(swearFrom(swearerID, "Mira, o'zing qo'tog'im").Message))

	if len(sent) != 1 || !strings.HasPrefix(sent[0].text, "@ali_s, guruhda so'kinish mumkin emas") || sent[0].replyTo != 30 {
		t.Fatalf("sent = %+v, want a warning on the edited message", sent)
	}
	if len(f.replier.recorded()) != 0 {
		t.Error("the model was asked")
	}
}

// Editing a message the guard already caught is no second swear: the
// warning stands, and no mute follows.
func TestSwearGuardLetsAnEditOfACaughtMessageBe(t *testing.T) {
	f, restrictor := guardFixture("")
	f.opts.Sworn = fakeSworn{caught: map[int64]bool{30: true}}
	f.opts.Strikes.(*fakeStrikes).actions = []storage.ModerationAction{
		{ChatID: groupID, Action: storage.ActionSwear, TargetUserID: swearerID, TargetMessageID: 30, UpdateID: -1},
	}

	sent := f.run(edited(swearFrom(swearerID, "o'zing qo'tog'im!!").Message))

	if len(sent) != 0 || len(restrictor.recorded()) != 0 {
		t.Errorf("sent %+v, restricted %+v; want the edit left alone", sent, restrictor.recorded())
	}
}

// An edit that calls the bot is not answered, and takes nothing from the
// quotas: only the guards look at edits.
func TestEditedMessagesAreNeverAnswered(t *testing.T) {
	f := newFixture()
	quota := &fakeGroupQuota{}
	f.opts.GroupReplies, f.opts.GroupRatePerMinute = quota, 10

	sent := f.run(edited(groupMessage("@MyBot what is Go?", "@MyBot")))

	if len(sent) != 0 || len(f.replier.recorded()) != 0 {
		t.Errorf("sent %+v to %d model calls; want nothing", sent, len(f.replier.recorded()))
	}
	if asked := quota.asked(); len(asked) != 0 {
		t.Errorf("replies reserved: %+v, want none", asked)
	}
}

// An edit in a private chat is no group's business.
func TestPrivateEditsAreIgnored(t *testing.T) {
	f, guards, moderator := protectFixture(storage.RuleImageGuard)
	guards.unsafe = "nudity"
	msg := &telegram.Message{MessageID: 5, From: &telegram.User{ID: 42}, Chat: telegram.Chat{ID: 42, Type: telegram.ChatTypePrivate},
		Photo: []telegram.PhotoSize{{FileID: "photo", FileSize: 40 << 10}}}

	if sent := f.run(edited(msg)); len(sent) != 0 || len(moderator.deleted) != 0 || guards.pictures != 0 {
		t.Errorf("sent %+v, deleted %v after %d checks", sent, moderator.deleted, guards.pictures)
	}
}
