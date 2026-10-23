package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// fakeRules keeps rules in memory the way the store does: an ignore or an
// address replaces the one before it for the same person.
type fakeRules struct {
	mu     sync.Mutex
	rules  []storage.GroupRule
	nextID int64
	setErr error
}

func (f *fakeRules) SetGroupRule(_ context.Context, r storage.GroupRule) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return 0, f.setErr
	}
	if r.Kind != storage.RuleInstruction {
		f.remove(func(old storage.GroupRule) bool {
			return old.ChatID == r.ChatID && old.Kind == r.Kind && old.UserID == r.UserID
		})
	}
	f.nextID++
	r.ID = f.nextID
	f.rules = append(f.rules, r)
	return r.ID, nil
}

func (f *fakeRules) GroupRules(_ context.Context, chatID int64) ([]storage.GroupRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []storage.GroupRule
	for _, r := range f.rules {
		if r.ChatID == chatID || r.ChatID == 0 {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRules) RemoveGroupRule(_ context.Context, chatID, id int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.remove(func(r storage.GroupRule) bool { return r.ID == id && (r.ChatID == chatID || r.ChatID == 0) }), nil
}

func (f *fakeRules) RemoveIgnore(_ context.Context, chatID, userID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.remove(func(r storage.GroupRule) bool {
		return r.ChatID == chatID && r.Kind == storage.RuleIgnore && r.UserID == userID
	}), nil
}

// remove drops the rules that match and reports whether there were any. The
// caller holds the lock.
func (f *fakeRules) remove(match func(storage.GroupRule) bool) bool {
	kept := f.rules[:0]
	for _, r := range f.rules {
		if !match(r) {
			kept = append(kept, r)
		}
	}
	removed := len(kept) < len(f.rules)
	f.rules = kept
	return removed
}

func (f *fakeRules) all() []storage.GroupRule {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]storage.GroupRule(nil), f.rules...)
}

// fakeAdmins answers which members administer a group.
type fakeAdmins struct {
	admins map[int64]bool
	err    error
}

func (f fakeAdmins) IsGroupAdmin(_ context.Context, _, userID int64) (bool, error) {
	return f.admins[userID], f.err
}

const (
	rulesBotID = int64(777)
	memberID   = int64(5005)
)

// rulesNow is a Friday afternoon in Tashkent.
var rulesNow = time.Date(2026, 10, 16, 15, 4, 0, 0, tashkent)

func rulesRegistry(t *testing.T, store *fakeRules, admins fakeAdmins) *Registry {
	t.Helper()
	return mustRegistry(t, RulesTool(RulesOptions{
		Store:    store,
		Admins:   admins,
		BotID:    rulesBotID,
		Location: tashkent,
		Now:      func() time.Time { return rulesNow },
	}))
}

// ownerReplying is an owner's turn in chatID, replying to target, if any.
func ownerReplying(chatID int64, target *Target) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: chatID, Owner: true, Human: true, UpdateID: 1, MessageID: 30, Target: target}, &Sink{})
}

// ownerSaying is an owner's turn in chatID whose own message is words.
func ownerSaying(chatID int64, words string) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: chatID, Owner: true, Human: true, UpdateID: 1, MessageID: 30, Words: words}, &Sink{})
}

func bekTarget() *Target {
	return &Target{MessageID: 29, UserID: memberID, Name: "Bek @bek", Text: "you're slow"}
}

func rules(registry *Registry, ctx context.Context, args string) envelope {
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, userID, "group_rules", args)), &env)
	return env
}

