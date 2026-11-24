package agent

import (
	"context"
	"testing"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/uzscript"
)

// weatherTools answers every call with the next of results.
type weatherTools struct{ results []string }

func (w *weatherTools) Definitions(context.Context, int64) []ai.ToolDefinition {
	return []ai.ToolDefinition{{Type: "function", Function: ai.FunctionDefinition{Name: weatherTool}}}
}

func (w *weatherTools) Execute(context.Context, int64, string, string) string {
	result := w.results[0]
	w.results = w.results[1:]
	return result
}

func weatherCall(id string) ai.Message {
	return ai.Message{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: id, Type: "function", Function: ai.FunctionCall{Name: weatherTool, Arguments: `{"city":"Toshkent"}`}}}}
}

const (
	weatherDown = `{"ok":false,"error":{"code":"unavailable","message":"the weather service did not answer"}}`
	weatherUp   = `{"ok":true,"result":{"city":"Toshkent","days":[{"max":7,"min":-1}]}}`
)

// A forecast told after every get_weather call failed is made up, so a line
// saying the weather could not be got, in the answer's language, is sent
// instead; an answer without numbers, or after a call that worked, stays.
func TestFailedWeatherIsNeverMadeUp(t *testing.T) {
	cases := []struct {
		name    string
		results []string
		replies []ai.Message
		want    string
	}{
		{"made up in English", []string{weatherDown}, []ai.Message{weatherCall("a"), text("Tomorrow in Tashkent it will be around 18°C with partly cloudy skies.")}, weatherLineEn},
		{"made up in Uzbek", []string{weatherDown}, []ai.Message{weatherCall("a"), text("Ertaga Toshkentda +12 daraja, quyoshli bo'ladi.")}, weatherLineUz},
		{"made up in Russian", []string{weatherDown}, []ai.Message{weatherCall("a"), text("Завтра в Ташкенте около 10 градусов, ветер 5 м/с.")}, weatherLineRu},
		{"made up in Uzbek Cyrillic", []string{weatherDown}, []ai.Message{weatherCall("a"), text("Эртага Тошкентда 15° бўлади, ёмғир эҳтимоли 40%.")}, uzscript.Cyrillic(weatherLineUz)},
		{"says it failed", []string{weatherDown}, []ai.Message{weatherCall("a"), text("Ob-havo xizmati hozir javob bermayapti.")}, "Ob-havo xizmati hozir javob bermayapti."},
		{"failed, then worked", []string{weatherDown, weatherUp}, []ai.Message{weatherCall("a"), weatherCall("b"), text("Ertaga 7° gacha, kechasi -1°.")}, "Ertaga 7° gacha, kechasi -1°."},
		{"worked", []string{weatherUp}, []ai.Message{weatherCall("a"), text("Ertaga 7° gacha.")}, "Ertaga 7° gacha."},
	}
	for _, c := range cases {
		model := &scriptedModel{replies: c.replies}
		a := New(Options{Store: newMemoryStore(), Model: model, Tools: &weatherTools{results: c.results}, MaxHistory: 20, MaxToolCalls: 3})
		answer, err := a.Reply(context.Background(), request(1, "ertaga havo qanaqa"))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if answer.Text != c.want {
			t.Errorf("%s: %q, want %q", c.name, answer.Text, c.want)
		}
	}
}
