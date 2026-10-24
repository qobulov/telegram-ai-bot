package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBotProfileTextIsSetPerLanguage(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":true}`)
	ctx := context.Background()
	if err := client.SetBotName(ctx, "Maria", "uz"); err != nil {
		t.Fatal(err)
	}
	if err := client.SetBotBio(ctx, "MLBB va kino", ""); err != nil {
		t.Fatal(err)
	}
	if err := client.SetBotDescription(ctx, "Salom!", "ru"); err != nil {
		t.Fatal(err)
	}
	calls := api.recorded()
	if len(calls) != 3 || calls[0].method != "setMyName" || calls[0].params["name"] != "Maria" || calls[0].params["language_code"] != "uz" ||
		calls[1].method != "setMyShortDescription" || calls[1].params["short_description"] != "MLBB va kino" || calls[1].params["language_code"] != nil ||
		calls[2].method != "setMyDescription" || calls[2].params["description"] != "Salom!" || calls[2].params["language_code"] != "ru" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestGetBotProfileReadsAllThree(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":{"name":"Maria"}}`, `{"ok":true,"result":{"short_description":"bio"}}`, `{"ok":true,"result":{"description":"about"}}`)
	profile, err := client.GetBotProfile(context.Background(), "")
	if err != nil || profile != (BotProfile{Name: "Maria", Bio: "bio", Description: "about"}) {
		t.Errorf("profile = %+v, %v", profile, err)
	}
}

func TestSetBotPhotoUploadsAStaticPhoto(t *testing.T) {
	var method, photo string
	var data []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not a multipart upload: %v", err)
		}
		photo = r.FormValue("photo")
		if file, _, err := r.FormFile("avatar"); err == nil {
			data, _ = io.ReadAll(file)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	client := NewClient(Options{Token: testToken, BaseURL: server.URL})

	if err := client.SetBotPhoto(context.Background(), []byte("JPEG")); err != nil {
		t.Fatal(err)
	}
	if method != "setMyProfilePhoto" || photo != `{"type":"static","photo":"attach://avatar"}` || string(data) != "JPEG" {
		t.Errorf("method %q, photo %q, data %q", method, photo, data)
	}
}
