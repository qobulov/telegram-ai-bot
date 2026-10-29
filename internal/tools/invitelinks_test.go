package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// InviteLink finds a link the bot made as the store does: the latest
// invite_link record of the chat, not undone, whose undo holds link.
func (f *fakeAudit) InviteLink(_ context.Context, chatID int64, link string) (storage.ModerationAction, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.actions) - 1; i >= 0; i-- {
		a := f.actions[i]
		var state linkState
		if a.ChatID != chatID || a.Action != actionInviteLink || f.undone[a.ID] || json.Unmarshal(a.Undo, &state) != nil {
			continue
		}
		if link == "" || state.Link == link {
			return a, true, nil
		}
	}
	return storage.ModerationAction{}, false, nil
}

// linkEnv is groupAdminEnv where the tool finds the links it made.
func linkEnv(t *testing.T) *moderationEnv {
	t.Helper()
	env := groupAdminEnv(t)
	env.opts.Links = env.audit
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.Tool())
	return env
}

func (env *moderationEnv) invites() []telegram.InviteLink {
	env.telegram.mu.Lock()
	defer env.telegram.mu.Unlock()
	return append([]telegram.InviteLink(nil), env.telegram.invites...)
}

// A new link may admit a number of people, or ask the admins to approve
// each, as Telegram allows; never both.
func TestInviteLinksTakeAMemberLimitOrJoinRequests(t *testing.T) {
	env := linkEnv(t)

	got := env.moderate(groupTurn(800, "50 kishilik taklif havolasi ber"), `{"action":"invite_link","member_limit":50}`)
	var result struct {
		Link        string `json:"link"`
		MemberLimit int    `json:"member_limit"`
	}
	if !got.OK || json.Unmarshal(got.Result, &result) != nil || result.Link != "https://t.me/+abc" || result.MemberLimit != 50 {
		t.Errorf("invite_link = %+v %s", got, got.Result)
	}
	env.act(t, groupTurn(801, "tasdiq bilan qo'shiladigan link ber"), `{"action":"invite_link","join_request":true}`)
	want := []telegram.InviteLink{{ChatID: groupA, MemberLimit: 50}, {ChatID: groupA, JoinRequest: true}}
	if got := env.invites(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("links asked for = %+v, want %+v", got, want)
	}

	before := len(env.telegram.made())
	for _, args := range []string{
		`{"action":"invite_link","member_limit":10,"join_request":true}`,
		`{"action":"invite_link","member_limit":100000}`,
		`{"action":"invite_link","member_limit":0}`,
	} {
		if got := env.moderate(groupTurn(802, "link ber"), args); got.OK || got.Error.Code != "invalid_arguments" {
			t.Errorf("%s = %+v, want refused", args, got)
		}
	}
	if got := env.moderate(groupTurn(803, "link ber"), `{"action":"invite_link","member_limit":10,"join_request":true}`); !strings.Contains(got.Error.Message, "approve") {
		t.Errorf("both limits = %+v, want Telegram's rule explained", got)
	}
	if len(env.telegram.made()) != before {
		t.Errorf("Telegram was asked: %q", env.telegram.made()[before:])
	}
}

