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
	"telegram-ai-bot/internal/tools"
)

// fakeToolRunner answers each tool with a fixed envelope and records what
// it was asked.
type fakeToolRunner struct {
	mu      sync.Mutex
	answers map[string]string
	asked   []string
}

func (f *fakeToolRunner) Execute(ctx context.Context, _ int64, name, args string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, name+" "+args)
	// As the tools that make pictures or find songs put them in the sink.
	if sink, ok := tools.SinkFrom(ctx); ok {
		switch {
		case name == "mlbb" && strings.Contains(args, `"skin":`), name == "cat_picture", name == "qr_code", name == "generate_image":
			sink.Add(tools.Drawing{Data: []byte("JPEG"), MimeType: "image/jpeg"})
		case name == "music_search":
			sink.AddAudio(tools.Audio{Data: []byte("MP3"), Title: "Yurak", Performer: "Shohruhxon"})
		}
	}
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
		textFrom(userID, keyboardOldDollar.uz),
	)

	if len(sent) != 5 || sent[0].text != "🕐 It is 21:05 in Tashkent, Thursday, 29.10.2026." {
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
	if sent[4].text != textWhichCurrency.uz {
		t.Errorf("the old dollar button = %+v", sent[4])
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
		replyTo(askedRequests["hero"].question.uz, "Ling"),
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
		if strings.Contains(m.text, fmt.Sprintf(askedRequests["hero"].request.uz, "Ling")) {
			memeAnswered = m.keyboard != nil
		}
	}
	if !memeAnswered {
		t.Errorf("the hero's answer came without the keyboard")
	}
	if prompt := find(textCityPrompt.uz); prompt.ask == "" {
		t.Errorf("city prompt = %+v", prompt)
	}
	find("🌤 Toshkent: hozir")
	if calls := f.replier.recorded(); len(calls) != 1 || !strings.Contains(calls[0].text, fmt.Sprintf(askedRequests["hero"].request.uz, "Ling")) {
		t.Errorf("model asked %+v; want the hero's request", calls)
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
// such as "🎲 Roll a die" may be taken for Uzbek; and a picture answering
// a question brings the keyboard back with it.
func TestMenusSpeakThePressedLabelsLanguage(t *testing.T) {
	f := newFixture()
	withToolRunner(f)
	f.replier.draws = 1

	sent := f.run(textFrom(userID, keyboardDie.en), textFrom(userID, keyboardMLBB.en))
	if len(sent) != 2 || sent[0].text != textWhichGame.en || sent[1].text != textMLBBMenu.uz {
		t.Fatalf("sent = %+v; want the English question, and the MLBB menu in the app's language", sent)
	}

	sent = f.run(replyTo(askedRequests["build"].question.en, "Alpha"))
	if last := sent[len(sent)-1]; !last.photo || last.keyboard == nil {
		t.Errorf("answer = %+v; want a picture with the keyboard", last)
	}
}

// A hero's skins are buttons, and the one pressed comes as its picture,
// named, without the model: the list had been the model's text.
func TestSkinsAreChosenAndShown(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	runner.answers["mlbb"] = `{"ok":true,"result":{"hero":"Kagura","skins":[{"name":"Onmyouji Master","tier":"Epic","price":"599 olmos"},{"name":"Cherry Witch","tier":"Special"},{"name":"Flower Season"}]}}`
	f.opts.Buttons = &fakeButtons{}

	sent := f.run(replyTo(skinsQuestion.question.uz, "kagura"))
	if len(sent) != 1 || sent[0].text != "👕 Kagura: qaysi skin?" || len(sent[0].buttons) != 2 || sent[0].buttons[0][1].Data != "kb:skin:Kagura:1" {
		t.Fatalf("choices = %+v", sent)
	}

	sent = f.run(menuPress(1, "kb:skin:Kagura:1"))
	if last := sent[len(sent)-1]; !last.photo || last.text != "Kagura — Cherry Witch (Special)" || last.keyboard == nil {
		t.Errorf("skin = %+v; want its picture, named, with the keyboard", last)
	}
	if asked := runner.recorded(); !strings.Contains(asked[len(asked)-1], `"skin":"Cherry Witch"`) {
		t.Errorf("tools asked = %q", asked)
	}
	if len(f.replier.recorded()) != 0 {
		t.Errorf("the model was asked")
	}

	delete(runner.answers, "mlbb")
	sent = f.run(replyTo(skinsQuestion.question.uz, "Nobody"))
	if last := sent[len(sent)-1]; last.text != textHeroUnknown.uz {
		t.Errorf("unknown hero = %+v", last)
	}
}

// «📋 Ko'proq» lists the sections, and the menus moved through change in
// place, each with a way back: a section its items, an item with choices
// its choices. An item without a value is answered at once, a choice
// too, and an item with a question asks it, its answer answered in code
// (see moreanswers_test.go); a press that fetches says so at once.
func TestMoreMenu(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	buttons := &fakeButtons{}
	f.opts.Buttons = buttons

	sent := f.run(textFrom(userID, keyboardMore.uz))
	if len(sent) != 1 || sent[0].text != textMoreMenu.uz || len(sent[0].buttons) != 4 || sent[0].buttons[0][0].Data != "kb:more:faith" {
		t.Fatalf("sections = %+v", sent)
	}
	f.run(menuPress(1, "kb:more:sport"), menuPress(2, "kb:item:table"), menuPress(3, "kb:more:"))
	edits, menus := buttons.recordedMenus()
	if len(edits) != 3 || edits[0] != "⚽ Sport" || edits[2] != textMoreMenu.uz {
		t.Fatalf("edits = %q", edits)
	}
	if back := menus[0][len(menus[0])-1][0]; back.Data != "kb:more:" || back.Text != textBack.uz {
		t.Errorf("sport's way back = %+v", back)
	}
	if leagues := menus[1]; leagues[0][1].Data != "kb:pick:table:1" || leagues[len(leagues)-1][0].Data != "kb:more:sport" {
		t.Errorf("leagues = %+v", leagues)
	}
	if sent := f.sender.messages(); len(sent) != 1 {
		t.Errorf("moving through the menus sent %d messages, want none", len(sent)-1)
	}

	f.run(menuPress(4, "kb:pick:table:1"))
	f.run(menuPress(5, "kb:item:cat"))
	sent = f.run(menuPress(6, "kb:item:movie"))
	if last := sent[len(sent)-1]; last.text != askedRequests["movie"].question.uz || last.ask == "" {
		t.Fatalf("film's question = %+v", last)
	}
	f.run(replyTo(askedRequests["movie"].question.uz, "Interstellar"))

	joined := strings.Join(runner.recorded(), "|")
	for _, want := range []string{`football {"action":"table","competition":"La Liga"}`, "cat_picture {}", `movies {"action":"search","lang":"ru","query":"Interstellar"}`} {
		if !strings.Contains(joined, want) {
			t.Errorf("tools asked %q; want %q among them", runner.recorded(), want)
		}
	}
	if calls := f.replier.recorded(); len(calls) != 0 {
		t.Errorf("the model was asked %+v", calls)
	}
	working := 0
	for _, a := range buttons.answers {
		if a.text == textWorking.uz {
			working++
		}
	}
	if working != 2 {
		t.Errorf("presses answered %+v; want the league and the cat, not the film's question, to say it is working", buttons.answers)
	}
}

// No two questions of the menus are the same, or an answer would go to
// the wrong request.
func TestMenuQuestionsAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for key, request := range askedRequests {
		for _, q := range []string{request.question.uz, request.question.ru, request.question.en} {
			if other, ok := seen[q]; ok || q == "" {
				t.Errorf("%s and %s ask %q", key, other, q)
			}
			seen[q] = key
		}
	}
	// An answer is known by the question it replies to holding it, so no
	// question may hold another.
	for q, key := range seen {
		for other, otherKey := range seen {
			if q != other && strings.Contains(q, other) {
				t.Errorf("%s's %q holds %s's %q", key, q, otherKey, other)
			}
		}
	}
}

// The model sees the tag the group shows by a sender's name, a member's
// tag or an admin's title, in «», never with brackets that could fake a
// mark.
func TestSendersTagIsInTheirLabel(t *testing.T) {
	f := newFixture()
	msg := groupMessage("Mira, salom")
	msg.SenderTag = "Pro [owner]"

	f.run(inGroup(msg))

	calls := f.replier.recorded()
	if len(calls) != 1 || !strings.Contains(calls[0].req.AuthorName, "«Pro owner»") || strings.Contains(calls[0].req.AuthorName, "[owner]") {
		t.Errorf("author = %q", calls[0].req.AuthorName)
	}
}

// withCodeAnswer answers key in code for one test, saying what it got.
func withCodeAnswer(t *testing.T, key string) {
	t.Helper()
	before, had := codeAnswers[key]
	codeAnswers[key] = func(_ context.Context, _ *Service, _ incoming, value string) outcome {
		return say("code " + key + ": " + value)
	}
	t.Cleanup(func() {
		if had {
			codeAnswers[key] = before
		} else {
			delete(codeAnswers, key)
		}
	})
}

// A request with a code answer never reaches the model: an item without a
// value, a choice, a reply to a question, and the meta.
func TestCodeAnswersSkipTheModel(t *testing.T) {
	for _, key := range []string{"cat", "table", "hero", "meta"} {
		withCodeAnswer(t, key)
	}
	f := newFixture()
	withToolRunner(f)
	f.opts.Buttons = &fakeButtons{}

	sent := f.run(menuPress(1, "kb:item:cat"))
	sent = append(sent, f.run(menuPress(2, "kb:pick:table:1"))...)
	sent = append(sent, f.run(menuPress(3, "kb:ml:meta"))...)
	sent = append(sent, f.run(replyTo(askedRequests["hero"].question.uz, "Ling"))...)

	var texts []string
	for _, m := range sent {
		texts = append(texts, m.text)
	}
	joined := strings.Join(texts, "|")
	for _, want := range []string{"code cat: ", "code table: La Liga", "code meta: ", "code hero: Ling"} {
		if !strings.Contains(joined, want) {
			t.Errorf("sent %q; want %q", texts, want)
		}
	}
	if calls := f.replier.recorded(); len(calls) != 0 {
		t.Errorf("the model was asked %+v", calls)
	}
}
