package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
)

// fakeMessageLog is the group log's memory of who wrote which messages,
// noting what it was asked.
type fakeMessageLog struct {
	ids   map[int64][]int64
	since []time.Time
}

func (f *fakeMessageLog) AuthorMessageIDs(_ context.Context, _, authorID int64, since time.Time, limit int) ([]int64, error) {
	f.since = append(f.since, since)
	ids := f.ids[authorID]
	return ids[:min(len(ids), limit)], nil
}

// purgeEnv is newModerationEnv where the log holds Bek's messages 10 and 11.
func purgeEnv(t *testing.T) (*moderationEnv, *fakeMessageLog) {
	t.Helper()
	env := newModerationEnv(t)
	log := &fakeMessageLog{ids: map[int64][]int64{memberID: {10, 11}}}
	env.opts.Messages = log
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.Tool())
	return env, log
}

// A purge deletes the member's logged messages of the last hour, and the
// one replied to, in one call; it is recorded, and cannot be undone.
func TestPurgeDeletesAMembersRecentMessages(t *testing.T) {
	env, log := purgeEnv(t)

	got := env.moderate(replyingWith(100, bekTarget(), "Maria, buning hammasini o'chir"), `{"action":"purge"}`)

	var result struct{ Said, Note string }
	if !got.OK || json.Unmarshal(got.Result, &result) != nil {
		t.Fatalf("purge = %+v", got)
	}
	if want := "Bajarildi: Bek @bek ning so'nggi 60 daqiqadagi 3 ta xabari o'chirildi; ularni qaytarib bo'lmaydi."; result.Said != want || result.Note != purgeNote {
		t.Errorf("said %q, note %q", result.Said, result.Note)
	}
	if want := []string{fmt.Sprintf("deleteMessages %d 10+3", groupA)}; !slices.Equal(env.telegram.made(), want) {
		t.Errorf("calls = %q, want %q", env.telegram.made(), want)
	}
	if len(log.since) != 1 || !log.since[0].Equal(rulesNow.Add(-time.Hour)) {
		t.Errorf("asked the log since %v, want an hour ago", log.since)
	}
	audit := env.audit.recorded()
	if len(audit) != 1 || audit[0].Action != "purge" || audit[0].TargetUserID != memberID || audit[0].Minutes != 60 || audit[0].TargetText != "3 messages" || audit[0].Undo != nil {
		t.Errorf("audit = %+v", audit)
	}
	if got := env.moderate(undoTurn(101, "qaytar"), `{"action":"undo"}`); got.OK {
		t.Errorf("undo after a purge = %+v, want nothing to undo", got)
	}
}

// Telegram deletes nothing older than 48 hours, so a purge reaches no
// further back; a hundred messages go in each call.
func TestPurgeReachesTwoDaysBackInBatches(t *testing.T) {
	env, log := purgeEnv(t)
	var many []int64
	for id := int64(1); id <= 250; id++ {
		many = append(many, id)
	}
	log.ids[memberID] = many

	env.act(t, replyingWith(100, &Target{UserID: memberID, Name: "Bek @bek"}, "tozala"), `{"action":"purge","minutes":100000}`)

	if !log.since[0].Equal(rulesNow.Add(-48 * time.Hour)) {
		t.Errorf("asked the log since %v, want two days ago", log.since[0])
	}
	want := []string{
		fmt.Sprintf("deleteMessages %d 1+100", groupA), fmt.Sprintf("deleteMessages %d 101+100", groupA), fmt.Sprintf("deleteMessages %d 201+50", groupA),
	}
	if got := env.telegram.made(); !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

// A purge Telegram stops in the middle says how many went; refused at once,
// it is not recorded, so it may be asked again.
func TestPurgeThatTelegramStops(t *testing.T) {
	env, log := purgeEnv(t)
	var many []int64
	for id := int64(1); id <= 150; id++ {
		many = append(many, id)
	}
	log.ids[memberID] = many
	env.telegram.err, env.telegram.failAfter = errors.New("connection reset"), 1

	got := env.moderate(replyingWith(100, bekTarget(), "tozala"), `{"action":"purge"}`)
	var result struct{ Said, Note string }
	if !got.OK || json.Unmarshal(got.Result, &result) != nil || result.Note != partlyNote || !strings.Contains(result.Said, " 100 ta xabari") {
		t.Errorf("purge = %+v %s, want 100 reported as deleted", got, got.Result)
	}

	env, _ = purgeEnv(t)
	env.telegram.err = &telegram.APIError{Code: 400, Description: "Bad Request: message can't be deleted"}
	expectRefusal(t, env.moderate(replyingWith(101, bekTarget(), "tozala"), `{"action":"purge"}`), "forbidden", "Telegram refused")
	if audit := env.audit.recorded(); len(audit) != 0 {
		t.Errorf("audit = %+v, want the refused purge forgotten", audit)
	}
}

// A purge needs its own words, as a deletion does, the delete right, and a
// log with something in it.
func TestPurgeIsRefusedWithoutWordsRightsOrMessages(t *testing.T) {
	env, _ := purgeEnv(t)
	expectRefusal(t, env.moderate(replyingWith(100, bekTarget(), "buni o'chir"), `{"action":"purge"}`), "forbidden", "does not ask for this")

	env.chat.set(userID, admin(telegram.RightRestrictMembers))
	expectRefusal(t, env.moderate(replyingWith(101, bekTarget(), "tozala"), `{"action":"purge"}`), "forbidden", "delete messages")
	env.nothingActed(t)

	env, _ = purgeEnv(t)
	expectRefusal(t, env.moderate(replyingWith(102, &Target{UserID: 9090, Name: "Quiet"}, "tozala"), `{"action":"purge"}`), "forbidden", "no message of theirs")
	env.nothingActed(t)

	env = newModerationEnv(t)
	expectRefusal(t, env.moderate(replyingWith(103, bekTarget(), "tozala"), `{"action":"purge"}`), "forbidden", "no log")
	env.nothingActed(t)
}

// A spammer just banned can still have their messages purged, tagged as
// well as replied to.
func TestPurgeReachesABannedMember(t *testing.T) {
	env, _ := purgeEnv(t)
	env.chat.set(memberID, telegram.ChatMember{Status: "kicked", User: &telegram.User{ID: memberID, FirstName: "Bek", Username: "bek"}})
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 100, MessageID: 100,
		Mentions: []Mention{{UserID: memberID}}, Words: "@bek ning hammasini o'chir"}, &Sink{})

	env.act(t, ctx, `{"action":"purge"}`)

	if want := []string{fmt.Sprintf("deleteMessages %d 10+2", groupA)}; !slices.Equal(env.telegram.made(), want) {
		t.Errorf("calls = %q, want %q", env.telegram.made(), want)
	}
}
