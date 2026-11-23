package agent

import (
	"context"
	"testing"

	"telegram-ai-bot/internal/ai"
)

func TestWithoutTrailingLaugh(t *testing.T) {
	for text, want := range map[string]string{
		"Rejissyor kim bo'ldi? 😂": "Rejissyor kim bo'ldi?",
		"Voy 😂🤣 😂":                "Voy",
		"Ha\n😂":                   "Ha",
		"😂":                       "😂",
		"🤣🤣":                      "🤣🤣",
		"😂 boshida qoladi":        "😂 boshida qoladi",
		"Salom":                   "Salom",
		"Kulgili 😂️":              "Kulgili",
	} {
		if got := withoutTrailingLaugh(text); got != want {
			t.Errorf("withoutTrailingLaugh(%q) = %q, want %q", text, got, want)
		}
	}
}

// The bot's past answers reach the model without the laughs that ended
// them, so that it does not copy them; what is stored stays as it was.
func TestHistoryLosesTrailingLaughs(t *testing.T) {
	f := newFixture(text("Yaxshi"))
	stored := []ai.Message{user("q"), text("Rejissyor 😂"), user("q2"), text("😂")}
	f.store.history[testScope] = append([]ai.Message(nil), stored...)

	if _, err := f.agent.Reply(context.Background(), request(1, "hi")); err != nil {
		t.Fatal(err)
	}
	sent := f.model.requests[0].Messages
	if sent[2].Content != "Rejissyor" || sent[4].Content != "😂" {
		t.Errorf("sent history = %q, %q", sent[2].Content, sent[4].Content)
	}
	if got := f.store.history[testScope][1].Content; got != "Rejissyor 😂" {
		t.Errorf("stored answer = %q, want it unchanged", got)
	}
}

// Every answer loses its trailing laugh, since 2026-11-23 whatever the
// answers before it did; a laugh alone stays.
func TestLaughingHabitIsBroken(t *testing.T) {
	cases := []struct {
		name    string
		history []ai.Message
		want    string
	}{
		{"two of five laugh", []ai.Message{user("a"), text("Ha 😂"), user("b"), text("Yo'q"), user("c"), text("Voy 🤣")}, "Rejissyor yana"},
		{"one laughs", []ai.Message{user("a"), text("Ha 😂"), user("b"), text("Yo'q")}, "Rejissyor yana"},
		{"old laughs", []ai.Message{user("a"), text("Ha 😂"), user("b"), text("Ha 😂"),
			user("c"), text("1"), user("d"), text("2"), user("e"), text("3"), user("f"), text("4"), user("g"), text("5")}, "Rejissyor yana"},
	}
	for _, c := range cases {
		f := newFixture(text("Rejissyor yana 😂"))
		f.store.history[testScope] = c.history
		answer, err := f.agent.Reply(context.Background(), request(1, "hi"))
		if err != nil || answer.Text != c.want {
			t.Errorf("%s: answer = %q, %v, want %q", c.name, answer.Text, err, c.want)
		}
	}
	f := newFixture(text("😂"))
	f.store.history[testScope] = []ai.Message{user("a"), text("Ha 😂"), user("b"), text("Voy 😂")}
	if answer, _ := f.agent.Reply(context.Background(), request(1, "hi")); answer.Text != "😂" {
		t.Errorf("a laugh alone = %q, want it kept", answer.Text)
	}
}
