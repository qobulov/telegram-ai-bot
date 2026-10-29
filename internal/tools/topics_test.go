package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// fakeTopics records what it is asked to do to a forum's topics.
type fakeTopics struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeTopics) do(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.err
}

func (f *fakeTopics) CreateForumTopic(_ context.Context, chatID int64, name string, iconColor int) (int64, error) {
	return 42, f.do(fmt.Sprintf("createForumTopic %d %s %d", chatID, name, iconColor))
}

func (f *fakeTopics) EditForumTopic(_ context.Context, chatID, threadID int64, name string) error {
	return f.do(fmt.Sprintf("editForumTopic %d %d %s", chatID, threadID, name))
}

func (f *fakeTopics) CloseForumTopic(_ context.Context, chatID, threadID int64) error {
	return f.do(fmt.Sprintf("closeForumTopic %d %d", chatID, threadID))
}

func (f *fakeTopics) ReopenForumTopic(_ context.Context, chatID, threadID int64) error {
	return f.do(fmt.Sprintf("reopenForumTopic %d %d", chatID, threadID))
}

func (f *fakeTopics) DeleteForumTopic(_ context.Context, chatID, threadID int64) error {
	return f.do(fmt.Sprintf("deleteForumTopic %d %d", chatID, threadID))
}

func (f *fakeTopics) HideGeneralForumTopic(_ context.Context, chatID int64) error {
	return f.do(fmt.Sprintf("hideGeneralForumTopic %d", chatID))
}

func (f *fakeTopics) UnhideGeneralForumTopic(_ context.Context, chatID int64) error {
	return f.do(fmt.Sprintf("unhideGeneralForumTopic %d", chatID))
}

func (f *fakeTopics) made() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// topicManager is an admin who may manage topics, and more.
func topicManager(rights ...string) telegram.ChatMember {
	member := admin(rights...)
	member.CanManageTopics = true
	return member
}

// newTopicsEnv is a moderation env whose groupA is a forum where the bot and
// the person asking may manage topics and delete messages.
func newTopicsEnv(t *testing.T) (*moderationEnv, *fakeTopics) {
	t.Helper()
	env := newModerationEnv(t)
	env.chat.set(rulesBotID, topicManager(telegram.RightDeleteMessages))
	env.chat.set(userID, topicManager(telegram.RightDeleteMessages))
	topics := &fakeTopics{}
	env.registry = mustRegistry(t, env.moderation.TopicsTool(topics))
	return env, topics
}

// inTopic is userID's request in update updateID, written in topic 7 of
// the forum groupA.
func inTopic(updateID int64, words string) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: updateID, MessageID: updateID, Words: words, Forum: true, TopicID: 7}, &Sink{})
}

func (env *moderationEnv) topic(ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var result envelope
	_ = json.Unmarshal([]byte(env.registry.Execute(ctx, caller.UserID, "forum_topic", args)), &result)
	return result
}

// saidOf is what a tool's done result says was done, or "" when it was not
// done.
func saidOf(got envelope) string {
	var result struct {
		Done bool   `json:"done"`
		Said string `json:"said"`
	}
	if !got.OK || json.Unmarshal(got.Result, &result) != nil || !result.Done {
		return ""
	}
	return result.Said
}

// Each action on one topic acts on the topic the request was written in,
// and is recorded with it.
func TestTopicActionsActOnTheRequestsTopic(t *testing.T) {
	env, topics := newTopicsEnv(t)
	cases := []struct {
		words, args, call, said string
	}{
		{"bu mavzuni yop", `{"action":"close"}`, "closeForumTopic -100111 7", "Bajarildi: mavzu yopildi."},
		{"mavzuni qayta och", `{"action":"reopen"}`, "reopenForumTopic -100111 7", "Bajarildi: mavzu qayta ochildi."},
		{"topic nomini Turnir qil", `{"action":"rename","name":"Turnir"}`, "editForumTopic -100111 7 Turnir", "Bajarildi: mavzu nomi «Turnir» bo'ldi."},
		{"удали эту тему", `{"action":"delete"}`, "deleteForumTopic -100111 7", "Bajarildi: mavzu o'chirildi."},
	}
	for i, c := range cases {
		if said := saidOf(env.topic(inTopic(int64(700+i), c.words), c.args)); said != c.said {
			t.Errorf("%s: said %q, want %q", c.args, said, c.said)
		}
		if calls := topics.made(); calls[len(calls)-1] != c.call {
			t.Errorf("%s: last call %q, want %q", c.args, calls[len(calls)-1], c.call)
		}
	}
	audit := env.audit.recorded()
	if len(audit) != 4 || audit[0].Action != "topic_close" || audit[0].TargetMessageID != 7 || audit[3].Action != "topic_delete" {
		t.Errorf("audit = %+v", audit)
	}
}

// A new topic gets the name and colour asked for; General is hidden and
// shown with no topic needed.
func TestTopicsAreCreatedAndGeneralHidden(t *testing.T) {
	env, topics := newTopicsEnv(t)
	general := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 712, MessageID: 712, Words: "General'ni yashir", Forum: true}, &Sink{})

	if said := saidOf(env.topic(inTopic(710, "Turnir degan yangi topic och, qizil bo'lsin"), `{"action":"create","name":"Turnir","color":"red"}`)); said != "Bajarildi: «Turnir» mavzusi ochildi." {
		t.Errorf("create said %q", said)
	}
	if said := saidOf(env.topic(general, `{"action":"hide_general"}`)); said != "Bajarildi: General mavzusi yashirildi." {
		t.Errorf("hide_general said %q", said)
	}
	want := []string{fmt.Sprintf("createForumTopic %d Turnir %d", groupA, 0xFB6F5F), fmt.Sprintf("hideGeneralForumTopic %d", groupA)}
	if got := topics.made(); !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