// revoke_link stops the latest link the bot made from working, or the one
// named, and only one it made; the next revoke finds none.
func TestRevokeLinkActsOnlyOnLinksTheBotMade(t *testing.T) {
	env := linkEnv(t)
	if got := env.moderate(groupTurn(810, "linkni bekor qil"), `{"action":"revoke_link"}`); got.OK || got.Error.Message != noLinkMade {
		t.Errorf("revoke with no link made = %+v", got)
	}
	env.moderate(groupTurn(811, "taklif havolasi ber"), `{"action":"invite_link"}`)

	if got := env.moderate(groupTurn(812, "https://t.me/+other linkni bekor qil"), `{"action":"revoke_link","link":"https://t.me/+other"}`); got.OK || got.Error.Message != notMyLink {
		t.Errorf("revoking another link = %+v", got)
	}
	if got := env.moderate(groupTurn(813, "salom link"), `{"action":"revoke_link"}`); got.OK || got.Error.Message != notAsked {
		t.Errorf("revoke without being asked = %+v", got)
	}
	if said := env.act(t, groupTurn(814, "t.me/+abc linkni o'chir"), `{"action":"revoke_link","link":"t.me/+abc"}`); said != "Bajarildi: taklif havolasi bekor qilindi." {
		t.Errorf("said %q", said)
	}
	if calls := env.telegram.made(); calls[len(calls)-1] != "revokeChatInviteLink https://t.me/+abc "+inGroupA {
		t.Errorf("last call = %q", calls[len(calls)-1])
	}
	if got := env.moderate(groupTurn(815, "linkni bekor qil"), `{"action":"revoke_link"}`); got.OK || got.Error.Message != noLinkMade {
		t.Errorf("a second revoke = %+v, want no link left", got)
	}
	if got := env.moderate(undoTurn(816, "qaytar"), `{"action":"undo"}`); got.OK {
		t.Errorf("undo after a revoke = %s, want the revoked link not revoked again", got.Result)
	}
}

// edit_link sets every limit anew from the link's own: what is not asked
// for stays, and the two Telegram does not combine are refused.
func TestEditLinkKeepsWhatItIsNotAskedToChange(t *testing.T) {
	env := linkEnv(t)
	env.moderate(groupTurn(820, "reklama nomli link ber, bir soatlik"), `{"action":"invite_link","text":"reklama","minutes":60}`)

	if said := env.act(t, groupTurn(821, "linkni 20 kishiga chekla"), `{"action":"edit_link","member_limit":20}`); said != "Bajarildi: taklif havolasi endi 20 kishigacha qo'shadi." {
		t.Errorf("said %q", said)
	}
	if got := env.moderate(groupTurn(822, "linkni tasdiq bilan qil"), `{"action":"edit_link","join_request":true}`); got.OK || !strings.Contains(got.Error.Message, "approve") {
		t.Errorf("join requests on a limited link = %+v", got)
	}
	env.act(t, groupTurn(823, "linkni limitsiz, tasdiq bilan qil"), `{"action":"edit_link","member_limit":0,"join_request":true}`)
	if got := env.moderate(groupTurn(824, "linkni o'zgartir"), `{"action":"edit_link"}`); got.OK || got.Error.Message != nothingToEdit {
		t.Errorf("an edit of nothing = %+v", got)
	}

	ends := rulesNow.Add(time.Hour)
	want := []telegram.InviteLink{
		{ChatID: groupA, Name: "reklama", Expires: ends},
		{ChatID: groupA, Name: "reklama", Expires: time.Unix(ends.Unix(), 0), MemberLimit: 20},
		{ChatID: groupA, Name: "reklama", Expires: time.Unix(ends.Unix(), 0), JoinRequest: true},
	}
	got := env.invites()
	if len(got) != len(want) {
		t.Fatalf("links asked for = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Name != want[i].Name || !got[i].Expires.Equal(want[i].Expires) || got[i].MemberLimit != want[i].MemberLimit || got[i].JoinRequest != want[i].JoinRequest {
			t.Errorf("link %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if calls := env.telegram.made(); calls[len(calls)-1] != "editChatInviteLink https://t.me/+abc "+inGroupA {
		t.Errorf("last call = %q", calls[len(calls)-1])
	}
	// Undo still revokes the link, edited or not.
	env.act(t, undoTurn(825, "qaytar"), `{"action":"undo"}`)
	if calls := env.telegram.made(); calls[len(calls)-1] != "revokeChatInviteLink https://t.me/+abc "+inGroupA {
		t.Errorf("last call = %q, want the link revoked", calls[len(calls)-1])
	}
}

// Without a way to find its links, the bot revokes and edits none.
func TestLinksCannotBeChangedWithoutTheirRecords(t *testing.T) {
	env := groupAdminEnv(t)
	env.moderate(groupTurn(830, "taklif havolasi ber"), `{"action":"invite_link"}`)
	if got := env.moderate(groupTurn(831, "linkni bekor qil"), `{"action":"revoke_link"}`); got.OK || got.Error.Message != noLinkMade {
		t.Errorf("revoke = %+v", got)
	}
}
