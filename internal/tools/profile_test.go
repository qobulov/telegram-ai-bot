package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/storage"
)

type fakeProfiles struct {
	mu       sync.Mutex
	profiles map[int64]storage.UserProfile
}

func (f *fakeProfiles) Profile(_ context.Context, userID int64) (storage.UserProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.profiles[userID]
	p.UserID = userID
	return p, nil
}

func (f *fakeProfiles) SetProfile(_ context.Context, p storage.UserProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.Empty() {
		delete(f.profiles, p.UserID)
		return nil
	}
	f.profiles[p.UserID] = p
	return nil
}

func profileCall(t *testing.T, store *fakeProfiles, words, args string) envelope {
	t.Helper()
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 100, Words: words}, &Sink{})
	registry := mustRegistry(t, ProfileTool(ProfileOptions{Store: store}))
	var got envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, userID, "my_profile", args)), &got)
	return got
}

func TestProfileSavesWhatThePersonAsksForThemselves(t *testing.T) {
	store := &fakeProfiles{profiles: map[int64]storage.UserProfile{}}
	if got := profileCall(t, store, "Maria, meni bundan keyin Aziz aka deb chaqir", `{"action":"set_name","name":"Aziz aka"}`); !got.OK {
		t.Fatalf("set_name = %+v", got)
	}
	if got := profileCall(t, store, "menga siz deb gapir", `{"action":"siz"}`); !got.OK {
		t.Fatalf("siz = %+v", got)
	}
	if p := store.profiles[userID]; p.CallName != "Aziz aka" || !p.Siz {
		t.Errorf("profile = %+v, want Aziz aka with siz", p)
	}
	got := profileCall(t, store, "nima saqlading", `{"action":"show"}`)
	if !got.OK || !strings.Contains(string(got.Result), `"call_name":"Aziz aka"`) {
		t.Errorf("show = %+v %s", got, got.Result)
	}
	if got := profileCall(t, store, "sen deb gapiraver", `{"action":"sen"}`); !got.OK || store.profiles[userID].Siz {
		t.Errorf("sen = %+v, profile %+v", got, store.profiles[userID])
	}
	if got := profileCall(t, store, "profilimni o'chir", `{"action":"clear"}`); !got.OK || len(store.profiles) != 0 {
		t.Errorf("clear = %+v, profiles %+v", got, store.profiles)
	}
}

// Only the person's own words name them: not a quote, not the model's idea.
func TestProfileNameMustBeTheirOwnWords(t *testing.T) {
	store := &fakeProfiles{profiles: map[int64]storage.UserProfile{}}
	for name, c := range map[string]struct{ words, args string }{
		"not in their message": {"meni chaqir", `{"action":"set_name","name":"Boss"}`},
		"too long":             {strings.Repeat("a", 60), `{"action":"set_name","name":"` + strings.Repeat("a", 60) + `"}`},
		"empty":                {"meni chaqir", `{"action":"set_name","name":" "}`},
		"unknown action":       {"x", `{"action":"forget_everyone"}`},
	} {
		if got := profileCall(t, store, c.words, c.args); got.OK {
			t.Errorf("%s: %+v, want refused", name, got)
		}
	}
	if len(store.profiles) != 0 {
		t.Errorf("saved %+v", store.profiles)
	}
}

// "Buni Ustoz deb chaqir", from an owner replying to a member, names that
// member in every chat; nobody else may set another's profile.
func TestOwnerSetsTheRepliedMembersName(t *testing.T) {
	store := &fakeProfiles{profiles: map[int64]storage.UserProfile{}}
	registry := mustRegistry(t, ProfileTool(ProfileOptions{Store: store}))
	call := func(c Caller, args string) envelope {
		var got envelope
		_ = json.Unmarshal([]byte(registry.Execute(WithTurn(context.Background(), c, &Sink{}), c.UserID, "my_profile", args)), &got)
		return got
	}
	patrick := &Target{MessageID: 29, UserID: 6006, Name: "Patrick @tuturu"}
	owner := Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, Words: "buni Ustoz deb chaqir", Target: patrick}

	got := call(owner, `{"action":"set_name","name":"Ustoz","for_member":true}`)
	if !got.OK || !strings.Contains(string(got.Result), `"member":"Patrick @tuturu"`) || store.profiles[6006].CallName != "Ustoz" {
		t.Fatalf("set_name for the member = %+v %s, profiles %+v", got, got.Result, store.profiles)
	}
	if _, ownChanged := store.profiles[userID]; ownChanged {
		t.Error("the owner's own profile was changed")
	}
	// The bot's answer to the member means that member.
	owner.Target = &Target{MessageID: 31, UserID: 777, IsBot: true, Answered: patrick}
	owner.Words = "unga siz deb gapir"
	if got := call(owner, `{"action":"siz","for_member":true}`); !got.OK || !store.profiles[6006].Siz {
		t.Errorf("siz for the member the bot answered = %+v, profile %+v", got, store.profiles[6006])
	}

	member := Caller{UserID: 5005, ChatID: groupA, Human: true, Words: "buni Boss deb chaqir", Target: patrick}
	if got := call(member, `{"action":"set_name","name":"Boss","for_member":true}`); got.OK {
		t.Errorf("a member set another's name: %+v", got)
	}
	nobody := Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, Words: "uni Boss deb chaqir"}
	if got := call(nobody, `{"action":"set_name","name":"Boss","for_member":true}`); got.OK || !strings.Contains(got.Error.Message, "reply to their message") {
		t.Errorf("for_member without a reply = %+v", got)
	}
	if store.profiles[6006].CallName != "Ustoz" {
		t.Errorf("profile = %+v, want Ustoz kept", store.profiles[6006])
	}
}
