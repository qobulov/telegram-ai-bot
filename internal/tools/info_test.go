package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/ranks"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

type fakeGroupInfo struct{}

func (fakeGroupInfo) GetChat(context.Context, int64) (telegram.Chat, error) {
	return telegram.Chat{Title: "BS news", Username: "bsnews", Description: "Brawl Stars yangiliklari",
		Permissions:   &telegram.ChatPermissions{CanSendMessages: true, CanSendPhotos: true},
		PinnedMessage: &telegram.Message{Text: "Qoidalar: reklama yo'q"}}, nil
}
func (fakeGroupInfo) GetChatMemberCount(context.Context, int64) (int, error) { return 1234, nil }
func (fakeGroupInfo) GetChatMember(_ context.Context, _, userID int64) (telegram.ChatMember, error) {
	if userID == rulesBotID {
		return telegram.ChatMember{Status: "administrator", CanDeleteMessages: true, CanRestrictMembers: true}, nil
	}
	return telegram.ChatMember{Status: "administrator", CustomTitle: "Moderator", User: &telegram.User{ID: userID, FirstName: "Abu", Username: "abu"}}, nil
}
func (fakeGroupInfo) GetChatAdministrators(context.Context, int64) ([]telegram.ChatMember, error) {
	return make([]telegram.ChatMember, 3), nil
}

// group_info reads the group's bio, link, size, permissions, pin and the
// bot's own rights.
func TestGroupInfo(t *testing.T) {
	tool := GroupInfoTool(fakeGroupInfo{}, rulesBotID, &fakeRanks{overrides: map[string]int{"ban": 3, "access": ranks.Above}})
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true}, &Sink{})
	got, err := tool.Run(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	for _, want := range []string{`"description":"Brawl Stars yangiliklari"`, `"link":"https://t.me/bsnews"`, `"members":1234`, `"admins":3`, `"pinned_message":"Qoidalar`, `"photos"`, `"can_restrict_members"`,
		`{"rank":1,"opens":["Guruhdan chiqarish (kik)","Mut va mutdan chiqarish"`, `{"rank":3,"opens":["Ban va bandan chiqarish","Xabarni o'chirish"`, `{"rank":6,"opens":["Buyruqlar darajasini o'zgartirish"]}`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("group_info = %s, want %s", b, want)
		}
	}
}

type fakeMemberInfo struct{}

func (fakeMemberInfo) MemberActivityOf(context.Context, int64, int64, time.Time) (storage.MemberActivity, error) {
	return storage.MemberActivity{MessagesToday: 12, MessagesWeek: 80, Muted: 1, BroughtBy: "Aziz", JoinedAt: time.Now()}, nil
}
func (fakeMemberInfo) RankOf(context.Context, int64, int64) (int, error) { return 2, nil }
func (fakeMemberInfo) Warns(context.Context, int64, int64, int) ([]storage.MemberWarn, error) {
	return []storage.MemberWarn{{Count: 1, Reason: "spam"}}, nil
}
func (fakeMemberInfo) InviteTop(_ context.Context, _ int64, q storage.InviteQuery) ([]storage.InviterCount, error) {
	return []storage.InviterCount{{UserID: q.Inviter, Added: 5, Stayed: 4}}, nil
}
func (fakeMemberInfo) AuthorByUsername(context.Context, int64, string) (int64, error) {
	return memberID, nil
}
func (fakeMemberInfo) CardCounts(context.Context, int64) (map[int]int, map[int]int, int64, error) {
	return nil, map[int]int{0: 3, 4: 1}, 7000, nil
}
func (fakeMemberInfo) CardPlayerOf(context.Context, int64) (storage.CardPlayer, time.Time, error) {
	return storage.CardPlayer{Tokens: 4}, time.Time{}, nil
}
func (fakeMemberInfo) CardWallet(context.Context, int64) (int, error) {
	return 30, nil
}
func (fakeMemberInfo) CardTop(context.Context, int) ([]storage.CardPlayer, error) {
	return []storage.CardPlayer{{UserID: 1}, {UserID: userID}}, nil
}

// member_info tells the asker about themselves, and about a tagged member;
// a tag names no one when there is none.
func TestMemberInfo(t *testing.T) {
	tool := MemberInfoTool(MemberInfoOptions{Store: fakeMemberInfo{}, Telegram: fakeGroupInfo{},
		Tokens: func(p storage.CardPlayer, _ time.Time) int { return p.Tokens }})
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true}, &Sink{})
	got, err := tool.Run(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	for _, want := range []string{`"about":"the asker"`, `"rank":2`, `"warnings":1`, `"last_warning_reason":"spam"`, `"messages_today":12`,
		`"brought_in_by":"Aziz"`, `"people_brought":5`, `"admin_title":"Moderator"`, `"points":7000`, `"place":2`, `"tokens":4`, `"crystals":30`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("member_info = %s, want %s", b, want)
		}
	}
	tagged := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, Mentions: []Mention{{Username: "bek"}}}, &Sink{})
	if got, err := tool.Run(tagged, json.RawMessage(`{"whose":"tagged"}`)); err != nil || got.(map[string]any)["about"] != "another member" {
		t.Errorf("tagged = %v, %v", got, err)
	}
	if _, err := tool.Run(ctx, json.RawMessage(`{"whose":"tagged"}`)); err == nil {
		t.Error("a tag with no one tagged was answered")
	}
}
