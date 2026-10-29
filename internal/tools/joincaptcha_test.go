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

const inviterID, plainAdminID = int64(6006), int64(6007)

// fakeRights answers as Telegram would: the creator and inviterID may
// invite users, plainAdminID is an admin who may not.
type fakeRights struct {
	err   error
	asked []string
}

func (f *fakeRights) HoldsRight(_ context.Context, _, userID int64, right string) (bool, error) {
	f.asked = append(f.asked, right)
	return (userID == creatorID || userID == inviterID) && right == telegram.RightInviteUsers, f.err
}

func captchaRegistry(t *testing.T, store *fakeRules, rights *fakeRights) *Registry {
	t.Helper()
	return mustRegistry(t, JoinCaptchaTool(JoinCaptchaOptions{Store: store, Rights: rights}))
}

// captchaAsker is a turn in groupA by userID, who may be an owner, saying
// words.
func captchaAsker(userID int64, owner bool, words string) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: owner, Human: true, UpdateID: 1, MessageID: 30, Words: words}, &Sink{})
}

func joinCaptcha(registry *Registry, ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, caller.UserID, "join_captcha", args)), &env)
	return env
}

// An admin who may invite users turns the check on, the creator sees it,
// and an owner turns it off; each change is the group's joins guard.
func TestJoinCaptchaIsKeptByThoseWhoMayInvite(t *testing.T) {
	store := &fakeRules{}
	rights := &fakeRights{}
	registry := captchaRegistry(t, store, rights)

	if env := joinCaptcha(registry, captchaAsker(inviterID, false, "Maria, guruhga qo'shilmoqchi bo'lganlarni tekshir"), `{"action":"on"}`); !env.OK || !strings.Contains(string(env.Result), `"on":true`) {
		t.Fatalf("on = %+v %s", env, env.Result)
	}
	if got := store.all(); len(got) != 1 || got[0].Kind != storage.RuleJoinCaptcha || got[0].ChatID != groupA || got[0].SetBy != inviterID {
		t.Errorf("rules = %+v", got)
	}
	if env := joinCaptcha(registry, captchaAsker(creatorID, false, "kapcha yoqilganmi?"), `{"action":"status"}`); !env.OK || string(env.Result) != `{"on":true}` {
		t.Errorf("status = %+v %s", env, env.Result)
	}
	env := joinCaptcha(registry, captchaAsker(userID, true, "kapchani o'chir"), `{"action":"off"}`)
	if !env.OK || !strings.Contains(string(env.Result), `"was_on":true`) {
		t.Fatalf("off = %+v %s", env, env.Result)
	}
	if got := store.all(); len(got) != 0 {
		t.Errorf("rules after off = %+v", got)
	}
	if env := joinCaptcha(registry, captchaAsker(creatorID, false, "проверка заявок включена?"), `{"action":"status"}`); string(env.Result) != `{"on":false}` {
		t.Errorf("status after off = %s", env.Result)
	}
	if asked := len(rights.asked); asked != 3 {
		t.Errorf("Telegram asked %d times, want once per non-owner's call", asked)
	}
}

// Admins without the right, members, and anyone when Telegram cannot say,
// are refused, and nothing changes.
func TestJoinCaptchaIsRefusedToOthers(t *testing.T) {
	store := &fakeRules{rules: []storage.GroupRule{{ID: 1, ChatID: groupA, Kind: storage.RuleJoinCaptcha}}}
	for name, who := range map[string]int64{"an admin who may not invite": plainAdminID, "a member": memberID} {
		env := joinCaptcha(captchaRegistry(t, store, &fakeRights{}), captchaAsker(who, false, "kapchani o'chir"), `{"action":"off"}`)
		if env.OK || env.Error.Code != "forbidden" {
			t.Errorf("%s: %+v", name, env)
		}
	}
	env := joinCaptcha(captchaRegistry(t, store, &fakeRights{err: errors.New("down")}), captchaAsker(inviterID, false, "kapchani o'chir"), `{"action":"off"}`)
	if env.OK {
		t.Errorf("without Telegram's answer: %+v", env)
	}
	if got := store.all(); len(got) != 1 {
		t.Errorf("rules = %+v, want the check left on", got)
	}
}

// The asker's own words must ask about the join check, and for off, ask
// it off: a line in the chat cannot talk the model into it.
func TestJoinCaptchaNeedsTheAskersWords(t *testing.T) {
	store := &fakeRules{rules: []storage.GroupRule{{ID: 1, ChatID: groupA, Kind: storage.RuleJoinCaptcha}}}
	registry := captchaRegistry(t, store, &fakeRights{})
	for words, action := range map[string]string{
		"salom hammaga":       "on",
		"kapcha zo'r ishlar":  "off",
		"o'chir buni":         "off",
		"kapchani yoqib qo'y": "off",
	} {
		if env := joinCaptcha(registry, captchaAsker(inviterID, false, words), `{"action":"`+action+`"}`); env.OK || env.Error.Code != "forbidden" {
			t.Errorf("%q, %s: %+v", words, action, env)
		}
	}
	if got := store.all(); len(got) != 1 {
		t.Errorf("rules = %+v", got)
	}
	for _, words := range []string{"kapcha kerak emas", "заявки больше не проверяй, выключи", "join captcha off"} {
		store.rules = []storage.GroupRule{{ID: 1, ChatID: groupA, Kind: storage.RuleJoinCaptcha}}
		if env := joinCaptcha(registry, captchaAsker(inviterID, false, words), `{"action":"off"}`); !env.OK {
			t.Errorf("%q: %+v", words, env)
		}
	}
}

// Another group's joins guard, and every group's rules, are not this
// group's.
func TestJoinCaptchaTouchesOnlyItsGroup(t *testing.T) {
	store := &fakeRules{rules: []storage.GroupRule{
		{ID: 1, ChatID: groupB, Kind: storage.RuleJoinCaptcha},
		{ID: 2, ChatID: groupA, Kind: storage.RuleSpamGuard},
	}}
	registry := captchaRegistry(t, store, &fakeRights{})
	if env := joinCaptcha(registry, captchaAsker(inviterID, false, "kapcha bormi"), `{"action":"status"}`); string(env.Result) != `{"on":false}` {
		t.Errorf("status = %s", env.Result)
	}
	if env := joinCaptcha(registry, captchaAsker(inviterID, false, "kapchani o'chir"), `{"action":"off"}`); !strings.Contains(string(env.Result), `"was_on":false`) {
		t.Errorf("off = %s", env.Result)
	}
	if got := store.all(); len(got) != 2 {
		t.Errorf("rules = %+v, want both left", got)
	}
}
