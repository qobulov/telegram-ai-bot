package tools

import (
	"context"
	"strings"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeUsernames is the group log's memory of who wrote under which
// @username.
type fakeUsernames map[string]int64

func (f fakeUsernames) AuthorByUsername(_ context.Context, _ int64, username string) (int64, error) {
	if id, ok := f[strings.ToLower(username)]; ok {
		return id, nil
	}
	return 0, storage.ErrUnknownAuthor
}

const saidID = int64(6006)

// mentionEnv is newModerationEnv where Said (saidID, @said_05) wrote in
// the group and is still a member.
func mentionEnv(t *testing.T) *moderationEnv {
	t.Helper()
	env := newModerationEnv(t)
	env.chat.set(saidID, telegram.ChatMember{Status: "member", User: &telegram.User{ID: saidID, FirstName: "Said", Username: "said_05"}})
	env.opts.Usernames = fakeUsernames{"said_05": saidID}
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.Tool())
	return env
}

// taggingAs is userID's turn, replying to target (or to nothing), with the
// members their words tag.
func taggingAs(updateID int64, target *Target, mentions ...Mention) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: updateID, MessageID: 50, Target: target, Mentions: mentions}, &Sink{})
}

// ownMessage is a message of the person asking, userID.
func ownMessage() *Target {
	return &Target{MessageID: 49, UserID: userID, Name: "Ali @ali", Text: "@said_05 go mute"}
}

func (env *moderationEnv) mutedOnly(t *testing.T, want int64) {
	t.Helper()
	restrictions := env.telegram.restricted()
	if len(restrictions) != 1 || restrictions[0].UserID != want || restrictions[0].ChatID != groupA {
		t.Errorf("restrictions = %+v, want only user %d muted in the group", restrictions, want)
	}
}

// In production a member tagged someone and replied to their own message:
// the tool saw only the asker. The tagged member is meant, once Telegram
// confirms who that is.
func TestMuteReplyingToOwnMessageActsOnTheTaggedMember(t *testing.T) {
	env := mentionEnv(t)
	promptID := env.ask(t, taggingAs(100, ownMessage(), Mention{Username: "said_05"}), `{"action":"mute","minutes":10}`)
	questions, _ := env.telegram.asked()
	if len(questions) != 1 || !strings.Contains(questions[0].Text, "Said @said_05") || questions[0].ReplyToMessageID != 50 {
		t.Fatalf("question = %+v; it must name the tagged member and reply to the request", questions)
	}
	env.confirm(t, 101, promptID, true)
	env.mutedOnly(t, saidID)
}

func TestMuteByTagWithoutReply(t *testing.T) {
	env := mentionEnv(t)
	env.confirm(t, 101, env.ask(t, taggingAs(100, nil, Mention{Username: "Said_05"}), `{"action":"mute"}`), true)
	env.mutedOnly(t, saidID)
}

// A link to a member without a username carries their ID from Telegram.
func TestMuteByTextMention(t *testing.T) {
	env := mentionEnv(t)
	env.confirm(t, 101, env.ask(t, taggingAs(100, nil, Mention{UserID: saidID}), `{"action":"mute"}`), true)
	env.mutedOnly(t, saidID)
}

// A reply to someone else's message names its author, whatever is tagged.
func TestReplyToAnotherMemberWinsOverATag(t *testing.T) {
	env := mentionEnv(t)
	env.confirm(t, 101, env.ask(t, taggingAs(100, bekTarget(), Mention{Username: "said_05"}), `{"action":"mute"}`), true)
	env.mutedOnly(t, memberID)
}

func TestUncertainTagsActOnNobody(t *testing.T) {
	cases := map[string]struct {
		target   *Target
		mentions []Mention
		setup    func(env *moderationEnv)
		want     string
	}{
		"own message, nothing tagged": {target: ownMessage(), want: immune},
		"nothing replied or tagged":   {want: noReplyTarget},
		"two members tagged": {
			mentions: []Mention{{Username: "said_05"}, {Username: "bek"}},
			want:     manyMentions,
		},
		"a username nobody here wrote under": {
			mentions: []Mention{{Username: "stranger_99"}},
			want:     unknownMention,
		},
		// The log knew the username under this ID, but it changed hands.
		"the logged ID now has another username": {
			mentions: []Mention{{Username: "said_05"}},
			setup: func(env *moderationEnv) {
				env.chat.set(saidID, telegram.ChatMember{Status: "member", User: &telegram.User{ID: saidID, FirstName: "Said", Username: "said_new"}})
			},
			want: unsureMention,
		},
		"Telegram answers about another account": {
			mentions: []Mention{{Username: "said_05"}},
			setup: func(env *moderationEnv) {
				env.chat.set(saidID, telegram.ChatMember{Status: "member", User: &telegram.User{ID: memberID, Username: "said_05"}})
			},
			want: unsureMention,
		},
		"the tagged member left": {
			mentions: []Mention{{Username: "said_05"}},
			setup: func(env *moderationEnv) {
				env.chat.set(saidID, telegram.ChatMember{Status: "left", User: &telegram.User{ID: saidID, Username: "said_05"}})
			},
			want: unsureMention,
		},
		"Telegram cannot say": {
			mentions: []Mention{{UserID: saidID}},
			setup:    func(env *moderationEnv) { env.chat.failing[saidID] = true },
			want:     errLookup.Error(),
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := mentionEnv(t)
			if c.setup != nil {
				c.setup(env)
			}
			got := env.moderate(taggingAs(100, c.target, c.mentions...), `{"action":"mute"}`)
			if got.OK || !strings.Contains(got.Error.Message, c.want) {
				t.Errorf("moderate = %+v, want refused with %q", got, c.want)
			}
			env.nothingDone(t)
		})
	}
}

// Deleting or pinning acts on the message replied to, never on a tag.
func TestTagsDoNotChangeWhichMessageIsDeleted(t *testing.T) {
	env := mentionEnv(t)
	env.confirm(t, 101, env.ask(t, taggingAs(100, ownMessage(), Mention{Username: "said_05"}), `{"action":"delete"}`), true)
	if calls := env.telegram.made(); len(calls) != 1 || !strings.Contains(calls[0], "49") {
		t.Errorf("calls = %q, want the own message 49 deleted", calls)
	}
}