func TestRulesToolIgnoresTheRepliedMember(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})

	env := rules(registry, ownerReplying(groupA, bekTarget()), `{"action":"ignore"}`)

	if !env.OK || string(env.Result) != `{"id":1,"ignored":"Bek @bek","saved":true,"until":"17 Oct 15:04"}` {
		t.Fatalf("ignore = %+v %s", env, env.Result)
	}
	want := storage.GroupRule{
		ID: 1, ChatID: groupA, Kind: storage.RuleIgnore, UserID: memberID, UserName: "Bek @bek",
		SetBy: userID, ExpiresAt: rulesNow.Add(24 * time.Hour),
	}
	if got := store.all(); len(got) != 1 || got[0] != want {
		t.Errorf("rules = %+v\nwant     [%+v]", got, want)
	}

	if env := rules(registry, ownerReplying(groupA, bekTarget()), `{"action":"ignore","hours":2}`); !env.OK {
		t.Fatalf("ignore again = %+v", env)
	}
	if got := store.all(); len(got) != 1 || !got[0].ExpiresAt.Equal(rulesNow.Add(2*time.Hour)) {
		t.Errorf("rules = %+v, want one ignore, for 2 hours", got)
	}

	env = rules(registry, ownerReplying(groupA, bekTarget()), `{"action":"unignore"}`)
	if !env.OK || string(env.Result) != `{"member":"Bek @bek","removed":true}` || len(store.all()) != 0 {
		t.Errorf("unignore = %s, rules = %+v", env.Result, store.all())
	}
}

// The owners' real orders ("javob berma dedim") were replies to the bot's
// answer to the member they meant: that member is whom they mean.
func TestRulesToolIgnoresWhomTheBotsMessageAnswered(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})
	botsAnswer := &Target{MessageID: 31, UserID: rulesBotID, Name: "Mira", IsBot: true, Answered: bekTarget()}

	env := rules(registry, ownerReplying(groupA, botsAnswer), `{"action":"ignore"}`)

	if !env.OK || !strings.Contains(string(env.Result), `"ignored":"Bek @bek"`) {
		t.Fatalf("ignore = %+v %s", env, env.Result)
	}
	if got := store.all(); len(got) != 1 || got[0].UserID != memberID {
		t.Errorf("rules = %+v, want Bek ignored", got)
	}
	env = rules(registry, ownerReplying(groupA, botsAnswer), `{"action":"unignore"}`)
	if !env.OK || string(env.Result) != `{"member":"Bek @bek","removed":true}` {
		t.Errorf("unignore = %+v %s", env, env.Result)
	}

	// When the bot cannot tell whom its message answered, the refusal says
	// what to do instead, rather than that the bot cannot be ignored.
	unknown := &Target{MessageID: 31, UserID: rulesBotID, Name: "Mira", IsBot: true}
	for _, action := range []string{"ignore", "unignore"} {
		env = rules(registry, ownerReplying(groupA, unknown), `{"action":"`+action+`"}`)
		if env.OK || !strings.Contains(env.Error.Message, "replied to my own message") {
			t.Errorf("%s replying to an unknown answer = %+v", action, env)
		}
	}
	// Whom it answered is held to the same rules as a member replied to.
	owners := &Target{MessageID: 31, UserID: rulesBotID, Name: "Mira", IsBot: true, Answered: &Target{UserID: 9002, Name: "Muhammad", Owner: true}}
	if env = rules(registry, ownerReplying(groupA, owners), `{"action":"ignore"}`); env.OK || !strings.Contains(env.Error.Message, "cannot be ignored") {
		t.Errorf("ignoring an owner the bot answered = %+v", env)
	}
}

