package service

import (
	"slices"
	"testing"

	"telegram-ai-bot/internal/ai"
)

// With the AI's credits spent, a chat is told three times, the second and
// third jokingly, and then nothing more; each lost reply is recorded.
func TestAILimitIsToldThreeTimes(t *testing.T) {
	f := newFixture()
	skips := &fakeSkips{}
	f.opts.Skips = skips
	f.replier.err = &ai.APIError{Status: 402, Message: "Your prepayment credits are depleted."}

	sent := f.run(
		inGroup(groupMessage("@MyBot salom", "@MyBot")),
		inGroup(groupMessage("@MyBot qalaysan", "@MyBot")),
		inGroup(groupMessage("@MyBot javob ber", "@MyBot")),
		inGroup(groupMessage("@MyBot hoy", "@MyBot")),
		inGroup(groupMessage("@MyBot yana", "@MyBot")),
	)

	var texts []string
	for _, m := range sent {
		texts = append(texts, m.text)
	}
	want := []string{textsAILimit[0].uz, textsAILimit[1].uz, textsAILimit[2].uz}
	if !slices.Equal(texts, want) {
		t.Errorf("sent = %q, want the three notices once each", texts)
	}
	if got := skips.reasons(); !slices.Equal(got, []string{"ai_limit", "ai_limit", "ai_limit", "ai_limit", "ai_limit"}) {
		t.Errorf("skips = %q, want every lost reply recorded", got)
	}
}
