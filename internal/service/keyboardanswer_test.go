package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
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

// The keyboard asks for what its buttons need before answering, and none
// of it takes from the day's limit or asks the model: the time at once,
// the rate, the weather and the die with buttons to choose from, the meme
// with the reply box open.
func TestKeyboardButtonsAskForTheirValue(t *testing.T) {
	f := newFixture()
	quota := withPrivateLimit(f, 0)
	withToolRunner(f)
	f.opts.Now = func() time.Time { return time.Date(2026, 10, 29, 16, 5, 0, 0, time.UTC) }

	sent := f.run(
		textFrom(userID, keyboardTime.en),
		textFrom(userID, keyboardRate.uz),
		textFrom(userID, keyboardWeather.ru),
		textFrom(userID, keyboardDie.uz),
		textFrom(userID, keyboardMeme.uz),
		textFrom(userID, keyboardOldDollar.uz),
	)

	if len(sent) != 6 || sent[0].text != "🕐 It is 21:05 in Tashkent, Thursday, 29.10.2026." {
		t.Fatalf("sent = %+v", sent)
	}
	if sent[1].text != textWhichCurrency.uz || len(sent[1].buttons) != 2 || sent[1].buttons[0][0].Data != "kb:rate:USD" {
		t.Errorf("rate = %+v", sent[1])
	}
	if sent[2].text != textWhichCity.ru || sent[2].buttons[len(sent[2].buttons)-1][0].Data != "kb:city:" {
		t.Errorf("weather = %+v", sent[2])
	}
	if sent[3].text != textWhichGame.uz || sent[3].buttons[0][1].Data != "kb:dice:🎯" {
		t.Errorf("die = %+v", sent[3])
	}
	if sent[4].text != askedRequests["meme"].question.uz || sent[4].ask == "" {
		t.Errorf("meme = %+v", sent[4])
	}
	if sent[5].text != textWhichCurrency.uz {
		t.Errorf("the old dollar button = %+v", sent[5])
	}
	if asked, _ := quota.recorded(); len(asked) != 0 || len(f.replier.recorded()) != 0 {
		t.Errorf("limit asked %d times, model %d", len(asked), len(f.replier.recorded()))
	}
}

// menuPress is userID pressing a menu's choice under message 70.
func menuPress(id int, data string) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, CallbackQuery: &telegram.CallbackQuery{
		ID: fmt.Sprint(id), From: &telegram.User{ID: userID}, Data: data,
		Message: &telegram.Message{MessageID: 70, Chat: telegram.Chat{ID: userID, Type: telegram.ChatTypePrivate}},
	}}
}

// replyTo is userID's text replying to the bot's question.
func replyTo(question, text string) telegram.Update {
	update := textFrom(userID, text)
	update.Message.ReplyToMessage = &telegram.Message{MessageID: 71, From: &telegram.User{ID: botID, IsBot: true}, Text: question}
	return update
}

// A choice is answered, and a rate asks for an amount to convert; the
// answers to the questions are answered in code too, and the meme's topic
// goes to the model.
func TestMenusAnswerTheirChoicesAndReplies(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	buttons := &fakeButtons{}
	f.opts.Buttons = buttons

	sent := f.run(
		menuPress(1, "kb:rate:EUR"),
		replyTo("💵 1 USD = 12 750,50 so'm (Markaziy bank, 29.10.2026)\n\n"+textAmountPrompt.uz, "1 500"),
		menuPress(2, "kb:city:"),
		replyTo(textCityPrompt.uz, "Termiz"),
		menuPress(3, "kb:dice:🎯"),
		replyTo(askedRequests["meme"].question.uz, "dushanba ertalab"),
	)

	// Presses are handled at once and messages in turn, so the order is
	// not theirs.
	asked := runner.recorded()
	slices.Sort(asked)
	want := []string{`currency_rate {"amount":1500,"code":"USD"}`, `currency_rate {"code":"EUR"}`, `get_weather {"city":"Termiz"}`, `send_dice {"emoji":"🎯"}`}
	if !slices.Equal(asked, want) {
		t.Errorf("tools asked = %q\nwant %q", asked, want)
	}
	if len(sent) != 5 {
		t.Fatalf("sent = %+v", sent)
	}
	find := func(prefix string) sentMessage {
		for _, m := range sent {
			if strings.HasPrefix(m.text, prefix) {
				return m
			}
		}
		t.Errorf("nothing sent starts %q: %+v", prefix, sent)
		return sentMessage{}
	}
	if rate := find("💵 1 USD"); !strings.Contains(rate.text, textAmountPrompt.uz) || rate.ask != "100" {
		t.Errorf("rate = %+v", rate)
	}
	// An answer that ends a question puts the keyboard back, which the
	// reply box hid.
	if converted := find("💵 1500 USD = 19 125 750,00 so'm (Markaziy bank, 29.10.2026)"); converted.keyboard == nil {
		t.Errorf("the conversion came without the keyboard")
	}
	memeAnswered := false
	for _, m := range sent {
		if strings.Contains(m.text, fmt.Sprintf(askedRequests["meme"].request.uz, "dushanba ertalab")) {
			memeAnswered = m.keyboard != nil
		}
	}
	if !memeAnswered {
		t.Errorf("the meme's answer came without the keyboard")
	}
	if prompt := find(textCityPrompt.uz); prompt.ask == "" {
		t.Errorf("city prompt = %+v", prompt)
	}
	find("🌤 Toshkent: hozir")
	if calls := f.replier.recorded(); len(calls) != 1 || !strings.Contains(calls[0].text, fmt.Sprintf(askedRequests["meme"].request.uz, "dushanba ertalab")) {
		t.Errorf("model asked %+v; want the meme's topic", calls)
	}
	if len(buttons.answers) != 3 {
		t.Errorf("presses answered %d times, want 3", len(buttons.answers))
	}
}