func TestRulesToolRefusesOwnersAdminsAndTheBot(t *testing.T) {
	cases := map[string]struct {
		target *Target
		admins fakeAdmins
		code   string
	}{
		"no reply":      {nil, fakeAdmins{}, "invalid_arguments"},
		"the bot":       {&Target{MessageID: 29, UserID: rulesBotID, Name: "Mira", IsBot: true}, fakeAdmins{}, "invalid_arguments"},
		"an owner":      {&Target{MessageID: 29, UserID: 9002, Name: "Muhammad", Owner: true}, fakeAdmins{}, "invalid_arguments"},
		"a group admin": {bekTarget(), fakeAdmins{admins: map[int64]bool{memberID: true}}, "invalid_arguments"},
		// Messages posted on behalf of a chat come from Telegram's accounts;
		// an anonymous admin's would otherwise pass for a member's.
		"a linked channel's post": {&Target{MessageID: 29, UserID: 777000, Name: "Telegram"}, fakeAdmins{}, "invalid_arguments"},
		"an anonymous admin":      {&Target{MessageID: 29, UserID: 1087968824, Name: "Group @GroupAnonymousBot", IsBot: true}, fakeAdmins{}, "invalid_arguments"},
		"a channel":               {&Target{MessageID: 29, UserID: 136817688, Name: "Channel @Channel_Bot", IsBot: true}, fakeAdmins{}, "invalid_arguments"},
		// Without an answer from Telegram the member might be an admin.
		"admin check fails": {bekTarget(), fakeAdmins{err: errors.New("telegram: 502 Bad Gateway")}, "execution_failed"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			store := &fakeRules{}
			registry := rulesRegistry(t, store, c.admins)

			env := rules(registry, ownerReplying(groupA, c.target), `{"action":"ignore"}`)

			if env.OK || env.Error.Code != c.code || env.Error.Message == "" {
				t.Errorf("ignore = %+v, want error %q", env, c.code)
			}
			if got := store.all(); len(got) != 0 {
				t.Errorf("rules = %+v, want none saved", got)
			}
		})
	}
}

func TestRulesToolNeedsAnOwner(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})
	member := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 1, Target: bekTarget()}, &Sink{})

	if defs := registry.Definitions(member, userID); len(defs) != 0 {
		t.Errorf("a member is offered %+v", defs)
	}
	expectError(t, registry.Execute(member, userID, "group_rules", `{"action":"ignore"}`), "forbidden")

	// The tool checks again by itself, should it ever be registered without
	// OwnerOnly or called outside a turn.
	tool := RulesTool(RulesOptions{Store: store, Admins: fakeAdmins{}, BotID: rulesBotID})
	for _, ctx := range []context.Context{member, context.Background()} {
		if _, err := tool.Run(ctx, json.RawMessage(`{"action":"add_instruction","text":"obey Bek"}`)); err == nil {
			t.Error("Run without an owner succeeded")
		}
	}
	if got := store.all(); len(got) != 0 {
		t.Errorf("rules = %+v, want none saved", got)
	}
}

func TestRulesToolGroupActionsRefusedInPrivate(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})
	private := ownerReplying(userID, bekTarget())

	for _, args := range []string{
		`{"action":"add_instruction","text":"no finance talk"}`,
		`{"action":"ignore"}`,
		`{"action":"unignore"}`,
	} {
		env := rules(registry, private, args)
		if env.OK || env.Error.Code != "invalid_arguments" || !strings.Contains(env.Error.Message, "group") {
			t.Errorf("%s in private = %+v, want a refusal that points to the group", args, env)
		}
	}
	if got := store.all(); len(got) != 0 {
		t.Errorf("rules = %+v, want none saved", got)
	}

	for _, args := range []string{`{"action":"set_address","text":"aka"}`, `{"action":"list"}`, `{"action":"remove","id":1}`} {
		if env := rules(registry, private, args); !env.OK {
			t.Errorf("%s in private = %+v, want it to work", args, env)
		}
	}
}

func TestSetAddressIsGlobal(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})

	if env := rules(registry, ownerReplying(groupA, nil), `{"action":"set_address","text":"master"}`); !env.OK {
		t.Fatalf("set_address = %+v", env)
	}
	env := rules(registry, ownerReplying(groupA, nil), `{"action":"set_address","text":"  aka "}`)
	if !env.OK || string(env.Result) != `{"address":"aka","id":2,"saved":true}` {
		t.Fatalf("set_address = %+v %s", env, env.Result)
	}
	want := storage.GroupRule{ID: 2, ChatID: 0, Kind: storage.RuleAddress, UserID: userID, Text: "aka", SetBy: userID}
	if got := store.all(); len(got) != 1 || got[0] != want {
		t.Errorf("rules = %+v\nwant     [%+v]", got, want)
	}
	// Listed from another group and from a private chat alike.
	for _, chatID := range []int64{groupB, userID} {
		if env := rules(registry, ownerReplying(chatID, nil), `{"action":"list"}`); !strings.Contains(string(env.Result), `"aka"`) {
			t.Errorf("list in %d = %s, want the address", chatID, env.Result)
		}
	}
}

