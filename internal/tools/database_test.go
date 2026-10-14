package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

const groupA, groupB = int64(-100111), int64(-100222)

type fakeDatabase struct {
	since    time.Time
	messages map[int64][]storage.GroupMessage
}

func (f *fakeDatabase) Overview(_ context.Context, since time.Time) (storage.Overview, error) {
	f.since = since
	return storage.Overview{Groups: 2, Users: 7}, nil
}

func (f *fakeDatabase) Groups(_ context.Context, _ time.Time, _ int) ([]storage.GroupSummary, error) {
	return []storage.GroupSummary{
		{ChatID: groupA, Title: "Devs", MessagesToday: 4},
		{ChatID: groupB, Title: "Old", MessagesToday: 0},
	}, nil
}

func (f *fakeDatabase) GroupMessagesSince(_ context.Context, chatID int64, _ time.Time, limit int) ([]storage.GroupMessage, error) {
	messages := f.messages[chatID]
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

type fakeMembers map[int64]Membership

func (f fakeMembers) Membership(_ context.Context, chatID int64) (Membership, error) {
	if m, ok := f[chatID]; ok {
		return m, nil
	}
	return Membership{}, ErrNotInGroup
}

func databaseRegistry(t *testing.T, db *fakeDatabase) *Registry {
	t.Helper()
	tool := DatabaseTool(DatabaseOptions{
		Store:    db,
		Members:  fakeMembers{groupA: {Status: "administrator", Members: 12, Title: "Devs Uz"}},
		Location: tashkent,
		Now:      func() time.Time { return time.Date(2026, 10, 14, 15, 0, 0, 0, tashkent) },
	})
	return mustRegistry(t, tool)
}

func ownerIn(chatID int64) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: chatID, Owner: true}, &Sink{})
}

func query(registry *Registry, ctx context.Context, args string) envelope {
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, userID, "bot_database", args)), &env)
	return env
}

func TestDatabaseToolIsOnlyForOwners(t *testing.T) {
	registry := databaseRegistry(t, &fakeDatabase{})
	member := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA}, &Sink{})

	if defs := registry.Definitions(member, userID); len(defs) != 0 {
		t.Errorf("a member is offered %+v", defs)
	}
	if defs := registry.Definitions(context.Background(), userID); len(defs) != 0 {
		t.Errorf("a call outside a turn is offered %+v", defs)
	}
	if defs := registry.Definitions(ownerIn(groupA), userID); len(defs) != 1 {
		t.Errorf("the owner is offered %+v", defs)
	}
	expectError(t, registry.Execute(member, userID, "bot_database", `{"action":"overview"}`), "forbidden")
	// The turn's owner must be the one calling: another user ID gets nothing.
	expectError(t, registry.Execute(ownerIn(groupA), userID+1, "bot_database", `{"action":"overview"}`), "forbidden")
}

func TestOverviewCountsFromTheStartOfToday(t *testing.T) {
	db := &fakeDatabase{}
	env := query(databaseRegistry(t, db), ownerIn(userID), `{"action":"overview"}`)

	if !env.OK || !strings.Contains(string(env.Result), `"groups":2`) || !strings.Contains(string(env.Result), `"users":7`) {
		t.Errorf("overview = %+v", env)
	}
	if want := time.Date(2026, 10, 14, 0, 0, 0, 0, tashkent); !db.since.Equal(want) {
		t.Errorf("since = %v, want %v", db.since, want)
	}
}

func TestGroupsShowTheBotsLiveStatus(t *testing.T) {
	env := query(databaseRegistry(t, &fakeDatabase{}), ownerIn(userID), `{"action":"groups"}`)
	if !env.OK {
		t.Fatalf("groups = %+v", env)
	}
	var result struct {
		Groups []struct {
			Title      string `json:"title"`
			Members    *int   `json:"members"`
			BotInGroup *bool  `json:"bot_in_group"`
			BotStatus  string `json:"bot_status"`
			BotIsAdmin bool   `json:"bot_is_admin"`
		} `json:"groups"`
		BotIsIn    int `json:"bot_is_in"`
		BotIsAdmin int `json:"bot_is_admin_in"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 2 || result.BotIsIn != 1 || result.BotIsAdmin != 1 {
		t.Fatalf("result = %+v", result)
	}
	devs, old := result.Groups[0], result.Groups[1]
	if devs.Members == nil || *devs.Members != 12 || devs.BotInGroup == nil || !*devs.BotInGroup || !devs.BotIsAdmin || devs.BotStatus != "administrator" {
		t.Errorf("devs = %+v", devs)
	}
	// Telegram's current title wins over the stored one.
	if devs.Title != "Devs Uz" {
		t.Errorf("title = %q", devs.Title)
	}
	if old.BotInGroup == nil || *old.BotInGroup || old.Members != nil || old.BotIsAdmin {
		t.Errorf("a group that removed the bot = %+v", old)
	}
}

func TestBotThatLeftIsNotInTheGroup(t *testing.T) {
	tool := DatabaseTool(DatabaseOptions{Store: &fakeDatabase{}, Members: fakeMembers{groupA: {Status: "left"}, groupB: {Status: "member", Members: 3}}})
	env := query(mustRegistry(t, tool), ownerIn(userID), `{"action":"groups"}`)

	if !env.OK || !strings.Contains(string(env.Result), `"bot_is_in":1`) || !strings.Contains(string(env.Result), `"bot_is_admin_in":0`) {
		t.Errorf("groups = %s", env.Result)
	}
}

func TestMessagesOfAGroup(t *testing.T) {
	db := &fakeDatabase{messages: map[int64][]storage.GroupMessage{
		groupA: {{AuthorName: "Ann", Text: "hello", SentAt: time.Date(2026, 10, 14, 9, 5, 0, 0, tashkent)}},
	}}
	env := query(databaseRegistry(t, db), ownerIn(userID), `{"action":"messages","chat_id":-100111}`)

	if !env.OK || !strings.Contains(string(env.Result), "09:05 Ann: hello") {
		t.Errorf("messages = %+v", env)
	}
}

func TestAnotherGroupsMessagesAreNotShownInAGroup(t *testing.T) {
	db := &fakeDatabase{messages: map[int64][]storage.GroupMessage{groupB: {{AuthorName: "Bek", Text: "secret"}}}}
	env := query(databaseRegistry(t, db), ownerIn(groupA), `{"action":"messages","chat_id":-100222}`)

	if env.OK || strings.Contains(env.Error.Message, "secret") || !strings.Contains(env.Error.Message, "private") {
		t.Errorf("result = %+v; another group's messages must be read in a private chat", env)
	}
}

func TestDatabaseToolChecksItsArguments(t *testing.T) {
	registry := databaseRegistry(t, &fakeDatabase{})
	for _, args := range []string{`{}`, `{"action":"drop"}`, `{"action":"messages"}`, `{"action":"messages","chat_id":5}`, `{"action":"messages","chat_id":-1,"limit":9999}`} {
		if env := query(registry, ownerIn(userID), args); env.OK || env.Error.Code != "invalid_arguments" {
			t.Errorf("%s: %+v", args, env)
		}
	}
}