// Nothing is done outside a forum, in General for one topic's action, when
// the asker's words do not ask for it, or with arguments Telegram would
// refuse.
func TestTopicsRefuseWhatTheyCannotDo(t *testing.T) {
	env, topics := newTopicsEnv(t)
	notForum := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 720, MessageID: 720, Words: "mavzuni yop"}, &Sink{})
	inGeneral := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 721, MessageID: 721, Words: "mavzuni yop", Forum: true}, &Sink{})

	for _, c := range []struct {
		ctx       context.Context
		args, has string
	}{
		{notForum, `{"action":"close"}`, notAForum},
		{inGeneral, `{"action":"close"}`, noTopic},
		{inTopic(722, "salom qalaysan"), `{"action":"delete"}`, notAsked},
		{inTopic(723, "yangi mavzu och"), `{"action":"create"}`, "name must be"},
		{inTopic(724, "yangi mavzu och"), `{"action":"create","name":"x","color":"black"}`, "color must be"},
		{inTopic(725, "mavzuni yop"), `{"action":"archive"}`, "action must be"},
	} {
		if got := env.topic(c.ctx, c.args); got.OK || !strings.Contains(got.Error.Message, c.has) {
			t.Errorf("%s = %+v, want refused with %q", c.args, got, c.has)
		}
	}
	if calls := topics.made(); len(calls) != 0 {
		t.Errorf("Telegram was asked to %q", calls)
	}
	if audit := env.audit.recorded(); len(audit) != 0 {
		t.Errorf("recorded %+v", audit)
	}
}

// The asker and the bot both need the right to manage topics, and to delete
// messages for a topic's deletion; an owner known by ID needs neither.
func TestTopicsCheckRightsWithTelegram(t *testing.T) {
	env, topics := newTopicsEnv(t)
	env.chat.set(userID, admin(telegram.RightDeleteMessages))
	if got := env.topic(inTopic(730, "mavzuni yop"), `{"action":"close"}`); got.OK || !strings.Contains(got.Error.Message, "manage topics") {
		t.Errorf("an admin who cannot manage topics = %+v", got)
	}

	env.chat.set(userID, topicManager())
	if got := env.topic(inTopic(731, "mavzuni o'chir"), `{"action":"delete"}`); got.OK || !strings.Contains(got.Error.Message, "delete messages") {
		t.Errorf("deleting without the right to delete messages = %+v", got)
	}

	env.chat.set(rulesBotID, topicManager())
	owner := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: 732, MessageID: 732, Words: "mavzuni o'chir", Forum: true, TopicID: 7}, &Sink{})
	if got := env.topic(owner, `{"action":"delete"}`); got.OK || !strings.Contains(got.Error.Message, "I don't have the right to delete messages") {
		t.Errorf("deleting when the bot cannot delete messages = %+v", got)
	}

	env.chat.set(userID, telegram.ChatMember{Status: "member"})
	ownerClose := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: 733, MessageID: 733, Words: "mavzuni yop", Forum: true, TopicID: 7}, &Sink{})
	if said := saidOf(env.topic(ownerClose, `{"action":"close"}`)); said == "" {
		t.Errorf("an owner could not close a topic")
	}
	if calls := topics.made(); !slices.Equal(calls, []string{"closeForumTopic -100111 7"}) {
		t.Errorf("calls = %q, want only the owner's close", calls)
	}
}

// An update delivered twice acts once; one Telegram refused may be tried
// again.
func TestTopicActionsActOnceAndForgetRefusals(t *testing.T) {
	env, topics := newTopicsEnv(t)
	env.topic(inTopic(740, "mavzuni yop"), `{"action":"close"}`)
	if got := env.topic(inTopic(740, "mavzuni yop"), `{"action":"close"}`); !got.OK || saidOf(got) != "" {
		t.Errorf("the second delivery = %+v %s, want done before with nothing said", got, got.Result)
	}
	if calls := topics.made(); len(calls) != 1 {
		t.Errorf("calls = %q, want one", calls)
	}

	topics.err = &telegram.APIError{Code: 400, Description: "Bad Request: TOPIC_NOT_MODIFIED"}
	got := env.topic(inTopic(741, "mavzuni qayta och"), `{"action":"reopen"}`)
	if got.OK || !strings.Contains(got.Error.Message, "TOPIC_NOT_MODIFIED") {
		t.Errorf("a refusal = %+v", got)
	}
	if audit := env.audit.recorded(); len(audit) != 1 {
		t.Errorf("audit = %+v, want the refused reopen forgotten", audit)
	}

	topics.err = errors.New("telegram: 502 Bad Gateway")
	env.topic(inTopic(742, "mavzuni yop"), `{"action":"close"}`)
	if audit := env.audit.recorded(); len(audit) != 2 {
		t.Errorf("audit = %+v, want a close Telegram may have done kept", audit)
	}
}

func TestTopicAskedNeedsATopicAndAnAction(t *testing.T) {
	for _, c := range []struct {
		words, action string
		want          bool
	}{
		{"bu topicni yopib qo'y", topicClose, true},
		{"закрой тему", topicClose, true},
		{"yop", topicClose, false},
		{"mavzu haqida gaplashamiz", topicClose, false},
		{"General'ni yashir", topicHideGeneral, true},
		{"umumiy chatni qaytar", topicUnhideGeneral, true},
		{"mavzuni yashir", topicHideGeneral, false},
		{"yangi mavzu yarat", topicCreate, true},
	} {
		if got := topicAsked(c.words, c.action); got != c.want {
			t.Errorf("topicAsked(%q, %s) = %v, want %v", c.words, c.action, got, c.want)
		}
	}
}