func TestRulesToolKeepsInstructions(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})
	owner := ownerSaying(groupA, "Mira, do not talk about finance, and call Bek ‘professor’ from now on")

	env := rules(registry, owner, `{"action":"add_instruction","text":" Do not talk about finance. "}`)
	if !env.OK || string(env.Result) != `{"id":1,"saved":true,"until":"23 Oct 15:04"}` {
		t.Fatalf("add_instruction = %+v %s", env, env.Result)
	}
	want := storage.GroupRule{ID: 1, ChatID: groupA, Kind: storage.RuleInstruction, Text: "Do not talk about finance.", SetBy: userID, ExpiresAt: rulesNow.Add(168 * time.Hour)}
	if got := store.all(); len(got) != 1 || got[0] != want {
		t.Errorf("rules = %+v\nwant     [%+v]", got, want)
	}
	rules(registry, owner, `{"action":"add_instruction","text":"Call Bek 'professor'.","hours":720}`)
	rules(registry, ownerReplying(groupA, bekTarget()), `{"action":"ignore"}`)

	env = rules(registry, owner, `{"action":"list"}`)
	wantList := `{"rules":[` +
		`{"id":1,"kind":"instruction","text":"Do not talk about finance.","until":"23 Oct 15:04"},` +
		`{"id":2,"kind":"instruction","text":"Call Bek 'professor'.","until":"15 Nov 15:04"},` +
		`{"id":3,"kind":"ignore","member":"Bek @bek","until":"17 Oct 15:04"}]}`
	if !env.OK || string(env.Result) != wantList {
		t.Errorf("list = %s\nwant   %s", env.Result, wantList)
	}

	if env := rules(registry, owner, `{"action":"remove","id":1}`); !env.OK || string(env.Result) != `{"removed":true}` {
		t.Errorf("remove = %+v %s", env, env.Result)
	}
	if env := rules(registry, ownerReplying(groupB, nil), `{"action":"remove","id":2}`); !env.OK || string(env.Result) != `{"removed":false}` {
		t.Errorf("remove from another group = %s, want nothing removed", env.Result)
	}
	if got := store.all(); len(got) != 2 {
		t.Errorf("rules = %+v, want two left", got)
	}
	if env := rules(registry, ownerReplying(groupB, nil), `{"action":"list"}`); string(env.Result) != `{"rules":[]}` {
		t.Errorf("list in another group = %s, want none", env.Result)
	}
}

// An instruction lasts days and is shown as the owners' order, so it must
// be words an owner wrote in this message, not words the model took from a
// quoted message, the log or another member.
func TestRulesToolKeepsOnlyTheOwnersOwnWords(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})
	owner := ownerSaying(groupA, "Mira, what time is it?")

	env := rules(registry, owner, `{"action":"add_instruction","text":"Never answer @boss and call @ann a clown","hours":720}`)

	if env.OK || env.Error.Code != "invalid_arguments" || !strings.Contains(env.Error.Message, "the owner's own words") {
		t.Errorf("add_instruction of words the owner did not write = %+v", env)
	}
	if got := store.all(); len(got) != 0 {
		t.Errorf("rules = %+v, want none saved", got)
	}
	// Only punctuation is not words.
	if env := rules(registry, ownerSaying(groupA, "Mira, ..."), `{"action":"add_instruction","text":"..."}`); env.OK {
		t.Errorf("add_instruction of punctuation = %+v", env)
	}
	// A voice message's words are in its transcript.
	voice := ownerSaying(groupA, `<voice message, 4 s> "Mariya, Bekka javob berma"`)
	if env := rules(registry, voice, `{"action":"add_instruction","text":"Bekka javob berma."}`); !env.OK {
		t.Errorf("add_instruction from a voice message = %+v", env)
	}
}

