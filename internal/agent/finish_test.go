package agent

import (
	"context"
	"reflect"
	"testing"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/tools"
)

// finishingTools ends the turn from its tool, as music_search does.
type finishingTools struct{ fakeTools }

func (f *finishingTools) Execute(ctx context.Context, callerID int64, name, args string) string {
	if sink, ok := tools.SinkFrom(ctx); ok {
		sink.Finish("Mana taqdimotingiz.")
	}
	return f.fakeTools.Execute(ctx, callerID, name, args)
}

// A tool that ends the turn saves the second call to the model, and its
// answer is saved as the bot's.
func TestAToolCanEndTheTurn(t *testing.T) {
	f := newFixture(toolCall("call_1"), text("never asked for"))
	f.agent.opts.Tools = &finishingTools{}
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: testUser}, &tools.Sink{})

	answer, err := f.agent.Reply(ctx, request(1, "taqdimot qilib ber"))

	if err != nil || answer.Text != "Mana taqdimotingiz." {
		t.Fatalf("answer = %q, err = %v", answer.Text, err)
	}
	if len(f.model.requests) != 1 {
		t.Errorf("model asked %d times, want once", len(f.model.requests))
	}
	saved := f.store.history[testScope]
	if want := []string{ai.RoleUser, ai.RoleAssistant, ai.RoleTool, ai.RoleAssistant}; !reflect.DeepEqual(roles(saved), want) || saved[3].Content != "Mana taqdimotingiz." {
		t.Errorf("saved = %+v", saved)
	}
}

// Without a turn's sink, as in the tests of other tools, nothing ends early.
func TestTurnsWithoutASinkGoOn(t *testing.T) {
	f := newFixture(toolCall("call_1"), text("2"))
	if answer, err := f.agent.Reply(context.Background(), request(1, "1+1?")); err != nil || answer.Text != "2" || len(f.model.requests) != 2 {
		t.Errorf("answer = %q, err = %v, requests = %d", answer.Text, err, len(f.model.requests))
	}
}
