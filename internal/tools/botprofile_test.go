package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
)

type fakeBotAccount struct {
	calls []string
	photo []byte
	err   error
}

func (f *fakeBotAccount) GetBotProfile(_ context.Context, language string) (telegram.BotProfile, error) {
	return telegram.BotProfile{Name: "Maria " + language}, f.err
}

func (f *fakeBotAccount) SetBotName(_ context.Context, name, language string) error {
	f.calls = append(f.calls, "name "+language+": "+name)
	return f.err
}

func (f *fakeBotAccount) SetBotBio(_ context.Context, bio, language string) error {
	f.calls = append(f.calls, "bio "+language+": "+bio)
	return f.err
}

func (f *fakeBotAccount) SetBotDescription(_ context.Context, description, language string) error {
	f.calls = append(f.calls, "description "+language+": "+description)
	return f.err
}

func (f *fakeBotAccount) SetBotPhoto(_ context.Context, jpeg []byte) error {
	f.photo = jpeg
	return f.err
}

func (f *fakeBotAccount) RemoveBotPhoto(context.Context) error {
	f.calls = append(f.calls, "remove photo")
	return f.err
}

func botProfile(t *testing.T, account *fakeBotAccount, caller Caller, args string) envelope {
	t.Helper()
	registry := mustRegistry(t, BotProfileTool(account))
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(WithTurn(context.Background(), caller, &Sink{}), userID, "bot_profile", args)), &env)
	return env
}

var botOwner = Caller{UserID: userID, ChatID: userID, Owner: true, UpdateID: 1}

func TestBotProfileIsOnlyForOwners(t *testing.T) {
	account := &fakeBotAccount{}
	env := botProfile(t, account, Caller{UserID: userID, ChatID: groupA, UpdateID: 1}, `{"action":"name","text":"Hacked"}`)
	if env.OK || len(account.calls) != 0 {
		t.Errorf("a member changed the name: %+v, %q", env, account.calls)
	}
}

func TestBotProfileSetsTextsPerLanguage(t *testing.T) {
	account := &fakeBotAccount{}
	for _, args := range []string{
		`{"action":"name","text":" Maria ✨ "}`,
		`{"action":"bio","text":"MLBB, kino va musiqa","language":"uz"}`,
		`{"action":"description","text":"Привет!","language":"ru"}`,
		`{"action":"remove_photo"}`,
	} {
		if env := botProfile(t, account, botOwner, args); !env.OK {
			t.Fatalf("%s: %+v", args, env)
		}
	}
	want := []string{"name : Maria ✨", "bio uz: MLBB, kino va musiqa", "description ru: Привет!", "remove photo"}
	if strings.Join(account.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %q", account.calls)
	}
	if env := botProfile(t, account, botOwner, `{"action":"show"}`); !env.OK || !strings.Contains(string(env.Result), `"Maria uz"`) {
		t.Errorf("show = %+v", env)
	}
}

func TestBotProfileChecksTheText(t *testing.T) {
	account := &fakeBotAccount{}
	for _, args := range []string{
		`{"action":"bio","text":"` + strings.Repeat("x", 121) + `"}`,
		`{"action":"name","text":""}`,
	} {
		if env := botProfile(t, account, botOwner, args); env.OK || env.Error.Code != "invalid_arguments" {
			t.Errorf("%s: %+v", args[:30], env)
		}
	}
	if len(account.calls) != 0 {
		t.Errorf("calls = %q", account.calls)
	}
}

// A photo is the picture in the message, made a JPEG if it is not one.
func TestBotProfilePhotoIsThePictureAsAJPEG(t *testing.T) {
	account := &fakeBotAccount{}
	if env := botProfile(t, account, botOwner, `{"action":"photo"}`); env.OK || !strings.Contains(env.Error.Message, "no picture") {
		t.Errorf("without a picture: %+v", env)
	}

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	withPicture := botOwner
	withPicture.Picture = pngData.Bytes()
	if env := botProfile(t, account, withPicture, `{"action":"photo"}`); !env.OK || !bytes.HasPrefix(account.photo, []byte{0xFF, 0xD8, 0xFF}) {
		t.Errorf("photo = %+v, sent %d bytes", env, len(account.photo))
	}
}

// Telegram's reason reaches the owner, such as when the name may change again.
func TestBotProfileSaysWhyTelegramRefused(t *testing.T) {
	account := &fakeBotAccount{err: &telegram.APIError{Code: 429, Description: "Too Many Requests: retry after 3600", RetryAfter: time.Hour}}
	env := botProfile(t, account, botOwner, `{"action":"name","text":"Mira"}`)
	if env.OK || !strings.Contains(env.Error.Message, "Too Many Requests") || !strings.Contains(env.Error.Message, "1h0m0s") {
		t.Errorf("env = %+v", env)
	}
}