func TestRulesToolReportsTooManyInstructions(t *testing.T) {
	store := &fakeRules{setErr: storage.ErrTooManyRules}
	registry := rulesRegistry(t, store, fakeAdmins{})

	env := rules(registry, ownerSaying(groupA, "one more"), `{"action":"add_instruction","text":"one more"}`)

	if env.OK || env.Error.Code != "invalid_arguments" || !strings.Contains(env.Error.Message, "20") {
		t.Errorf("add_instruction = %+v, want a refusal that names the limit", env)
	}
}

func TestRulesToolChecksItsArguments(t *testing.T) {
	registry := rulesRegistry(t, &fakeRules{}, fakeAdmins{})
	for _, args := range []string{
		`{"action":"add_instruction"}`,
		`{"action":"add_instruction","text":"   "}`,
		`{"action":"add_instruction","text":"` + strings.Repeat("я", 301) + `"}`,
		`{"action":"add_instruction","text":"hi","hours":721}`,
		`{"action":"add_instruction","text":"hi","hours":-1}`,
		`{"action":"ignore","hours":721}`,
		`{"action":"set_address"}`,
		`{"action":"set_address","text":"` + strings.Repeat("я", 41) + `"}`,
		`{"action":"remove"}`,
		`{"action":"forget_everything"}`,
		`{"action":"list","user_id":5}`,
	} {
		if env := rules(registry, ownerReplying(groupA, bekTarget()), args); env.OK || env.Error.Code != "invalid_arguments" {
			t.Errorf("%.60s: %+v", args, env)
		}
	}
	// The longest accepted.
	for _, args := range []string{
		`{"action":"add_instruction","text":"` + strings.Repeat("я", 300) + `","hours":720}`,
		`{"action":"set_address","text":"` + strings.Repeat("я", 40) + `"}`,
	} {
		if env := rules(registry, ownerSaying(groupA, strings.Repeat("я", 300)), args); !env.OK {
			t.Errorf("%.60s: %+v", args, env)
		}
	}
}

// An owner's form of address is theirs: another owner neither sees it in
// list nor removes it by its id.
func TestRulesToolKeepsAnOwnersAddressTheirs(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})
	if env := rules(registry, ownerReplying(groupA, nil), `{"action":"set_address","text":"aka"}`); !env.OK {
		t.Fatalf("set_address = %+v", env)
	}

	const otherOwner = int64(9003)
	other := WithTurn(context.Background(), Caller{UserID: otherOwner, ChatID: groupA, Owner: true, Human: true, UpdateID: 2, MessageID: 31}, &Sink{})
	for _, c := range []struct{ args, want string }{
		{`{"action":"list"}`, `{"rules":[]}`},
		{`{"action":"remove","id":1}`, `{"removed":false}`},
	} {
		var env envelope
		_ = json.Unmarshal([]byte(registry.Execute(other, otherOwner, "group_rules", c.args)), &env)
		if !env.OK || string(env.Result) != c.want {
			t.Errorf("another owner's %s = %+v %s, want %s", c.args, env, env.Result, c.want)
		}
	}
	if got := store.all(); len(got) != 1 {
		t.Fatalf("rules = %+v, want the address kept", got)
	}

	if env := rules(registry, ownerReplying(userID, nil), `{"action":"remove","id":1}`); string(env.Result) != `{"removed":true}` {
		t.Errorf("the owner's own remove = %+v %s", env, env.Result)
	}
}

