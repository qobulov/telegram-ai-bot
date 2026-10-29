package telegram

import (
	"context"
	"reflect"
	"testing"
)

// everyKind is allowed_updates as Telegram is sent it: every kind of update
// the bot handles, in order.
var everyKind = []any{"message", "my_chat_member", "chat_member", "callback_query", "inline_query", "pre_checkout_query", "edited_message", "chat_join_request"}

func TestSecurityMethodsSendExactParams(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		method string
		call   func(*Client) error
		want   map[string]any
	}{
		{
			method: "deleteMessages",
			call:   func(c *Client) error { return c.DeleteMessages(ctx, -100123, []int64{30, 31}) },
			want:   map[string]any{"chat_id": float64(-100123), "message_ids": []any{float64(30), float64(31)}},
		},
		{
			method: "banChatSenderChat",
			call:   func(c *Client) error { return c.BanChatSenderChat(ctx, -100123, -100777) },
			want:   map[string]any{"chat_id": float64(-100123), "sender_chat_id": float64(-100777)},
		},
		{
			method: "unbanChatSenderChat",
			call:   func(c *Client) error { return c.UnbanChatSenderChat(ctx, -100123, -100777) },
			want:   map[string]any{"chat_id": float64(-100123), "sender_chat_id": float64(-100777)},
		},
		{
			method: "approveChatJoinRequest",
			call:   func(c *Client) error { return c.ApproveChatJoinRequest(ctx, -100123, 42) },
			want:   map[string]any{"chat_id": float64(-100123), "user_id": float64(42)},
		},
		{
			method: "declineChatJoinRequest",
			call:   func(c *Client) error { return c.DeclineChatJoinRequest(ctx, -100123, 42) },
			want:   map[string]any{"chat_id": float64(-100123), "user_id": float64(42)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			client, api := newTestClient(t, okTrue)

			if err := tt.call(client); err != nil {
				t.Fatalf("%s: %v", tt.method, err)
			}
			sent := api.recorded()[0]
			if sent.method != tt.method || !reflect.DeepEqual(sent.params, tt.want) {
				t.Errorf("call = %+v, want %s %v", sent, tt.method, tt.want)
			}
		})
	}
}

func TestGetUpdatesParsesEditsAndJoinRequests(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":[
		{"update_id":8,"edited_message":{"message_id":3,"from":{"id":42},"chat":{"id":-100123,"type":"supergroup"},"text":"new words"}},
		{"update_id":9,"chat_join_request":{"chat":{"id":-100123,"type":"supergroup","title":"Lobby"},
			"from":{"id":42,"first_name":"Ann","language_code":"ru"},"user_chat_id":42,"date":1700000000}}]}`)

	updates, err := client.GetUpdates(context.Background(), 8, 0)
	if err != nil || len(updates) != 2 {
		t.Fatalf("updates = %+v, %v", updates, err)
	}
	if edited := updates[0].EditedMessage; edited == nil || updates[0].Message != nil || edited.Text != "new words" || edited.Chat.ID != -100123 {
		t.Errorf("edit = %+v", updates[0])
	}
	request := updates[1].ChatJoinRequest
	if request == nil || request.Chat.Title != "Lobby" || request.From == nil || request.From.ID != 42 ||
		request.From.LanguageCode != "ru" || request.UserChatID != 42 || request.Date != 1700000000 {
		t.Errorf("join request = %+v", request)
	}
}
