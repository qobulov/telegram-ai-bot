package cards

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

func postBody(t *testing.T, h http.Handler, body map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(raw))))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

// A letter's boxes go to the inventory once; each box opens once, its
// prize drawn by the box's odds; the state counts both for the 🎒 badge.
func TestInboxMailAndBoxes(t *testing.T) {
	now := time.Now()
	store := &fakeStore{mail: []storage.CardMail{{ID: 5, UserID: 7, Note: "Sovg'a", Box: "box", Boxes: 2, Tokens: 3}}}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }, BoxRoll: func(n int) int { return n - 1 }})
	init := signedInit(7, now)

	state := postBody(t, h, map[string]any{"init_data": init, "action": "state"})
	if inbox, _ := state["inbox"].(map[string]any); inbox["mail"] != 1.0 || inbox["boxes"] != 0.0 {
		t.Fatalf("state inbox = %v", state["inbox"])
	}
	in := postBody(t, h, map[string]any{"init_data": init, "action": "inbox"})
	if mail := in["mail"].([]any); len(mail) != 1 || mail[0].(map[string]any)["note"] != "Sovg'a" {
		t.Fatalf("inbox = %v", in)
	}
	claim := postBody(t, h, map[string]any{"init_data": init, "action": "mail_claim", "offer": 5})
	if claim["boxes"] != 2.0 || claim["tokens_given"] != 3.0 {
		t.Fatalf("claim = %v", claim)
	}
	if again := postBody(t, h, map[string]any{"init_data": init, "action": "mail_claim", "offer": 5}); again["refused"] != "claimed" {
		t.Fatalf("a second claim = %v", again)
	}
	open := postBody(t, h, map[string]any{"init_data": init, "action": "box_open", "item": "box"})
	if open["won"] != 5000.0 || open["left"] != 1.0 || open["least"] != 1500.0 {
		t.Fatalf("open = %v", open)
	}
	postBody(t, h, map[string]any{"init_data": init, "action": "box_open", "item": "box"})
	if none := postBody(t, h, map[string]any{"init_data": init, "action": "box_open", "item": "box"}); none["refused"] != "none" {
		t.Fatalf("a third open = %v", none)
	}
	if bad := postBody(t, h, map[string]any{"init_data": init, "action": "box_open", "item": "c300"}); bad["error"] == nil {
		t.Fatalf("a plain pack opened as a box: %v", bad)
	}
}
