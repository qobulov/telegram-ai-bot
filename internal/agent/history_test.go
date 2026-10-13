package agent

import (
	"reflect"
	"testing"

	"telegram-ai-bot/internal/ai"
)

func user(content string) ai.Message {
	return ai.Message{Role: ai.RoleUser, Content: content}
}

func result(id string) ai.Message {
	return ai.Message{Role: ai.RoleTool, ToolCallID: id, Content: "{}"}
}

func TestSanitize(t *testing.T) {
	cases := map[string]struct {
		history []ai.Message
		want    []ai.Message
	}{
		"empty": {},
		"plain conversation is kept": {
			history: []ai.Message{user("q"), text("a")},
			want:    []ai.Message{user("q"), text("a")},
		},
		"leading assistant message is dropped": {
			history: []ai.Message{text("a0"), user("q"), text("a")},
			want:    []ai.Message{user("q"), text("a")},
		},
		"leading tool results are dropped": {
			history: []ai.Message{result("x"), text("a0"), user("q")},
			want:    []ai.Message{user("q")},
		},
		"leading tool exchange without its question is dropped": {
			history: []ai.Message{toolCall("x"), result("x"), text("a0"), user("q")},
			want:    []ai.Message{user("q")},
		},
		"complete tool exchange is kept": {
			history: []ai.Message{user("q"), toolCall("x", "y"), result("x"), result("y"), text("a")},
			want:    []ai.Message{user("q"), toolCall("x", "y"), result("x"), result("y"), text("a")},
		},
		"tool exchange missing a result is dropped with its partial results": {
			history: []ai.Message{user("q"), toolCall("x", "y"), result("x"), text("a"), user("q2")},
			want:    []ai.Message{user("q"), text("a"), user("q2")},
		},
		"tool exchange with a mismatched result is dropped": {
			history: []ai.Message{user("q"), toolCall("x"), result("other"), text("a")},
			want:    []ai.Message{user("q"), text("a")},
		},
		"orphan tool result in the middle is dropped": {
			history: []ai.Message{user("q"), result("x"), text("a")},
			want:    []ai.Message{user("q"), text("a")},
		},
		"tool call at the very end without results is dropped": {
			history: []ai.Message{user("q"), toolCall("x")},
			want:    []ai.Message{user("q")},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := sanitize(tc.history)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("sanitize = %+v\nwant       %+v", got, tc.want)
			}
		})
	}
}
