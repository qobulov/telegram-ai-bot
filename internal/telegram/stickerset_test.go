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

// The sticker's PNG is uploaded for its set's owner, with no chat.
func TestUploadStickerFileUploadsAStaticPNG(t *testing.T) {
	var method, user, format, chat string
	var data []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not a multipart upload: %v", err)
		}
		user, format, chat = r.FormValue("user_id"), r.FormValue("sticker_format"), r.FormValue("chat_id")
		if file, _, err := r.FormFile("sticker"); err == nil {
			data, _ = io.ReadAll(file)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"file_id":"up1","file_unique_id":"u1"}}`)
	}))
	defer server.Close()
	client := NewClient(Options{Token: testToken, BaseURL: server.URL})

	id, err := client.UploadStickerFile(context.Background(), 42, []byte("PNG"))
	if err != nil || id != "up1" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
	if method != "uploadStickerFile" || user != "42" || format != "static" || chat != "" || string(data) != "PNG" {
		t.Errorf("method %q, user %q, format %q, chat %q, data %q", method, user, format, chat, data)
	}
}

func TestStickerSetCallsSendExactlyTheirParams(t *testing.T) {
	sticker := InputSticker{FileID: "up1", Emoji: []string{"😂", "🔥"}}
	for _, tc := range []struct {
		method string
		do     func(*Client) error
		want   string
	}{
		{"createNewStickerSet", func(c *Client) error {
			return c.CreateNewStickerSet(context.Background(), 42, "maria_by_mariabot", "Maria", sticker)
		}, `{"name":"maria_by_mariabot","stickers":[{"emoji_list":["😂","🔥"],"format":"static","sticker":"up1"}],"title":"Maria","user_id":42}`},
		{"addStickerToSet", func(c *Client) error {
			return c.AddStickerToSet(context.Background(), 42, "maria_by_mariabot", sticker)
		}, `{"name":"maria_by_mariabot","sticker":{"emoji_list":["😂","🔥"],"format":"static","sticker":"up1"},"user_id":42}`},
		{"deleteStickerFromSet", func(c *Client) error {
			return c.DeleteStickerFromSet(context.Background(), "f9")
		}, `{"sticker":"f9"}`},
		{"setStickerSetTitle", func(c *Client) error {
			return c.SetStickerSetTitle(context.Background(), "maria_by_mariabot", "Maria stikerlari")
		}, `{"name":"maria_by_mariabot","title":"Maria stikerlari"}`},
	} {
		client, api := newTestClient(t, `{"ok":true,"result":true}`)
		if err := tc.do(client); err != nil {
			t.Fatalf("%s: %v", tc.method, err)
		}
		sent := api.recorded()[0]
		encoded, _ := json.Marshal(sent.params)
		if sent.method != tc.method || string(encoded) != tc.want {
			t.Errorf("call %s %s, want %s %s", sent.method, encoded, tc.method, tc.want)
		}
	}
}