// The longest list there can be fits in the tool's result: every
// instruction a group may have and the form of address, at their longest
// and in the character JSON spends most on, ids of 19 digits, and more
// ignores than are listed, with names longer than any member has.
func TestRulesToolListsTheLongestRules(t *testing.T) {
	ctx := context.Background()
	store := &fakeRules{nextID: math.MaxInt64 - 1000}
	for range storage.MaxInstructions {
		_, _ = store.SetGroupRule(ctx, storage.GroupRule{
			ChatID: groupA, Kind: storage.RuleInstruction, Text: strings.Repeat("<", maxInstructionRunes),
			SetBy: userID, ExpiresAt: rulesNow.Add(time.Hour),
		})
	}
	var ignoreIDs []int64
	for i := range maxIgnoresListed + 5 {
		id, _ := store.SetGroupRule(ctx, storage.GroupRule{
			ChatID: groupA, Kind: storage.RuleIgnore, UserID: int64(10000 + i), UserName: strings.Repeat("<", 200),
			SetBy: userID, ExpiresAt: rulesNow.Add(time.Hour),
		})
		ignoreIDs = append(ignoreIDs, id)
	}
	registry := rulesRegistry(t, store, fakeAdmins{})
	owner := ownerReplying(groupA, nil)
	if env := rules(registry, owner, `{"action":"set_address","text":"`+strings.Repeat("<", maxAddressRunes)+`"}`); !env.OK {
		t.Fatalf("set_address = %+v", env)
	}

	raw := registry.Execute(owner, userID, "group_rules", `{"action":"list"}`)

	var env envelope
	_ = json.Unmarshal([]byte(raw), &env)
	if !env.OK {
		t.Fatalf("list = %+v", env)
	}
	if len(raw) <= maxOutputBytes {
		t.Errorf("the list is %d bytes; the test means to exceed the default limit of %d", len(raw), maxOutputBytes)
	}
	var list ruleList
	if err := json.Unmarshal(env.Result, &list); err != nil {
		t.Fatalf("list result: %v", err)
	}
	if want := storage.MaxInstructions + maxIgnoresListed + 1; len(list.Rules) != want {
		t.Fatalf("listed %d rules, want %d", len(list.Rules), want)
	}
	if want := "5 older ignores left out; unignore works on them too"; list.Note != want {
		t.Errorf("note = %q, want %q", list.Note, want)
	}
	// The newest ignores are the ones listed, each name cut short.
	first := list.Rules[storage.MaxInstructions]
	if first.ID != ignoreIDs[5] || first.Member != strings.Repeat("<", maxListedNameRunes)+"…" {
		t.Errorf("first ignore listed = %+v, want id %d with its name cut", first, ignoreIDs[5])
	}
	if last := list.Rules[len(list.Rules)-1]; last.Kind != storage.RuleAddress {
		t.Errorf("last = %+v, want the address", last)
	}
}

func TestRulesToolListsIgnoresUpToTheLimit(t *testing.T) {
	for _, ignores := range []int{maxIgnoresListed - 1, maxIgnoresListed} {
		t.Run(fmt.Sprint(ignores), func(t *testing.T) {
			store := &fakeRules{}
			for i := range ignores {
				_, _ = store.SetGroupRule(context.Background(), storage.GroupRule{ChatID: groupA, Kind: storage.RuleIgnore, UserID: int64(10000 + i), UserName: "Bek"})
			}
			env := rules(rulesRegistry(t, store, fakeAdmins{}), ownerReplying(groupA, nil), `{"action":"list"}`)

			var list ruleList
			_ = json.Unmarshal(env.Result, &list)
			if !env.OK || len(list.Rules) != ignores || list.Note != "" {
				t.Errorf("list = %d rules, note %q; want all %d and no note", len(list.Rules), list.Note, ignores)
			}
		})
	}
}

