package tools

import (
	"context"
	"encoding/json"
	"errors"
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
	owner := ownerReplying(groupA, nil)

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

func TestRulesToolReportsTooManyInstructions(t *testing.T) {
	store := &fakeRules{setErr: storage.ErrTooManyRules}
	registry := rulesRegistry(t, store, fakeAdmins{})

	env := rules(registry, ownerReplying(groupA, nil), `{"action":"add_instruction","text":"one more"}`)

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
		if env := rules(registry, ownerReplying(groupA, nil), args); !env.OK {
			t.Errorf("%.60s: %+v", args, env)
		}
	}
}
