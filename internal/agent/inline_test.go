package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"telegram-ai-bot/internal/ai"
)

func inlineRequest(text string) Request {
	req := request(1, text)
	req.Inline = true
	return req
}

// An inline question goes to the chat model alone, without the router,
// tools or history, told what inline mode is, and is saved nowhere.
func TestInlineQuestionIsAnsweredByTheChatModelAlone(t *testing.T) {
	f, router, chat := routedFixture("TOOLS", text("Toshkent — O'zbekiston poytaxti."), text("main"))
	f.store.history[testScope] = []ai.Message{{Role: ai.RoleUser, Content: "earlier"}}

	answer, err := f.agent.Reply(context.Background(), inlineRequest("O'zbekiston poytaxti?"))

	if err != nil || answer.Text != "Toshkent — O'zbekiston poytaxti." || answer.Route != RouteInline {
		t.Fatalf("answer = %+v, %v", answer, err)
	}
	if len(router.requests) != 0 || len(f.model.requests) != 0 || len(f.tools.executions) != 0 {
		t.Errorf("router asked %d times, main model %d", len(router.requests), len(f.model.requests))
	}
	sent := chat.requests[0]
	if len(sent.Tools) != 0 || len(sent.Messages) != 2 || !strings.Contains(sent.Messages[0].Content, "inline mode") || !strings.Contains(sent.Messages[0].Content, "be funny") {
		t.Errorf("chat request = %+v", sent)
	}
	if f.store.saves != 0 {
		t.Errorf("the inline turn was saved %d times", f.store.saves)
	}
}

// Without a router, the main model answers, still without tools.
func TestInlineQuestionWithoutAChatModel(t *testing.T) {
	f := newFixture(text("Salom!"))

	answer, err := f.agent.Reply(context.Background(), inlineRequest("salom!"))

	if err != nil || answer.Text != "Salom!" || len(f.model.requests) != 1 || len(f.model.requests[0].Tools) != 0 {
		t.Errorf("answer = %+v, %v; requests = %+v", answer, err, f.model.requests)
	}
}

// A question that needs a tool cannot be answered inline.
func TestInlineQuestionNeedingAToolFails(t *testing.T) {
	for name, says := range map[string]string{"marker": "[tools]", "tool text": "[get_weather city:Toshkent]"} {
		f, _, _ := routedFixture("CHAT", text(says))
		if _, err := f.agent.Reply(context.Background(), inlineRequest("Toshkentda havo qanday?")); !errors.Is(err, ErrNeedsTools) {
			t.Errorf("%s: err = %v, want ErrNeedsTools", name, err)
		}
	}
}