// In production an owner said "uni Imom buva deb chaqir" in reply to a
// member, and the order was saved as "uni ...": later turns could not tell
// whom it meant. The member replied to is now saved with it.
func TestRulesToolTiesAnInstructionToTheMemberItIsAbout(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})
	turn := func(target *Target) context.Context {
		return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: 1, MessageID: 30, Target: target, Words: "uni Imom buva deb chaqir"}, &Sink{})
	}
	botsAnswer := &Target{MessageID: 31, UserID: rulesBotID, Name: "Mira", IsBot: true, Answered: bekTarget()}

	for name, target := range map[string]*Target{"the member's message": bekTarget(), "the bot's answer to them": botsAnswer} {
		store.rules = nil
		env := rules(registry, turn(target), `{"action":"add_instruction","text":"uni Imom buva deb chaqir","about_member":true}`)
		if !env.OK || !strings.Contains(string(env.Result), `"about":"Bek @bek"`) {
			t.Errorf("%s: add_instruction = %+v %s", name, env, env.Result)
		}
		if got := store.all(); len(got) != 1 || got[0].UserID != memberID || got[0].UserName != "Bek @bek" {
			t.Errorf("%s: rules = %+v, want the order tied to Bek", name, got)
		}
	}
	env := rules(registry, turn(bekTarget()), `{"action":"list"}`)
	if !strings.Contains(string(env.Result), `"kind":"instruction","text":"uni Imom buva deb chaqir","member":"Bek @bek"`) {
		t.Errorf("list = %s, want the member named", env.Result)
	}

	// Not replying to the member, or replying to the owner's own message,
	// nobody can tell whom it means: it is refused with what to do.
	ownMessage := &Target{MessageID: 28, UserID: userID, Name: "Owner", Owner: true}
	for name, target := range map[string]*Target{"no reply": nil, "the owner's own message": ownMessage} {
		store.rules = nil
		env = rules(registry, turn(target), `{"action":"add_instruction","text":"uni Imom buva deb chaqir","about_member":true}`)
		if env.OK || !strings.Contains(env.Error.Message, "reply to that member's own message") || len(store.all()) != 0 {
			t.Errorf("%s: about no one = %+v, rules %+v", name, env, store.all())
		}
	}
	// An order about the group, not about the member replied to, names no one.
	store.rules = nil
	if env = rules(registry, turn(bekTarget()), `{"action":"add_instruction","text":"uni Imom buva deb chaqir"}`); !env.OK || store.all()[0].UserID != 0 {
		t.Errorf("without about_member = %+v, rules %+v", env, store.all())
	}
}

// An owner turns the swear guard on, naming whom a mute tags or leaving it
// to the group's creator, and off again; only in a group, and the
// @username must be the owner's own word.
func TestRulesToolWatchesSwearing(t *testing.T) {
	store := &fakeRules{}
	registry := rulesRegistry(t, store, fakeAdmins{})

	env := rules(registry, ownerSaying(groupA, "Maria, so'kinganlarni mute qil"), `{"action":"watch_swearing"}`)
	if !env.OK || !strings.Contains(string(env.Result), `"mute_tags":"the group's creator"`) {
		t.Fatalf("watch_swearing = %+v %s", env, env.Result)
	}
	env = rules(registry, ownerSaying(groupA, "so'kinsa @Bakxtyorvch ni chaqir"), `{"action":"watch_swearing","text":"@Bakxtyorvch"}`)
	if !env.OK || !strings.Contains(string(env.Result), `"mute_tags":"@Bakxtyorvch"`) {
		t.Fatalf("watch_swearing naming someone = %+v %s", env, env.Result)
	}
	if got := store.all(); len(got) != 1 || got[0].Kind != storage.RuleSwearGuard || got[0].UserName != "@Bakxtyorvch" || got[0].ChatID != groupA {
		t.Errorf("rules = %+v, want one guard tagging @Bakxtyorvch", got)
	}

	for name, c := range map[string]struct {
		ctx  context.Context
		args string
	}{
		"a name the owner did not write": {ownerSaying(groupA, "so'kinganni mute qil"), `{"action":"watch_swearing","text":"@someone_else"}`},
		"not a username":                 {ownerSaying(groupA, "so'kinsa Bobur akani chaqir"), `{"action":"watch_swearing","text":"Bobur aka"}`},
		"in a private chat":              {ownerSaying(9001, "so'kinganni mute qil"), `{"action":"watch_swearing"}`},
	} {
		if env := rules(registry, c.ctx, c.args); env.OK {
			t.Errorf("%s: %+v, want refused", name, env)
		}
	}

	env = rules(registry, ownerSaying(groupA, "endi so'kinishni kuzatma"), `{"action":"stop_watching_swearing"}`)
	if !env.OK || string(env.Result) != `{"removed":true}` || len(store.all()) != 0 {
		t.Errorf("stop_watching_swearing = %s, rules %+v", env.Result, store.all())
	}
}

