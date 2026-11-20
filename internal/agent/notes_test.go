package agent

import (
	"context"
	"strings"
	"testing"

	"telegram-ai-bot/internal/ai"
)

// The owner rules go only to a message an owner writes or to one with an
// owner among the recent lines or questions.
func TestOwnerNoteOnlyWhenNeeded(t *testing.T) {
	cases := []struct {
		name          string
		text, context string
		history       []ai.Message
		owners        bool
	}{
		{name: "a member's small talk", text: "Ali: salom qalaysan"},
		{name: "an owner in a group", text: "Javohir [owner]: jim bo'l", owners: true},
		{name: "an owner in the recent lines", text: "Ali: nima dedi?", context: "Javohir [owner]: Aliga javob berma", owners: true},
		{name: "an owner in private", text: "salom", context: "[This message is from your owner.]", owners: true},
		{name: "an owner's earlier question", text: "Ali: xo'p", history: []ai.Message{{Role: ai.RoleUser, Content: "Javohir [owner]: gif tasha"}, text("ok")}, owners: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(text("ok"))
			f.agent.opts.OwnerNote = "OWNER RULES"
			f.store.history[testScope] = c.history
			req := request(1, c.text)
			req.Context = c.context
			if _, err := f.agent.Reply(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			system := f.model.requests[0].Messages[0].Content
			if got := strings.Contains(system, "OWNER RULES"); got != c.owners {
				t.Errorf("owner rules sent = %t, want %t", got, c.owners)
			}
			if !strings.HasPrefix(system, "be funny") {
				t.Errorf("the persona does not come first: %q", system)
			}
		})
	}
}
