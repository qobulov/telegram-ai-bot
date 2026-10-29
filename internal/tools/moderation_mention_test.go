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
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: updateID, MessageID: 50, Target: target, Mentions: mentions, Words: askingForAll}, &Sink{})
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
	said := env.act(t, taggingAs(100, ownMessage(), Mention{Username: "said_05"}), `{"action":"mute","minutes":10}`)
	if !strings.Contains(said, "Said @said_05") {
		t.Errorf("said %q; it must name the tagged member", said)
	}
	env.mutedOnly(t, saidID)
}

func TestMuteByTagWithoutReply(t *testing.T) {
	env := mentionEnv(t)
	env.act(t, taggingAs(100, nil, Mention{Username: "Said_05"}), `{"action":"mute"}`)
	env.mutedOnly(t, saidID)
}

// A link to a member without a username carries their ID from Telegram.
func TestMuteByTextMention(t *testing.T) {
	env := mentionEnv(t)
	env.act(t, taggingAs(100, nil, Mention{UserID: saidID}), `{"action":"mute"}`)
	env.mutedOnly(t, saidID)
}

// A reply to someone else's message names its author, whatever is tagged.
func TestReplyToAnotherMemberWinsOverATag(t *testing.T) {
	env := mentionEnv(t)
	env.act(t, taggingAs(100, bekTarget(), Mention{Username: "said_05"}), `{"action":"mute"}`)
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
			env.nothingActed(t)
		})
	}
}

// Deleting or pinning acts on the message replied to, never on a tag.
func TestTagsDoNotChangeWhichMessageIsDeleted(t *testing.T) {
	env := mentionEnv(t)
	env.act(t, taggingAs(100, ownMessage(), Mention{Username: "said_05"}), `{"action":"delete"}`)
	if calls := env.telegram.made(); len(calls) != 1 || !strings.Contains(calls[0], "49") {
		t.Errorf("calls = %q, want the own message 49 deleted", calls)
	}
}

const azamatID = int64(7007)

// namingEnv is mentionEnv where the bot has seen Azamat, without a tag in
// the request, among others.
func namingEnv(t *testing.T, seen ...storage.GroupMember) *moderationEnv {
	t.Helper()
	env := mentionEnv(t)
	env.chat.set(azamatID, telegram.ChatMember{Status: "restricted", User: &telegram.User{ID: azamatID, FirstName: "Azamat 아자마트", Username: "mkmillioner8"}})
	roster := append([]storage.GroupMember{
		{ChatID: groupA, UserID: saidID, FirstName: "Said", Username: "said_05"},
		{ChatID: groupA, UserID: azamatID, FirstName: "Azamat 아자마트", Username: "mkmillioner8"},
	}, seen...)
	env.opts.Roster = &fakeRoster{members: roster}
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.Tool())
	return env
}

// sayingAs is userID's turn, replying to target (or to nothing), in words.
func sayingAs(target *Target, words string) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 100, MessageID: 50, Target: target, Words: words}, &Sink{})
}

// In production "Unda azamatni mute qil 15 min ga" was answered with a
// request to tag @mkmillioner8, whom the bot itself had found.
func TestMuteAMemberNamedInWords(t *testing.T) {
	for _, member := range []string{"Azamat", "@mkmillioner8", "azamat 아자마트"} {
		t.Run(member, func(t *testing.T) {
			env := namingEnv(t)
			said := env.act(t, sayingAs(nil, "Unda azamatni mute qil 15 min ga"), `{"action":"mute","minutes":15,"member":"`+member+`"}`)
			if !strings.Contains(said, "@mkmillioner8") {
				t.Errorf("said %q; it must name Azamat", said)
			}
			env.mutedOnly(t, azamatID)
		})
	}
	env := namingEnv(t)
	env.act(t, sayingAs(ownMessage(), "mkmillioner8 ni mute qil"), `{"action":"mute","member":"mkmillioner8"}`)
	env.mutedOnly(t, azamatID)
}

func TestNamesThatFitNobodyOrManyActOnNobody(t *testing.T) {
	cases := map[string]struct {
		words, member string
		seen          []storage.GroupMember
		want          string
	}{
		// The model cannot pick someone the asker did not name.
		"not in their words":      {words: "Saidni mute qil", member: "Azamat", want: unnamedMember},
		"a longer name":           {words: "Azamatbekni mute qil", member: "Azamat", want: unnamedMember},
		"nobody seen by the name": {words: "Jasurni mute qil", member: "Jasur", want: unnamedMember},
		"two Azamats": {words: "Azamatni mute qil", member: "Azamat", want: manyNamed,
			seen: []storage.GroupMember{{ChatID: groupA, UserID: 8008, FirstName: "Azamat"}}},
		"two first names Azamat": {words: "Azamatni mute qil", member: "@mkmillioner8", want: manyNamed,
			seen: []storage.GroupMember{{ChatID: groupA, UserID: 8008, FirstName: "Azamat Q"}}},
		"a name too short": {words: "Alini mute qil", member: "Al", want: unnamedMember,
			seen: []storage.GroupMember{{ChatID: groupA, UserID: 8008, FirstName: "Al"}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := namingEnv(t, c.seen...)
			got := env.moderate(sayingAs(nil, c.words), `{"action":"mute","member":"`+c.member+`"}`)
			if got.OK || !strings.Contains(got.Error.Message, c.want) {
				t.Errorf("moderate = %+v, want refused with %q", got, c.want)
			}
			env.nothingActed(t)
		})
	}
}

// Two members of the same first name are told apart by a username in the
// asker's words.
func TestAUsernameTellsNamesakesApart(t *testing.T) {
	env := namingEnv(t, storage.GroupMember{ChatID: groupA, UserID: 8008, FirstName: "Azamat"})
	env.act(t, sayingAs(nil, "Azamat mkmillioner8 ni mute qil"), `{"action":"mute","member":"mkmillioner8"}`)
	env.mutedOnly(t, azamatID)
}