// A tool that fails is said to, and an amount that is no number is the
// model's.
func TestMenusSayFailures(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	delete(runner.answers, "get_weather")

	sent := f.run(replyTo(textCityPrompt.uz, "Atlantida"), replyTo("💵 1 USD = 1\n\n"+textAmountPrompt.uz, "ko'p"))

	if len(sent) != 2 || sent[0].text != textCityUnknown.uz || len(f.replier.recorded()) != 1 {
		t.Errorf("sent = %+v, model %d", sent, len(f.replier.recorded()))
	}
}

// In a group the buttons' words are words: the model answers them.
func TestKeyboardWordsInAGroupAreTheModels(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)

	f.run(inGroup(groupMessage("Mira, " + keyboardRate.uz)))

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

// Mobile Legends has its own menu: the meta is asked of the model at once,
// and the rest ask for the hero or the ID first, the answer going to the
// model with the request.
func TestMLBBMenu(t *testing.T) {
	f := newFixture()
	withToolRunner(f)
	f.opts.Buttons = &fakeButtons{}

	sent := f.run(textFrom(userID, keyboardMLBB.uz))
	if len(sent) != 1 || sent[0].text != textMLBBMenu.uz || len(sent[0].buttons) != 4 || sent[0].buttons[0][0].Data != "kb:ml:meta" {
		t.Fatalf("menu = %+v", sent)
	}

	sent = f.run(menuPress(1, "kb:ml:build"))
	if len(sent) != 2 || sent[1].text != askedRequests["build"].question.uz || sent[1].ask == "" {
		t.Fatalf("build's question = %+v", sent)
	}
	f.run(replyTo(askedRequests["build"].question.uz, "Ling"))
	f.run(menuPress(2, "kb:ml:meta"))

	calls := f.replier.recorded()
	if len(calls) != 2 || !strings.Contains(calls[0].text, "MLBB: Ling uchun eng yaxshi build") || !strings.Contains(calls[1].text, textMLBBMeta.uz) {
		t.Errorf("model asked %+v", calls)
	}
}

// A button is answered in the language of its label, though a few words
// such as "😂 Make a meme" were taken for Uzbek; and the meme, a picture,
// brings the keyboard back with it.
func TestMenusSpeakThePressedLabelsLanguage(t *testing.T) {
	f := newFixture()
	withToolRunner(f)
	f.opts.MemesEnabled = true
	f.replier.draws = 1

	sent := f.run(textFrom(userID, keyboardMeme.en), textFrom(userID, keyboardMLBB.en))
	if len(sent) != 2 || sent[0].text != askedRequests["meme"].question.en || sent[1].text != textMLBBMenu.uz {
		t.Fatalf("sent = %+v; want the English question, and the MLBB menu in the app's language", sent)
	}

	sent = f.run(replyTo(askedRequests["meme"].question.en, "random"))
	if last := sent[len(sent)-1]; !last.photo || last.keyboard == nil {
		t.Errorf("meme = %+v; want a picture with the keyboard", last)
	}
}
