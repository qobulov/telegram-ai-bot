package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

const creatorID = int64(4004)

type fakeCreators struct{ err error }

func (f fakeCreators) IsGroupCreator(_ context.Context, _, userID int64) (bool, error) {
	return userID == creatorID, f.err
}

func punishmentsRegistry(t *testing.T, store *fakeRules, creators fakeCreators) *Registry {
	t.Helper()
	return mustRegistry(t, PunishmentsTool(PunishmentsOptions{Store: store, Creators: creators, BotID: rulesBotID, Location: tashkent, Now: func() time.Time { return rulesNow }}))
}

// asking in the group as a member who may be an owner, replying to target.
func punisher(userID int64, owner bool, target *Target, words string) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: owner, Human: true, UpdateID: 1, MessageID: 30, Target: target, Words: words}, &Sink{})
}

func punishments(registry *Registry, ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, caller.UserID, "punishments", args)), &env)
	return env
}

// "Bu bola yana so'kinsa ban qil" in reply to a member is kept as a ban for
// their swearing; "18+ tashasa 3 soat mute qil", from the group's creator,
// for anyone's pictures.
func TestPunishmentsAreSetByOwnersAndTheGroupsCreator(t *testing.T) {
	store := &fakeRules{}
	registry := punishmentsRegistry(t, store, fakeCreators{})

	env := punishments(registry, punisher(userID, true, bekTarget(), "bu bola yana so'kinsa ban qil"), `{"action":"set","punish_for":"swearing","punishment":"ban","about_member":true}`)
	if !env.OK || !strings.Contains(string(env.Result), `"who":"Bek @bek"`) {
		t.Fatalf("owner's punishment = %+v %s", env, env.Result)
	}
	env = punishments(registry, punisher(creatorID, false, nil, "18+ tashasa 3 soat mute qil"), `{"action":"set","punish_for":"pictures","punishment":"mute","mute_hours":3}`)
	if !env.OK {
		t.Fatalf("creator's punishment = %+v", env)
	}
	got := store.all()
	if len(got) != 2 || got[0].Kind != storage.RuleSwearPunishment || got[0].UserID != memberID || got[0].Text != "ban" ||
		!got[0].ExpiresAt.Equal(rulesNow.Add(maxRuleHours*time.Hour)) || got[1].Kind != storage.RulePicturePunishment || got[1].UserID != 0 || got[1].Text != "mute:3" || got[1].SetBy != creatorID {
		t.Errorf("rules = %+v", got)
	}
}

func TestPunishmentsAreRefusedToOthers(t *testing.T) {
	store := &fakeRules{}
	for name, ctx := range map[string]context.Context{
		"an admin or member": punisher(memberID, false, nil, "kim so'kinsa ban qil"),
	} {
		if env := punishments(punishmentsRegistry(t, store, fakeCreators{}), ctx, `{"action":"set","punish_for":"swearing","punishment":"ban"}`); env.OK || env.Error.Code != "forbidden" {
			t.Errorf("%s: %+v", name, env)
		}
	}
	if env := punishments(punishmentsRegistry(t, store, fakeCreators{err: errors.New("down")}), punisher(creatorID, false, nil, "kim so'kinsa ban qil"), `{"action":"list"}`); env.OK {
		t.Errorf("without Telegram's answer: %+v", env)
	}
	if got := store.all(); len(got) != 0 {
		t.Errorf("rules = %+v", got)
	}
}

func TestPunishmentNeedsTheAskersWords(t *testing.T) {
	store := &fakeRules{}
	registry := punishmentsRegistry(t, store, fakeCreators{})
	for words, args := range map[string]string{
		"bu bola yomon":                  `{"action":"set","punish_for":"swearing","punishment":"ban","about_member":true}`,
		"yana so'kinsa ban qil":          `{"action":"set","punish_for":"swearing","punishment":"mute","about_member":true}`,
		"yana so'kinsa mute qil":         `{"action":"set","punish_for":"swearing","punishment":"mute","mute_hours":721}`,
		"yana so'kinsa ban qil, iltimos": `{"action":"set","punish_for":"spam","punishment":"ban"}`,
	} {
		if env := punishments(registry, punisher(userID, true, bekTarget(), words), args); env.OK {
			t.Errorf("%q, %s: saved", words, args)
		}
	}
	owner := &Target{MessageID: 29, UserID: memberID, Name: "Boss", Owner: true}
	if env := punishments(registry, punisher(creatorID, false, owner, "yana so'kinsa ban qil"), `{"action":"set","punish_for":"swearing","punishment":"ban","about_member":true}`); env.OK {
		t.Errorf("an owner was given a punishment: %s", env.Result)
	}
	if got := store.all(); len(got) != 0 {
		t.Errorf("rules = %+v", got)
	}
}

// list shows the group's punishments only, and remove takes nothing else.
func TestPunishmentsAreListedAndRemoved(t *testing.T) {
	store := &fakeRules{}
	registry := punishmentsRegistry(t, store, fakeCreators{})
	punishments(registry, punisher(userID, true, bekTarget(), "yana so'kinsa ban qil"), `{"action":"set","punish_for":"swearing","punishment":"ban","about_member":true}`)
	if _, err := store.SetGroupRule(context.Background(), storage.GroupRule{ChatID: groupA, Kind: storage.RuleInstruction, Text: "salom"}); err != nil {
		t.Fatal(err)
	}
	env := punishments(registry, punisher(creatorID, false, nil, ""), `{"action":"list"}`)
	if !env.OK || strings.Contains(string(env.Result), "salom") || !strings.Contains(string(env.Result), `"text":"ban"`) {
		t.Fatalf("list = %+v %s", env, env.Result)
	}
	if env := punishments(registry, punisher(creatorID, false, nil, ""), `{"action":"remove","id":2}`); !env.OK || string(env.Result) != `{"removed":false}` {
		t.Errorf("removing the instruction = %s", env.Result)
	}
	if env := punishments(registry, punisher(creatorID, false, nil, ""), `{"action":"remove","id":1}`); !env.OK || string(env.Result) != `{"removed":true}` {
		t.Errorf("removing the punishment = %s", env.Result)
	}
}
