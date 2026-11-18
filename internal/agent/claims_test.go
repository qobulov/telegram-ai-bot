package agent

import (
	"context"
	"testing"

	"telegram-ai-bot/internal/ai"
)

// moderateTools answers every call with the next of results.
type moderateTools struct {
	results []string
}

func (m *moderateTools) Definitions(context.Context, int64) []ai.ToolDefinition {
	return []ai.ToolDefinition{{Type: "function", Function: ai.FunctionDefinition{Name: moderateTool}}}
}

func (m *moderateTools) Execute(context.Context, int64, string, string) string {
	result := m.results[0]
	m.results = m.results[1:]
	return result
}

func moderateCall(id string) ai.Message {
	return ai.Message{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: id, Type: "function", Function: ai.FunctionCall{Name: moderateTool, Arguments: `{"action":"mute"}`}}}}
}

const (
	refusedMute = `{"ok":false,"error":{"code":"forbidden","message":"their own message does not ask for this","said":"So'zingizda buyruq ko'rinmadi."}}`
	doneMute    = `{"ok":true,"result":{"done":true,"said":"Bajarildi: Bek 15:04 gacha yoza olmaydi."}}`
)

func replyWith(t *testing.T, results []string, replies ...ai.Message) string {
	t.Helper()
	model := &scriptedModel{replies: replies}
	a := New(Options{Store: newMemoryStore(), Model: model, Tools: &moderateTools{results: results}, MaxHistory: 20, MaxToolCalls: 3})
	answer, err := a.Reply(context.Background(), request(1, "Bekni mut qil"))
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	return answer.Text
}

// When moderate refused and the model still says it was done, the tool's
// own line is sent instead; any other answer, and one after a success,
// stays.
func TestRefusedModerationIsNeverSaidDone(t *testing.T) {
	cases := []struct {
		name    string
		results []string
		replies []ai.Message
		want    string
	}{
		{"claims done", []string{refusedMute}, []ai.Message{moderateCall("a"), text("Bajarildi, 1 daqiqaga mutesiz 😂")}, "So'zingizda buyruq ko'rinmadi."},
		{"claims done in Russian", []string{refusedMute}, []ai.Message{moderateCall("a"), text("Готово, он замучен.")}, "So'zingizda buyruq ko'rinmadi."},
		{"says the refusal", []string{refusedMute}, []ai.Message{moderateCall("a"), text("So'zingizda buyruq ko'rinmadi.")}, "So'zingizda buyruq ko'rinmadi."},
		{"own reason, no claim", []string{refusedMute}, []ai.Message{moderateCall("a"), text("Avval mut qil deb yozing.")}, "Avval mut qil deb yozing."},
		{"refused, then done", []string{refusedMute, doneMute}, []ai.Message{moderateCall("a"), moderateCall("b"), text("Bajarildi: Bek mute.")}, "Bajarildi: Bek mute."},
		{"no said", []string{`{"ok":false,"error":{"code":"timeout","message":"context deadline exceeded"}}`}, []ai.Message{moderateCall("a"), text("Done ✅")}, notDoneLine},
		{"no moderation", nil, []ai.Message{text("Bajarildi!")}, "Bajarildi!"},
	}
	for _, c := range cases {
		if got := replyWith(t, c.results, c.replies...); got != c.want {
			t.Errorf("%s: answer = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestClaimsDone(t *testing.T) {
	for reply, want := range map[string]bool{
		"Bajarildi":                           true,
		"Hasan 1 daqiqaga mute qilib qo'ydim": true,
		"Бажарилди, ўчирилди":                 true,
		"Сделано":                             true,
		"done!":                               true,
		"Bajarilmadi, hech narsa qilinmadi":   false,
		"abandoned":                           false,
		"Kimni? Xabariga reply qiling":        false,
	} {
		if got := claimsDone(reply); got != want {
			t.Errorf("claimsDone(%q) = %t, want %t", reply, got, want)
		}
	}
}
