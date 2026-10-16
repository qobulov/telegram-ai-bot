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
// same, and at Telegram's longest names and titles it still fits in the
// result.
func TestAdminsListIsCut(t *testing.T) {
	list := &fakeAdminList{}
	for i := range 60 {
		member := listedAdmin("administrator", telegram.User{
			ID: int64(1000 + i), FirstName: strings.Repeat("<", 64), Username: fmt.Sprintf("%032d", i),
		})
		member.CustomTitle = strings.Repeat("<", 16)
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
	if defs := registry.Definitions(private, memberID); len(defs) != 0 {
		t.Errorf("offered in a private chat: %+v", defs)
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

// The model cannot point the tool at another group.
func TestAdminsTakesNoArguments(t *testing.T) {
	list := &fakeAdminList{}

	got := askAdmins(adminsRegistry(t, list), memberAsking(groupA, 100), `{"chat_id":-100222}`)

	if got.OK || got.Error.Code != "invalid_arguments" {
		t.Errorf("group_admins with a chat_id = %+v, want invalid_arguments", got)
	}
	if chats := list.chats(); len(chats) != 0 {
		t.Errorf("asked Telegram about chats %v", chats)
	}
}

func TestAdminsTelegramFailureIsShort(t *testing.T) {
	for name, err := range map[string]error{
		"no answer": errors.New("telegram getChatAdministrators: dial tcp 149.154.167.220:443: i/o timeout"),
		"refused":   &telegram.APIError{Code: 400, Description: "Bad Request: chat not found"},
	} {
		t.Run(name, func(t *testing.T) {
			list := &fakeAdminList{err: err}

			got := askAdmins(adminsRegistry(t, list), memberAsking(groupA, 100), `{}`)

			if got.OK || got.Error.Code != "execution_failed" || got.Error.Message != adminsUnavailable {
				t.Errorf("group_admins = %+v, want only %q", got, adminsUnavailable)
			}
		})
	}
}
