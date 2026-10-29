package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeToolRunner answers each tool with a fixed envelope and records what
// it was asked.
type fakeToolRunner struct {
	mu      sync.Mutex
	answers map[string]string
	asked   []string
}

func (f *fakeToolRunner) Execute(_ context.Context, _ int64, name, args string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, name+" "+args)
	if answer, ok := f.answers[name]; ok {
		return answer
	}
	return `{"ok":false,"error":{"code":"unknown_tool","message":"no"}}`
}

func (f *fakeToolRunner) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

func withToolRunner(f *fixture) *fakeToolRunner {
	runner := &fakeToolRunner{answers: map[string]string{
		"currency_rate": `{"ok":true,"result":{"code":"USD","rate_uzs":12750.5,"diff":-12.3,"rate_date":"29.10.2026"}}`,
		"get_weather":   `{"ok":true,"result":{"place":"Toshkent","now":{"temp_c":14.6,"conditions":"ochiq","wind_kmh":9},"daily":[{"min_c":8,"max_c":18.2,"rain_chance_pct":10}]}}`,
		"send_dice":     `{"ok":true,"result":{"emoji":"🎲","value":4}}`,
	}}
	f.opts.Tools = runner
	return runner
}

// In production the keyboard's buttons went unanswered past the day's
// limit, though the dollar, the weather, the time and the die need no
// model: they are answered in code, with the limit spent, and the model is
// never asked.
func TestKeyboardButtonsTakeNothingFromTheLimit(t *testing.T) {
	f := newFixture()
	quota := withPrivateLimit(f, 0)
	runner := withToolRunner(f)
	f.opts.Now = func() time.Time { return time.Date(2026, 10, 29, 16, 5, 0, 0, time.UTC) }

	sent := f.run(
		textFrom(userID, keyboardDollar.uz),
		textFrom(userID, keyboardWeather.ru),
		textFrom(userID, keyboardTime.en),
		textFrom(userID, keyboardDie.uz),
	)

	if len(sent) != 3 {
		t.Fatalf("sent = %+v; want the dollar, the weather and the time, the die being its own answer", sent)
	}
	if !strings.Contains(sent[0].text, "12 750,50 so'm") || !strings.Contains(sent[0].text, "📉") {
		t.Errorf("dollar = %q", sent[0].text)
	}
	if !strings.Contains(sent[1].text, "Toshkent: сейчас +15°, ochiq") || !strings.Contains(sent[1].text, "+8°…+18°") {
		t.Errorf("weather = %q", sent[1].text)
	}
	if sent[2].text != "🕐 It is 21:05 in Tashkent, Thursday, 29.10.2026." {
		t.Errorf("time = %q", sent[2].text)
	}
	if asked := runner.recorded(); len(asked) != 3 || !strings.HasPrefix(asked[2], "send_dice") {
		t.Errorf("tools asked = %q", asked)
	}
	if asked, _ := quota.recorded(); len(asked) != 0 {
		t.Errorf("the day's limit was asked %d times", len(asked))
	}
	if calls := f.replier.recorded(); len(calls) != 0 {
		t.Errorf("the model was asked %d times", len(calls))
	}
}

// A meme's words are the model's, so its button is a turn like any other,
// and a tool that fails is said to.
func TestMemeButtonIsTheModelsAndFailuresAreSaid(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	delete(runner.answers, "currency_rate")

	sent := f.run(textFrom(userID, keyboardMeme.uz), textFrom(userID, keyboardDollar.uz))

	if len(f.replier.recorded()) != 1 {
		t.Errorf("model asked %d times, want once for the meme", len(f.replier.recorded()))
	}
	if len(sent) != 2 || sent[1].text != textError.uz {
		t.Errorf("sent = %+v; want the meme's turn, then the error", sent)
	}
}

// In a group the buttons' words are words: the model answers them.
func TestKeyboardWordsInAGroupAreTheModels(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)

	f.run(inGroup(groupMessage("Mira, " + keyboardDollar.uz)))

	if len(runner.recorded()) != 0 || len(f.replier.recorded()) != 1 {
		t.Errorf("tools %q, model %d; want the model alone", runner.recorded(), len(f.replier.recorded()))
	}
}

// Tools take nothing from the day's limit: a turn that used one is given
// back, and past the limit a question in words is still answered when a
// tool answers it, while talk gets the limit's notice.
func TestToolsTakeNothingFromTheLimit(t *testing.T) {
	f := newFixture()
	quota := withPrivateLimit(f, 1)
	f.replier.toolsUsed = 1
	sent := f.run(textFrom(userID, "dollar qancha"), textFrom(userID, "evro qancha"))
	if _, released := quota.recorded(); len(sent) != 2 || len(released) != 2 {
		t.Fatalf("sent = %+v, released %v; want both answered and given back", sent, released)
	}

	f.replier.toolsUsed = 0
	f.run(textFrom(userID, "salom")) // the day's one answer of talk
	sent = f.run(textFrom(userID, "nima gap"))
	if len(sent) != 4 || !strings.HasPrefix(sent[3].text, "Bugungi limit tugadi") {
		t.Errorf("sent = %+v; want the notice past the limit", sent)
	}

	f.replier.toolsUsed = 1
	sent = f.run(textFrom(userID, "rubl qancha"))
	if len(sent) != 5 || sent[4].text != "echo: rubl qancha" {
		t.Errorf("sent = %+v; want the tool's answer past the limit", sent)
	}
	if req := f.replier.recorded(); !req[len(req)-1].req.ToolsOnly {
		t.Errorf("the turn past the limit was not for tools only")
	}
}
