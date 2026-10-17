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
