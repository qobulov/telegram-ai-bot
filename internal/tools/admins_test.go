package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// fakeAdminList answers getChatAdministrators and records the chats asked
// about.
type fakeAdminList struct {
	mu     sync.Mutex
	admins []telegram.ChatMember
	err    error
	asked  []int64
}

func (f *fakeAdminList) GetChatAdministrators(_ context.Context, chatID int64) ([]telegram.ChatMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, chatID)
	return f.admins, f.err
}

func (f *fakeAdminList) chats() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.asked...)
}

func adminsRegistry(t *testing.T, list *fakeAdminList) *Registry {
	t.Helper()
	return mustRegistry(t, AdminsTool(AdminsOptions{Telegram: list, BotID: rulesBotID, Owners: map[int64]bool{userID: true}}))
}

func askAdmins(registry *Registry, ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var result envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, caller.UserID, "group_admins", args)), &result)
	return result
}

// adminList is the result of group_admins, its admins left as JSON.
type adminList struct {
	Admins json.RawMessage `json:"admins"`
	Note   string          `json:"note"`
}

func decodeAdmins(t *testing.T, got envelope) adminList {
	t.Helper()
	if !got.OK {
		t.Fatalf("group_admins = %+v, want the list", got)
	}
	var list adminList
	if err := json.Unmarshal(got.Result, &list); err != nil {
		t.Fatalf("result %s: %v", got.Result, err)
	}
	return list
}

func listedAdmin(status string, user telegram.User) telegram.ChatMember {
	return telegram.ChatMember{Status: status, User: &user}
}

func TestAdminsListed(t *testing.T) {
	bek := listedAdmin("administrator", telegram.User{ID: memberID, FirstName: " Bek "})
	bek.CustomTitle = "Moderator"
	list := &fakeAdminList{admins: []telegram.ChatMember{
		listedAdmin("creator", telegram.User{ID: userID, FirstName: "Javohir", Username: "pronium"}),
		bek,
		listedAdmin("administrator", telegram.User{ID: rulesBotID, IsBot: true, FirstName: "Maria", Username: "maria_bot"}),
		listedAdmin("administrator", telegram.User{ID: 888, IsBot: true, FirstName: "Combot", Username: "combot"}),
	}}

	got := decodeAdmins(t, askAdmins(adminsRegistry(t, list), memberAsking(groupA, 100), `{}`))

	want := `[{"name":"Javohir","username":"@pronium","role":"creator","owner":true},` +
		`{"name":"Bek","role":"administrator","title":"Moderator"},` +
		`{"name":"Maria","username":"@maria_bot","role":"administrator","you":true,"bot":true},` +
		`{"name":"Combot","username":"@combot","role":"administrator","bot":true}]`
	if string(got.Admins) != want {
		t.Errorf("admins = %s\nwant     %s", got.Admins, want)
	}
	if got.Note != adminsNote {
		t.Errorf("note = %q, want %q", got.Note, adminsNote)
	}
	// The list is always the group the turn is in.
	if chats := list.chats(); !slices.Equal(chats, []int64{groupA}) {
		t.Errorf("asked about chats %v, want [%d]", chats, groupA)
	}
}

// An anonymous admin chose to hide who they are in the group: the list says
// only that there is one, never who, not even that it is an owner.
func TestAdminsHidesAnonymousAdmins(t *testing.T) {
	creator := listedAdmin("creator", telegram.User{ID: userID, FirstName: "Javohir", Username: "pronium"})
	creator.IsAnonymous, creator.CustomTitle = true, "Ghost"
	hidden := listedAdmin("administrator", telegram.User{ID: 4242, FirstName: "Secret", Username: "hidden_one"})
	hidden.IsAnonymous = true
	list := &fakeAdminList{admins: []telegram.ChatMember{creator, hidden, listedAdmin("administrator", telegram.User{ID: memberID, FirstName: "Bek"})}}

	got := askAdmins(adminsRegistry(t, list), memberAsking(groupA, 100), `{}`)
	admins := decodeAdmins(t, got)

	want := `[{"role":"creator","anonymous":true},{"role":"administrator","anonymous":true},{"name":"Bek","role":"administrator"}]`
	if string(admins.Admins) != want {
		t.Errorf("admins = %s\nwant     %s", admins.Admins, want)
	}
	for _, secret := range []string{"Javohir", "pronium", "Ghost", "Secret", "hidden_one", "4242", `"owner"`} {
		if strings.Contains(string(got.Result), secret) {
			t.Errorf("result %s gives away %q", got.Result, secret)
		}
	}
	if admins.Note != adminsNote+" "+anonymousNote {
		t.Errorf("note = %q, want it to say who an anonymous admin is stays hidden", admins.Note)
	}
}

