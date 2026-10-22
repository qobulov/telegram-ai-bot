package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestFormattedMarksBoldCodeHeadingsAndBullets(t *testing.T) {
	for name, c := range map[string]struct {
		in, text string
		entities []messageEntity
	}{
		"plain":   {"Salom, senpai", "Salom, senpai", nil},
		"bold":    {"**Ling**: 52.8% win", "Ling: 52.8% win", []messageEntity{{"bold", 0, 4}}},
		"code":    {"ID `1407157025`", "ID 1407157025", []messageEntity{{"code", 3, 10}}},
		"bullets": {"Meta:\n* **Masha** 60%\n- Marcel", "Meta:\n• Masha 60%\n• Marcel", []messageEntity{{"bold", 8, 5}}},
		"heading": {"## Ling\nAssassin", "Ling\nAssassin", []messageEntity{{"bold", 0, 4}}},
		// An emoji is two UTF-16 units, as Telegram counts offsets.
		"emoji":     {"🔥 **Fanny**", "🔥 Fanny", []messageEntity{{"bold", 3, 5}}},
		"unmatched": {"2 ** 3 = 8, *ok*", "2 ** 3 = 8, *ok*", nil},
		"two lines": {"**a\nb**", "**a\nb**", nil},
	} {
		text, entities := formatted(c.in)
		if text != c.text || !reflect.DeepEqual(entities, c.entities) {
			t.Errorf("%s: got %q %+v, want %q %+v", name, text, entities, c.text, c.entities)
		}
	}
}

func TestSendMessageSendsEntities(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"message_id":5}}`)
	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 1, Text: "**Ling** zo'r"}); err != nil {
		t.Fatal(err)
	}
	sent := api.recorded()[0]
	entities, _ := sent.params["entities"].([]any)
	if sent.params["text"] != "Ling zo'r" || len(entities) != 1 {
		t.Errorf("params = %+v", sent.params)
	}
	client, api = newTestClient(t, `{"ok":true,"result":{"message_id":6}}`)
	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 1, Text: "salom"}); err != nil {
		t.Fatal(err)
	}
	if _, has := api.recorded()[0].params["entities"]; has {
		t.Error("plain text was sent with entities")
	}
}

func TestSendPhotoSendsCaptionEntities(t *testing.T) {
	var caption, entities string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		caption, entities = r.FormValue("caption"), r.FormValue("caption_entities")
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	defer server.Close()
	client := NewClient(Options{Token: testToken, BaseURL: server.URL})

	if _, err := client.SendPhoto(context.Background(), Photo{ChatID: 1, Data: []byte("x"), Caption: "**Ling** rasmi"}); err != nil {
		t.Fatal(err)
	}
	if caption != "Ling rasmi" || entities != `[{"type":"bold","offset":0,"length":4}]` {
		t.Errorf("caption %q, entities %q", caption, entities)
	}
}
