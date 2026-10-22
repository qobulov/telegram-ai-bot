package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
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

func TestIsRich(t *testing.T) {
	for text, want := range map[string]bool{
		"Salom, senpai":                             false,
		"**Ling** zo'r":                             false,
		"## Ling\nAssassin":                         true,
		"Meta:\n- Masha\n- Marcel":                  true,
		"Top:\n1. Masha\n2. Marcel":                 true,
		"| Hero | Win |\n|---|---|\n| Ling | 52% |": true,
		"> iqtibos":                                 true,
		"2-3 kun":                                   false,
		strings.Repeat("- x\n", RichLimit):          false,
	} {
		if got := isRich(text); got != want {
			t.Errorf("isRich(%.30q) = %v, want %v", text, got, want)
		}
	}
}

// richServer answers sendRichMessage with rich and anything else with OK,
// and records the methods called.
func richServer(t *testing.T, rich string) (*Client, *[]string) {
	t.Helper()
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		methods = append(methods, method)
		if method == "sendRichMessage" {
			_, _ = io.WriteString(w, rich)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":2}}`)
	}))
	t.Cleanup(server.Close)
	return NewClient(Options{Token: testToken, BaseURL: server.URL}), &methods
}

func TestSendMessageSendsStructureAsARichMessage(t *testing.T) {
	client, methods := richServer(t, `{"ok":true,"result":{"message_id":1}}`)
	id, err := client.SendMessage(context.Background(), Outgoing{ChatID: 1, Text: "## Ling\n- Assassin"})
	if err != nil || id != 1 || strings.Join(*methods, ",") != "sendRichMessage" {
		t.Errorf("id %d, err %v, methods %v", id, err, *methods)
	}
}

// A rich message Telegram refuses goes as ordinary text; a rate limit does
// not, as the plain one would be refused too.
func TestSendMessageFallsBackFromARefusedRichMessage(t *testing.T) {
	client, methods := richServer(t, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse rich message"}`)
	id, err := client.SendMessage(context.Background(), Outgoing{ChatID: 1, Text: "## Ling\n- Assassin"})
	if err != nil || id != 2 || strings.Join(*methods, ",") != "sendRichMessage,sendMessage" {
		t.Errorf("id %d, err %v, methods %v", id, err, *methods)
	}
	client, methods = richServer(t, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`)
	if _, err := client.SendMessage(context.Background(), Outgoing{ChatID: 1, Text: "## Ling\n- Assassin"}); err == nil || slices.Contains(*methods, "sendMessage") {
		t.Errorf("err %v, methods %v, want the rate limit returned", err, *methods)
	}
}