type fakeGroupList []storage.GroupSummary

func (f fakeGroupList) Groups(context.Context, time.Time, int) ([]storage.GroupSummary, error) {
	return f, nil
}

type fakeMuter struct{ muted map[int64]bool }

func (f *fakeMuter) SetMuted(_ context.Context, chatID int64, muted bool, _ int64) error {
	f.muted[chatID] = muted
	return nil
}

// From a private chat an owner names the group an order is for: "Xz
// chatiga umuman yozma" mutes the bot in Xz, and an instruction is saved
// there. A name that fits several groups, or none, does nothing.
func TestRulesToolActsOnANamedGroupFromPrivate(t *testing.T) {
	store, muter := &fakeRules{}, &fakeMuter{muted: map[int64]bool{}}
	const xz, pub = int64(-1004339638864), int64(-1002903357611)
	registry := mustRegistry(t, RulesTool(RulesOptions{
		Store: store, Admins: fakeAdmins{}, BotID: rulesBotID, Location: tashkent,
		Now:    func() time.Time { return rulesNow },
		Groups: fakeGroupList{{ChatID: xz, Title: "Xz"}, {ChatID: pub, Title: "PUB-Қовоқхона Tursunoff", Username: "pubchat"}, {ChatID: -100777, Title: "Xz fanlar"}},
		Muter:  muter,
	}))
	private := ownerSaying(userID, "Xz chatiga umuman yozma")

	env := rules(registry, private, `{"action":"mute","group":"Xz"}`)
	if !env.OK || !muter.muted[xz] || !strings.Contains(string(env.Result), `"group":"Xz"`) {
		t.Fatalf("mute Xz = %+v %s, muted %v", env, env.Result, muter.muted)
	}
	if env := rules(registry, private, `{"action":"add_instruction","text":"umuman yozma","group":"@pubchat"}`); !env.OK {
		t.Fatalf("instruction for @pubchat = %+v", env)
	}
	if got := store.all(); len(got) != 1 || got[0].ChatID != pub || got[0].Text != "umuman yozma" {
		t.Errorf("rules = %+v, want the instruction in PUB", got)
	}
	if env := rules(registry, private, `{"action":"unmute","group":"қовоқхона"}`); !env.OK || muter.muted[pub] {
		t.Errorf("unmute by part of the title = %+v", env)
	}

	for name, group := range map[string]string{"several": "x", "none": "Afsona"} {
		before := len(muter.muted)
		env := rules(registry, private, `{"action":"mute","group":"`+group+`"}`)
		if env.OK || env.Error.Code != "invalid_arguments" || !strings.Contains(env.Error.Message, "Nothing was done") && !strings.Contains(env.Error.Message, "nothing was done") {
			t.Errorf("%s: %+v, want a refusal", name, env)
		}
		if len(muter.muted) != before {
			t.Errorf("%s: muted %v", name, muter.muted)
		}
	}
	if env := rules(registry, ownerReplying(userID, bekTarget()), `{"action":"ignore","group":"Xz"}`); env.OK {
		t.Errorf("ignore from private = %+v, want it to need a reply in the group", env)
	}
}