// Telegram allows a supergroup 50 admins, but the list is bounded all the
// same, names and titles longer than Telegram's are cut, and the list still
// fits in the result. '<' is escaped in JSON, so each takes 6 bytes.
func TestAdminsListIsCut(t *testing.T) {
	list := &fakeAdminList{}
	for i := range 60 {
		member := listedAdmin("administrator", telegram.User{
			ID: int64(1000 + i), FirstName: strings.Repeat("<", 70), Username: fmt.Sprintf("%032d", i),
		})
		member.CustomTitle = strings.Repeat("<", 20)
		list.admins = append(list.admins, member)
	}

	got := decodeAdmins(t, askAdmins(adminsRegistry(t, list), memberAsking(groupA, 100), `{}`))

	var admins []map[string]any
	if err := json.Unmarshal(got.Admins, &admins); err != nil {
		t.Fatal(err)
	}
	if len(admins) != 50 || admins[49]["username"] != "@"+fmt.Sprintf("%032d", 49) {
		t.Errorf("listed %d admins, the last %v; want the first 50", len(admins), admins[len(admins)-1])
	}
	for _, admin := range admins {
		if admin["name"] != strings.Repeat("<", 64)+"…" || admin["title"] != strings.Repeat("<", 16)+"…" {
			t.Fatalf("admin %v, want the name cut to 64 runes and the title to 16", admin)
		}
	}
	if !strings.Contains(got.Note, "10 more admins") {
		t.Errorf("note = %q, want it to say 10 admins were left out", got.Note)
	}
}

func TestAdminsOnlyInAGroup(t *testing.T) {
	list := &fakeAdminList{}
	registry := adminsRegistry(t, list)
	private := WithTurn(context.Background(), Caller{UserID: memberID, ChatID: memberID, Human: true, UpdateID: 100}, &Sink{})

	if got := askAdmins(registry, private, `{}`); got.OK || got.Error.Code != "forbidden" || !strings.Contains(got.Error.Message, "only in a group") {
		t.Errorf("group_admins in a private chat = %+v, want refused", got)
	}
	// Any member of a group may ask, not only admins and owners.
	if defs := registry.Definitions(memberAsking(groupA, 100), memberID); len(defs) != 1 {
		t.Errorf("offered to a member of a group: %+v, want group_admins", defs)
	}
	// The tool checks it too, without the registry.
	tool := AdminsTool(AdminsOptions{Telegram: list})
	for name, ctx := range map[string]context.Context{"private chat": private, "no turn": context.Background()} {
		var denied *DeniedError
		if _, err := tool.Run(ctx, json.RawMessage(`{}`)); !errors.As(err, &denied) {
			t.Errorf("%s: Run = %v, want refused", name, err)
		}
	}
	if chats := list.chats(); len(chats) != 0 {
		t.Errorf("asked Telegram about chats %v", chats)
	}
}

// A member cannot point the tool at another group; an owner can, from
// anywhere, by its chat_id.
func TestAdminsOfAnotherGroupOnlyForOwners(t *testing.T) {
	list := &fakeAdminList{}
	registry := adminsRegistry(t, list)

	got := askAdmins(registry, memberAsking(groupA, 100), `{"chat_id":-100222}`)
	if got.OK || got.Error.Code != "forbidden" {
		t.Errorf("a member naming another group = %+v, want refused", got)
	}
	if chats := list.chats(); len(chats) != 0 {
		t.Errorf("asked Telegram about chats %v", chats)
	}

	ownerPrivate := WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Owner: true, Human: true, UpdateID: 101}, &Sink{})
	if got := askAdmins(registry, ownerPrivate, `{"chat_id":-100222}`); !got.OK {
		t.Errorf("an owner naming a group = %+v, want the list", got)
	}
	if chats := list.chats(); len(chats) != 1 || chats[0] != -100222 {
		t.Errorf("asked Telegram about chats %v, want -100222", chats)
	}
}

// The model gets a short message; the logs get Telegram's reason, so that a
// timeout can be told from a refusal.
func TestAdminsTelegramFailureIsShort(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		logged string
	}{
		"no answer": {errors.New("telegram getChatAdministrators: dial tcp 149.154.167.220:443: i/o timeout"), "i/o timeout"},
		"refused":   {&telegram.APIError{Code: 400, Description: "Bad Request: chat not found"}, "chat not found"},
	} {
		t.Run(name, func(t *testing.T) {
			list := &fakeAdminList{err: tc.err}
			var logs bytes.Buffer
			registry, err := NewRegistry(isAdmin, slog.New(slog.NewTextHandler(&logs, nil)),
				AdminsTool(AdminsOptions{Telegram: list, BotID: rulesBotID}))
			if err != nil {
				t.Fatal(err)
			}

			got := askAdmins(registry, memberAsking(groupA, 100), `{}`)

			if got.OK || got.Error.Code != "execution_failed" || got.Error.Message != adminsUnavailable {
				t.Errorf("group_admins = %+v, want only %q", got, adminsUnavailable)
			}
			if !strings.Contains(logs.String(), tc.logged) || !strings.Contains(logs.String(), "tool=group_admins") {
				t.Errorf("logs = %q, want %q for group_admins", logs.String(), tc.logged)
			}
		})
	}
}

// The list says nothing about rights, so the model must not use it to say
// who may moderate; and only those who wrote in its messages are known to
// be regular members.
func TestAdminsDescription(t *testing.T) {
	description := AdminsTool(AdminsOptions{}).Description
	if strings.Contains(description, "moderate") {
		t.Errorf("description = %q, want nothing about who may moderate", description)
	}
	if !strings.Contains(description, "anyone not listed who wrote in the messages you were given is a regular member") {
		t.Errorf("description = %q, want regular members limited to the messages given", description)
	}
}

// An owner who is not a Telegram admin was listed among the regular members
// ("o'zingiz (master)"): the note must say owners never are.
func TestAdminsNoteKeepsOwnersOutOfRegularMembers(t *testing.T) {
	for _, want := range []string{"except your owners", "never call an owner a regular member"} {
		if !strings.Contains(adminsNote, want) {
			t.Errorf("the note does not say %q: %q", want, adminsNote)
		}
	}
}
