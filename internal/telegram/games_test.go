package telegram

import (
	"context"
	"reflect"
	"testing"
)

func TestSendGameSendsExactParams(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":{"message_id":77}}`)

	id, err := client.SendGame(context.Background(), -100123, "tarozi", 9)
	if err != nil || id != 77 {
		t.Fatalf("SendGame = %d, %v; want 77", id, err)
	}
	want := map[string]any{
		"chat_id":          float64(-100123),
		"game_short_name":  "tarozi",
		"reply_parameters": map[string]any{"message_id": float64(9), "allow_sending_without_reply": true},
	}
	if sent := api.recorded()[0]; sent.method != "sendGame" || !reflect.DeepEqual(sent.params, want) {
		t.Errorf("call = %+v, want sendGame %v", sent, want)
	}
}

func TestAnswerGameCallbackSendsExactParams(t *testing.T) {
	client, api := newTestClient(t, okTrue)

	if err := client.AnswerGameCallback(context.Background(), "q1", "https://example.com/game/?s=x"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"callback_query_id": "q1", "url": "https://example.com/game/?s=x"}
	if sent := api.recorded()[0]; sent.method != "answerCallbackQuery" || !reflect.DeepEqual(sent.params, want) {
		t.Errorf("call = %+v, want answerCallbackQuery %v", sent, want)
	}
}

func TestSetGameScoreSendsExactParams(t *testing.T) {
	cases := map[string]struct {
		game GameMessage
		want map[string]any
	}{
		"in a chat": {GameMessage{UserID: 42, ChatID: -100123, MessageID: 77}, map[string]any{"user_id": float64(42), "chat_id": float64(-100123), "message_id": float64(77), "score": float64(130)}},
		"inline":    {GameMessage{UserID: 42, InlineMessageID: "AbC"}, map[string]any{"user_id": float64(42), "inline_message_id": "AbC", "score": float64(130)}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			client, api := newTestClient(t, `{"ok":true,"result":{"message_id":77}}`)

			improved, err := client.SetGameScore(context.Background(), c.game, 130)
			if err != nil || !improved {
				t.Fatalf("SetGameScore = %v, %v; want improved", improved, err)
			}
			if sent := api.recorded()[0]; sent.method != "setGameScore" || !reflect.DeepEqual(sent.params, c.want) {
				t.Errorf("call = %+v, want setGameScore %v", sent, c.want)
			}
		})
	}
}

// A score no better than the player's best is no error: Telegram keeps the
// best.
func TestSetGameScoreNotBetterIsNoError(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":false,"error_code":400,"description":"Bad Request: BOT_SCORE_NOT_MODIFIED"}`)

	improved, err := client.SetGameScore(context.Background(), GameMessage{UserID: 42, ChatID: 1, MessageID: 2}, 5)
	if err != nil || improved {
		t.Errorf("SetGameScore = %v, %v; want not improved, no error", improved, err)
	}
}

func TestGetGameHighScoresSendsExactParams(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":[{"position":1,"user":{"id":7,"is_bot":false,"first_name":"Ali"},"score":200},{"position":2,"user":{"id":42,"is_bot":false,"first_name":"Bek"},"score":130}]}`)

	scores, err := client.GetGameHighScores(context.Background(), GameMessage{UserID: 42, ChatID: -100123, MessageID: 77})
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 2 || scores[0].Position != 1 || scores[0].User.FirstName != "Ali" || scores[1].Score != 130 {
		t.Errorf("scores = %+v", scores)
	}
	want := map[string]any{"user_id": float64(42), "chat_id": float64(-100123), "message_id": float64(77)}
	if sent := api.recorded()[0]; sent.method != "getGameHighScores" || !reflect.DeepEqual(sent.params, want) {
		t.Errorf("call = %+v, want getGameHighScores %v", sent, want)
	}
}

func TestSetMenuWebAppSendsExactParams(t *testing.T) {
	client, api := newTestClient(t, okTrue, okTrue)

	if err := client.SetMenuWebApp(context.Background(), "Statistika", "https://example.com/stats/"); err != nil {
		t.Fatal(err)
	}
	if err := client.SetMenuCommands(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := api.recorded()
	want := map[string]any{"menu_button": map[string]any{"type": "web_app", "text": "Statistika", "web_app": map[string]any{"url": "https://example.com/stats/"}}}
	if calls[0].method != "setChatMenuButton" || !reflect.DeepEqual(calls[0].params, want) {
		t.Errorf("call = %+v, want setChatMenuButton %v", calls[0], want)
	}
	back := map[string]any{"menu_button": map[string]any{"type": "commands"}}
	if calls[1].method != "setChatMenuButton" || !reflect.DeepEqual(calls[1].params, back) {
		t.Errorf("call = %+v, want setChatMenuButton %v", calls[1], back)
	}
}

// A web_app button goes as Telegram expects it, beside the link buttons.
func TestWebAppButtonSent(t *testing.T) {
	client, api := newTestClient(t, okSent)

	_, err := client.SendMessage(context.Background(), Outgoing{ChatID: 42, Text: "Ochish", Buttons: [][]Button{{{Text: "📊", WebApp: &WebAppInfo{URL: "https://example.com/stats/"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"inline_keyboard": []any{[]any{map[string]any{"text": "📊", "web_app": map[string]any{"url": "https://example.com/stats/"}}}}}
	if got := api.recorded()[0].params["reply_markup"]; !reflect.DeepEqual(got, want) {
		t.Errorf("reply_markup = %v, want %v", got, want)
	}
}
