package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeRoster is the members the bot has seen, and the group's Telegram
// numbers.
type fakeRoster struct {
	members  []storage.GroupMember
	storeErr error
	count    int
	countErr error
	admins   []telegram.ChatMember
	adminErr error
}

func (f *fakeRoster) GroupMembers(_ context.Context, _ int64, limit int) ([]storage.GroupMember, int, error) {
	if f.storeErr != nil {
		return nil, 0, f.storeErr
	}
	return f.members[:min(limit, len(f.members))], len(f.members), nil
}

func (f *fakeRoster) GetChatMemberCount(context.Context, int64) (int, error) {
	return f.count, f.countErr
}

func (f *fakeRoster) GetChatAdministrators(context.Context, int64) ([]telegram.ChatMember, error) {
	return f.admins, f.adminErr
}

type memberList struct {
	Known   int `json:"known"`
	Total   int `json:"total"`
	Members []struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Role     string `json:"role"`
		Owner    bool   `json:"owner"`
		Bot      bool   `json:"bot"`
		LastSeen string `json:"last_seen"`
		Tag      string `json:"tag"`
	} `json:"members"`
	Note string `json:"note"`
}

func askMembers(t *testing.T, roster *fakeRoster, ctx context.Context) (envelope, memberList) {
	t.Helper()
	registry := mustRegistry(t, MembersTool(MembersOptions{Store: roster, Telegram: roster, Owners: map[int64]bool{userID: true}, Location: tashkent}))
	caller, _ := CallerFrom(ctx)
	var got envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, caller.UserID, "group_members", `{}`)), &got)
	var list memberList
	if got.OK {
		if err := json.Unmarshal(got.Result, &list); err != nil {
			t.Fatalf("result %s: %v", got.Result, err)
		}
	}
	return got, list
}

func seen(id int64, first, username string) storage.GroupMember {
	return storage.GroupMember{ChatID: groupA, UserID: id, FirstName: first, Username: username, Status: "member", LastSeen: rulesNow}
}

func TestMembersListsWhoWasSeenWithAdminsAndOwnersMarked(t *testing.T) {
	roster := &fakeRoster{
		members: []storage.GroupMember{seen(userID, "Ali", "ali"), seen(memberID, "Bek", "bek"), seen(7007, "Said", ""), {ChatID: groupA, UserID: 8008, FirstName: "Helper", IsBot: true, Status: "member", LastSeen: rulesNow}},
		count:   42,
		admins:  []telegram.ChatMember{listedAdmin("creator", telegram.User{ID: memberID}), listedAdmin("administrator", telegram.User{ID: rulesBotID, IsBot: true})},
	}
	got, list := askMembers(t, roster, asking(100, nil))
	if !got.OK || list.Known != 4 || list.Total != 42 || len(list.Members) != 4 {
		t.Fatalf("group_members = %+v %s", got, got.Result)
	}
	ali, bek, said, helper := list.Members[0], list.Members[1], list.Members[2], list.Members[3]
	if ali.Name != "Ali" || ali.Username != "@ali" || !ali.Owner || ali.Role != "" {
		t.Errorf("Ali = %+v, want an owner with no admin role", ali)
	}
	if bek.Role != "creator" || bek.Owner {
		t.Errorf("Bek = %+v, want the creator", bek)
	}
	if said.Username != "" || said.Role != "" || said.LastSeen != "16 Oct 15:04" {
		t.Errorf("Said = %+v, want a plain member seen 16 Oct 15:04", said)
	}
	if !helper.Bot {
		t.Errorf("Helper = %+v, want marked a bot", helper)
	}
	for _, want := range []string{"have seen", "not everyone", "never regular members", "Never guess"} {
		if !strings.Contains(list.Note, want) {
			t.Errorf("note %q does not say %q", list.Note, want)
		}
	}
}

// Telegram failing to count or to list admins still leaves the list; the
// note says roles are unknown.
func TestMembersWithoutTelegramStillLists(t *testing.T) {
	roster := &fakeRoster{members: []storage.GroupMember{seen(memberID, "Bek", "bek")}, countErr: errors.New("502"), adminErr: errors.New("502")}
	got, list := askMembers(t, roster, asking(100, nil))
	if !got.OK || list.Total != 0 || len(list.Members) != 1 || !strings.Contains(list.Note, "admins could not be checked") {
		t.Errorf("group_members = %+v %s", got, got.Result)
	}
}

func TestMembersListIsCut(t *testing.T) {
	roster := &fakeRoster{count: 500}
	for i := range maxMembersListed + 7 {
		roster.members = append(roster.members, seen(int64(10000+i), "M", ""))
	}
	got, list := askMembers(t, roster, asking(100, nil))
	if !got.OK || len(list.Members) != maxMembersListed || list.Known != maxMembersListed+7 || !strings.Contains(list.Note, "7 more") {
		t.Errorf("known %d, listed %d, note %q", list.Known, len(list.Members), list.Note)
	}
}

func TestMembersRefusals(t *testing.T) {
	roster := &fakeRoster{storeErr: errors.New("db down: secret")}
	if got, _ := askMembers(t, roster, asking(100, nil)); got.OK || strings.Contains(got.Error.Message, "secret") {
		t.Errorf("store failure = %+v, want a short refusal", got)
	}
	private := WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Human: true, UpdateID: 100, MessageID: 50}, &Sink{})
	if got, _ := askMembers(t, &fakeRoster{}, private); got.OK {
		t.Errorf("private chat = %+v, want refused", got)
	}
}

// An admin who never wrote is listed all the same, with their role.
func TestMembersListsAdminsWhoNeverWrote(t *testing.T) {
	roster := &fakeRoster{
		members: []storage.GroupMember{seen(memberID, "Bek", "bek")},
		admins:  []telegram.ChatMember{listedAdmin("creator", telegram.User{ID: 7007, FirstName: "Silent", Username: "silent"})},
	}
	got, list := askMembers(t, roster, asking(100, nil))
	if !got.OK || list.Known != 2 || len(list.Members) != 2 || list.Members[1].Name != "Silent" || list.Members[1].Role != "creator" || list.Members[1].LastSeen != "" {
		t.Errorf("group_members = %+v %s", got, got.Result)
	}
}

// A member without a @username gets a tag the reply turns into their name,
// linked so that Telegram tells them; one with a @username needs none.
func TestMembersWithoutAUsernameGetATag(t *testing.T) {
	roster := &fakeRoster{members: []storage.GroupMember{seen(memberID, "無敵•[MAYOR]", ""), seen(7007, "Ali", "ali")}}
	_, list := askMembers(t, roster, asking(100, nil))
	if len(list.Members) != 2 {
		t.Fatalf("members = %+v", list.Members)
	}
	var tags []string
	for _, m := range list.Members {
		tags = append(tags, m.Tag)
	}
	if tags[0] != "[無敵•MAYOR](tg://user?id=5005)" || tags[1] != "" {
		t.Errorf("tags = %q", tags)
	}
}
