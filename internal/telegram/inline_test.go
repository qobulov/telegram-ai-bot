package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInlineQueryUpdateIsDecoded(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":[{"update_id":9,"inline_query":{
		"id":"q1","from":{"id":42,"first_name":"Ann","language_code":"uz"},"query":"salom?","chat_type":"supergroup","offset":""}}]}`)

	updates, err := client.GetUpdates(context.Background(), 0, 0)
	if err != nil || len(updates) != 1 {
		t.Fatalf("updates = %+v, %v", updates, err)
	}
	q := updates[0].InlineQuery
	if q == nil || q.ID != "q1" || q.From == nil || q.From.ID != 42 || q.Query != "salom?" || q.ChatType != "supergroup" {
		t.Errorf("inline query = %+v", q)
	}
}

// An answer is personal, sends its cache time even when it is 0, and each
// article posts its text, formatted, with the text's start under the
// title when no description is given.
func TestAnswerInlineQuerySendsExactParams(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":true}`)

	err := client.AnswerInlineQuery(context.Background(), InlineAnswer{
		QueryID:  "q1",
		Articles: []InlineArticle{{ID: "answer", Title: "Maria", Text: "**Toshkent** — poytaxt."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sent := api.recorded()[0]
	encoded, _ := json.Marshal(sent.params)
	want := `{"cache_time":0,"inline_query_id":"q1","is_personal":true,"results":[{"description":"Toshkent — poytaxt.","id":"answer","input_message_content":{"entities":[{"length":8,"offset":0,"type":"bold"}],"message_text":"Toshkent — poytaxt."},"title":"Maria","type":"article"}]}`
	if sent.method != "answerInlineQuery" || string(encoded) != want {
		t.Errorf("call %s:\n%s\nwant\n%s", sent.method, encoded, want)
	}
}

// An article posts one message, so a longer text is cut to one, and a long
// description to a line.
func TestAnswerInlineQueryCutsLongTexts(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":true}`)

	long := strings.Repeat("so'z ", 2000)
	if err := client.AnswerInlineQuery(context.Background(), InlineAnswer{QueryID: "q", Articles: []InlineArticle{{ID: "a", Title: "t", Text: long}}}); err != nil {
		t.Fatal(err)
	}
	result := api.recorded()[0].params["results"].([]any)[0].(map[string]any)
	text := result["input_message_content"].(map[string]any)["message_text"].(string)
	if n := len([]rune(text)); n == 0 || n > MessageLimit {
		t.Errorf("message_text has %d characters, want 1 to %d", n, MessageLimit)
	}
	if n := len([]rune(result["description"].(string))); n > inlineDescriptionRunes {
		t.Errorf("description has %d characters, want at most %d", n, inlineDescriptionRunes)
	}
}
