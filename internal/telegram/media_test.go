package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendMediaGroupUploadsEveryPicture(t *testing.T) {
	var got struct {
		method, chatID, media, reply string
		photos                       []string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not a multipart upload: %v", err)
		}
		got.chatID, got.media, got.reply = r.FormValue("chat_id"), r.FormValue("media"), r.FormValue("reply_parameters")
		for _, name := range []string{"photo0", "photo1", "photo2"} {
			if file, _, err := r.FormFile(name); err == nil {
				data, _ := io.ReadAll(file)
				got.photos = append(got.photos, string(data))
			}
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":[{"message_id":70},{"message_id":71},{"message_id":72}]}`)
	}))
	defer server.Close()
	client := NewClient(Options{Token: testToken, BaseURL: server.URL})

	id, err := client.SendMediaGroup(context.Background(), Album{ChatID: -100, ReplyToMessageID: 9, Photos: [][]byte{[]byte("A"), []byte("B"), []byte("C")}, Caption: "**Uchta** rasm"})
	if err != nil || id != 70 {
		t.Fatalf("id = %d, err = %v", id, err)
	}
	if got.method != "sendMediaGroup" || got.chatID != "-100" || strings.Join(got.photos, "") != "ABC" || !strings.Contains(got.reply, `"message_id":9`) {
		t.Errorf("request = %+v", got)
	}
	var media []map[string]any
	if err := json.Unmarshal([]byte(got.media), &media); err != nil || len(media) != 3 {
		t.Fatalf("media = %s (%v)", got.media, err)
	}
	if media[0]["media"] != "attach://photo0" || media[2]["media"] != "attach://photo2" || media[0]["caption"] != "Uchta rasm" || media[0]["caption_entities"] == nil {
		t.Errorf("media = %s", got.media)
	}
	if _, ok := media[1]["caption"]; ok {
		t.Errorf("second picture has a caption: %s", got.media)
	}
}

func TestSendMediaGroupRefusesASizeTelegramDoesNot(t *testing.T) {
	client, api := newTestClient(t, okSent)
	for _, n := range []int{1, 11} {
		if _, err := client.SendMediaGroup(context.Background(), Album{ChatID: 1, Photos: make([][]byte, n)}); err == nil {
			t.Errorf("%d pictures: no error", n)
		}
	}
	if calls := api.recorded(); len(calls) != 0 {
		t.Errorf("calls = %+v, want none", calls)
	}
}

func TestSendDiceReturnsTheValue(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"message_id":31,"dice":{"emoji":"🎯","value":6}}}`)

	id, value, err := client.SendDice(context.Background(), -100, "🎯", 30)
	if err != nil || id != 31 || value != 6 {
		t.Fatalf("id = %d, value = %d, err = %v", id, value, err)
	}
	sent := api.recorded()[0]
	encoded, _ := json.Marshal(sent.params)
	want := `{"chat_id":-100,"emoji":"🎯","reply_parameters":{"allow_sending_without_reply":true,"message_id":30}}`
	if sent.method != "sendDice" || string(encoded) != want {
		t.Errorf("call %s %s, want %s", sent.method, encoded, want)
	}
}
