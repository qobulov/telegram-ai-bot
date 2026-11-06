package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestBusinessUpdatesAreDecoded(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":[
		{"update_id":1,"business_connection":{"id":"bc1","user":{"id":7,"first_name":"Owner"},"user_chat_id":7,"date":100,
			"rights":{"can_reply":true,"can_manage_stories":true},"is_enabled":true}},
		{"update_id":2,"business_message":{"message_id":5,"business_connection_id":"bc1","from":{"id":42,"first_name":"Ali"},
			"chat":{"id":42,"type":"private","first_name":"Ali"},"text":"narxi qancha?"}},
		{"update_id":3,"business_message":{"message_id":6,"business_connection_id":"bc1","from":{"id":7},
			"chat":{"id":42,"type":"private"},"sender_business_bot":{"id":99,"is_bot":true},"text":"salom"}}]}`)

	updates, err := client.GetUpdates(context.Background(), 0, 0)
	if err != nil || len(updates) != 3 {
		t.Fatalf("updates = %+v, err = %v", updates, err)
	}
	connection := updates[0].BusinessConnection
	if connection == nil || connection.ID != "bc1" || connection.User.ID != 7 || connection.UserChatID != 7 || !connection.CanReply() || !connection.CanManageStories() {
		t.Errorf("connection = %+v", connection)
	}
	msg := updates[1].BusinessMessage
	if msg == nil || msg.BusinessConnectionID != "bc1" || msg.From.ID != 42 || msg.Chat.FirstName != "Ali" || msg.Text != "narxi qancha?" || msg.SenderBusinessBot != nil {
		t.Errorf("business message = %+v", msg)
	}
	if bot := updates[2].BusinessMessage.SenderBusinessBot; bot == nil || bot.ID != 99 {
		t.Errorf("sender_business_bot = %+v", bot)
	}
}

func TestBusinessConnectionRights(t *testing.T) {
	cases := []struct {
		name               string
		connection         BusinessConnection
		canReply, canStory bool
	}{
		{"both", BusinessConnection{IsEnabled: true, Rights: &BusinessBotRights{CanReply: true, CanManageStories: true}}, true, true},
		{"no rights", BusinessConnection{IsEnabled: true}, false, false},
		{"disabled", BusinessConnection{Rights: &BusinessBotRights{CanReply: true, CanManageStories: true}}, false, false},
		{"reply only", BusinessConnection{IsEnabled: true, Rights: &BusinessBotRights{CanReply: true}}, true, false},
	}
	for _, c := range cases {
		if c.connection.CanReply() != c.canReply || c.connection.CanManageStories() != c.canStory {
			t.Errorf("%s: reply %v, stories %v", c.name, c.connection.CanReply(), c.connection.CanManageStories())
		}
	}
}

func TestBusinessMethodsSendExactParams(t *testing.T) {
	ctx := context.Background()
	checklist := Checklist{BusinessConnectionID: "bc1", ChatID: 42, Title: "Buyurtma", Tasks: []string{"To'lov", "Yetkazish"}}
	wantChecklist := map[string]any{
		"title": "Buyurtma",
		"tasks": []any{
			map[string]any{"id": float64(1), "text": "To'lov"},
			map[string]any{"id": float64(2), "text": "Yetkazish"},
		},
		"others_can_add_tasks":          true,
		"others_can_mark_tasks_as_done": true,
	}
	cases := []struct {
		name   string
		reply  string
		call   func(*Client) error
		method string
		params map[string]any
	}{
		{
			name:  "getBusinessConnection",
			reply: `{"ok":true,"result":{"id":"bc1","user":{"id":7},"user_chat_id":7,"date":1,"is_enabled":true}}`,
			call: func(c *Client) error {
				got, err := c.GetBusinessConnection(ctx, "bc1")
				if err == nil && (got.ID != "bc1" || got.User.ID != 7 || !got.IsEnabled) {
					t.Errorf("connection = %+v", got)
				}
				return err
			},
			method: "getBusinessConnection",
			params: map[string]any{"business_connection_id": "bc1"},
		},
		{
			name:  "sendChecklist",
			reply: `{"ok":true,"result":{"message_id":31}}`,
			call: func(c *Client) error {
				id, err := c.SendChecklist(ctx, checklist)
				if err == nil && id != 31 {
					t.Errorf("id = %d", id)
				}
				return err
			},
			method: "sendChecklist",
			params: map[string]any{"business_connection_id": "bc1", "chat_id": float64(42), "checklist": wantChecklist},
		},
		{
			name:   "editMessageChecklist",
			reply:  `{"ok":true,"result":{"message_id":31}}`,
			call:   func(c *Client) error { return c.EditMessageChecklist(ctx, 31, checklist) },
			method: "editMessageChecklist",
			params: map[string]any{"business_connection_id": "bc1", "chat_id": float64(42), "message_id": float64(31), "checklist": wantChecklist},
		},
		{
			name:  "sendMessage on behalf of a business",
			reply: okSent,
			call: func(c *Client) error {
				_, err := c.SendMessage(ctx, Outgoing{ChatID: 42, Text: "salom", BusinessConnectionID: "bc1"})
				return err
			},
			method: "sendMessage",
			params: map[string]any{"business_connection_id": "bc1", "chat_id": float64(42), "text": "salom"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, api := newTestClient(t, c.reply)
			if err := c.call(client); err != nil {
				t.Fatalf("call: %v", err)
			}
			calls := api.recorded()
			if len(calls) != 1 || calls[0].method != c.method || !reflect.DeepEqual(calls[0].params, c.params) {
				t.Errorf("calls = %+v, want %s %v", calls, c.method, c.params)
			}
		})
	}
}

// formRecorder answers a multipart upload with reply and keeps its fields
// and the file in part.
func formRecorder(t *testing.T, part, reply string) (*Client, *map[string]string, *[]byte) {
	t.Helper()
	fields := map[string]string{}
	var file []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields["method"] = strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not a multipart upload: %v", err)
		}
		for name, values := range r.MultipartForm.Value {
			fields[name] = values[0]
		}
		if f, _, err := r.FormFile(part); err == nil {
			file, _ = io.ReadAll(f)
		}
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(server.Close)
	return NewClient(Options{Token: testToken, BaseURL: server.URL}), &fields, &file
}

func TestPostStoryUploadsThePicture(t *testing.T) {
	client, fields, file := formRecorder(t, "story", `{"ok":true,"result":{"chat":{"id":7,"type":"private"},"id":12}}`)

	id, err := client.PostStory(context.Background(), Story{BusinessConnectionID: "bc1", Photo: []byte("JPEG"), Caption: "**Yangi** kolleksiya", ActivePeriod: 86400})
	if err != nil || id != 12 {
		t.Fatalf("id = %d, err = %v", id, err)
	}
	got := *fields
	if got["method"] != "postStory" || got["business_connection_id"] != "bc1" || got["active_period"] != "86400" || got["caption"] != "Yangi kolleksiya" || got["caption_entities"] == "" || string(*file) != "JPEG" {
		t.Errorf("form = %v, file = %q", got, *file)
	}
	var content map[string]string
	if err := json.Unmarshal([]byte(got["content"]), &content); err != nil || content["type"] != "photo" || content["photo"] != "attach://story" {
		t.Errorf("content = %s (%v)", got["content"], err)
	}
	if _, ok := got["chat_id"]; ok {
		t.Errorf("a story names no chat: %v", got)
	}
}

func TestPostStoryWithoutCaptionSendsNone(t *testing.T) {
	client, fields, _ := formRecorder(t, "story", `{"ok":true,"result":{"chat":{"id":7,"type":"private"},"id":3}}`)
	if _, err := client.PostStory(context.Background(), Story{BusinessConnectionID: "bc1", Photo: []byte("J"), Caption: "  ", ActivePeriod: 6 * 3600}); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*fields)["caption"]; ok {
		t.Errorf("form = %v", *fields)
	}
}

func TestPicturesGoOnBehalfOfABusiness(t *testing.T) {
	client, fields, _ := formRecorder(t, "photo", okSent)
	if _, err := client.SendPhoto(context.Background(), Photo{ChatID: 42, Data: []byte("P"), BusinessConnectionID: "bc1"}); err != nil {
		t.Fatal(err)
	}
	if (*fields)["business_connection_id"] != "bc1" || (*fields)["chat_id"] != "42" {
		t.Errorf("sendPhoto form = %v", *fields)
	}

	client, fields, _ = formRecorder(t, "photo0", `{"ok":true,"result":[{"message_id":1},{"message_id":2}]}`)
	if _, err := client.SendMediaGroup(context.Background(), Album{ChatID: 42, Photos: [][]byte{[]byte("A"), []byte("B")}, BusinessConnectionID: "bc1"}); err != nil {
		t.Fatal(err)
	}
	if (*fields)["business_connection_id"] != "bc1" {
		t.Errorf("sendMediaGroup form = %v", *fields)
	}
}

func TestPicturesOutsideABusinessNameNoConnection(t *testing.T) {
	client, fields, _ := formRecorder(t, "photo", okSent)
	if _, err := client.SendPhoto(context.Background(), Photo{ChatID: 42, Data: []byte("P")}); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*fields)["business_connection_id"]; ok {
		t.Errorf("sendPhoto form = %v", *fields)
	}
}
