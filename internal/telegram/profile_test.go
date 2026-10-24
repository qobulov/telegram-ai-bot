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

// The profile photo is downloaded in the size nearest 640 pixels.
func TestProfilePhotoTakesTheSizeNear640(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot" + testToken + "/getUserProfilePhotos":
			_, _ = io.WriteString(w, `{"ok":true,"result":{"total_count":1,"photos":[[
				{"file_id":"small","file_unique_id":"u1","width":160,"height":160},
				{"file_id":"file-1","file_unique_id":"u2","width":640,"height":640},
				{"file_id":"big","file_unique_id":"u3","width":1280,"height":1280}]]}}`)
		case "/bot" + testToken + "/getFile":
			_, _ = io.WriteString(w, `{"ok":true,"result":{"file_id":"file-1","file_size":4,"file_path":"photos/a.jpg"}}`)
		case "/file/bot" + testToken + "/photos/a.jpg":
			_, _ = io.WriteString(w, "JPEG")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(Options{Token: testToken, BaseURL: server.URL})

	data, id, ok, err := client.ProfilePhoto(context.Background(), 777, 1<<20)
	if err != nil || !ok || string(data) != "JPEG" || id != "u2" {
		t.Errorf("photo = %q, %q, %v, %v", data, id, ok, err)
	}
}
