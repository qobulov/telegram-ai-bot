package service

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	mlbbMetaJSON = `{"ok":true,"result":{"rank":"all","sorted_by":"win_rate","measured":"2026-10-28","heroes":[` +
		`{"hero":"Ling","win_rate":"55.2%","pick_rate":"1.2%","ban_rate":"30.1%","tier":"S"},` +
		`{"hero":"Tigreal","win_rate":"54.0%","pick_rate":"0.9%","ban_rate":"2.0%"}]}}`
	mlbbHeroJSON = `{"ok":true,"result":{"hero":"Ling","win_rate":"52.1%","pick_rate":"1.0%","ban_rate":"20.3%","tier":"A",` +
		`"title":"Lone Swordsman","roles":["Assassin"],"lanes":["Jungle"],"difficulty":"8/10",` +
		`"skills":[{"name":"Cloud Walker"},{"name":"Finch Poise"}],` +
		`"strong_against":[{"hero":"Eudora","win_rate_change":"+2.0%"}],` +
		`"countered_by":[{"hero":"Khufra","win_rate_change":"-3.2%"},{"hero":"Saber","win_rate_change":"-2.1%"}],` +
		`"good_partners":["Tigreal"]}}`
	mlbbBuildJSON = `{"ok":true,"result":{"hero":"Ling","lane":"jungle","builds":[{"spell":"Retribution","emblem":"Assassin",` +
		`"core_items":["Blade of Heptaseas","Corrosion Scythe"],"win_rate":"56.0%","pick_rate":"12.3%"}],"finish_with":["Tough Boots"]}}`
	mlbbDraftJSON = `{"ok":true,"result":{"enemies":["Ling","Tigreal"],"picks":[` +
		`{"hero":"Khufra","score":"+4.1%","why":["counters Ling (+3.2%)","counters Tigreal (+0.9%)","weak against Ling (-0.1%)"]}]}}`
	mlbbWinrateJSON = `{"ok":true,"result":{"matches":500,"win_rate":"48.0%","target_win_rate":"60.0%","wins_in_a_row":150}}`
	mlbbPlayerJSON  = `{"ok":true,"result":{"player_id":"1407157025","zone":"15832","nickname":"Shadow","country":"Uzbekistan"}}`
)

// mlbbReply is the answer to the MLBB menu's question for key, written
// value, with the mlbb tool answering answer; it fails the test should the
// model be asked.
func mlbbReply(t *testing.T, key, answer, value string) (sentMessage, []string) {
	t.Helper()
	f := newFixture()
	runner := withToolRunner(f)
	if answer != "" {
		runner.answers["mlbb"] = answer
	}
	sent := f.run(replyTo(askedRequests[key].question.en, value))
	if len(sent) == 0 {
		t.Fatalf("%s: nothing sent", key)
	}
	if calls := f.replier.recorded(); len(calls) != 0 {
		t.Errorf("%s: the model was asked %+v", key, calls)
	}
	last := sent[len(sent)-1]
	if last.keyboard == nil {
		t.Errorf("%s: answer %+v came without the keyboard", key, last)
	}
	return last, runner.recorded()
}

// The meta is answered when pressed, from the tool, without the model.
func TestMetaIsAnsweredInCode(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	runner.answers["mlbb"] = mlbbMetaJSON
	f.opts.Buttons = &fakeButtons{}

	sent := f.run(menuPress(1, "kb:ml:meta"))

	want := "**🔥 MLBB meta: win rate bo'yicha top 2 (2026-10-28)**\n" +
		"1. **Ling** — WR 55.2% · pik 1.2% · ban 30.1% · tier S\n" +
		"2. **Tigreal** — WR 54.0% · pik 0.9% · ban 2.0%"
	if last := sent[len(sent)-1]; last.text != want || last.keyboard == nil {
		t.Errorf("meta = %+v\nwant %q", last, want)
	}
	if asked := runner.recorded(); len(asked) != 1 || asked[0] != `mlbb {"action":"meta"}` {
		t.Errorf("tools asked = %q", asked)
	}
	if len(f.replier.recorded()) != 0 {
		t.Errorf("the model was asked")
	}

	delete(runner.answers, "mlbb")
	if sent := f.run(menuPress(2, "kb:ml:meta")); sent[len(sent)-1].text != textError.uz {
		t.Errorf("a failed meta = %+v", sent[len(sent)-1])
	}
}

// A hero is its profile, short, under its portrait.
func TestHeroIsAnsweredInCode(t *testing.T) {
	answer, asked := mlbbReply(t, "hero", mlbbHeroJSON, "ling")

	want := "🦸 **Ling**, Lone Swordsman\nAssassin · Jungle\nWR 52.1% · pik 1.0% · ban 20.3% · tier A\n" +
		"Qiyinlik: 8/10\nSkillar: Cloud Walker, Finch Poise\nYengadi: Eudora\nQiyin raqiblar: Khufra, Saber\nYaxshi sheriklar: Tigreal"
	if !answer.photo || answer.text != want {
		t.Errorf("hero = %+v\nwant a picture captioned %q", answer, want)
	}
	if len(asked) != 1 || asked[0] != `mlbb {"action":"hero","hero":"ling","picture":true}` {
		t.Errorf("tools asked = %q", asked)
	}

	if answer, _ := mlbbReply(t, "hero", `{"ok":false,"error":{"code":"invalid_arguments","message":"no hero"}}`, "Nobody"); answer.text != textHeroUnknown.uz {
		t.Errorf("unknown hero = %+v", answer)
	}
}

// A build is the tool's picture, with a short caption; without a picture,
// it is written out.
func TestBuildIsAnsweredInCode(t *testing.T) {
	answer, asked := mlbbReply(t, "build", mlbbBuildJSON, "Ling")
	if !answer.photo || answer.text != "🛠 **Ling** · Jungle\nWR 56.0% · buildlarning 12.3%" {
		t.Errorf("build = %+v", answer)
	}
	if len(asked) != 1 || asked[0] != `mlbb {"action":"build","hero":"Ling"}` {
		t.Errorf("tools asked = %q", asked)
	}

	in := incoming{msg: textFrom(userID, "Ling").Message}
	var envelope struct {
		Result mlbbBuildResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(mlbbBuildJSON), &envelope); err != nil {
		t.Fatal(err)
	}
	text := buildText(in, envelope.Result, false)
	for _, want := range []string{"Spell: Retribution", "Emblema: Assassin", "Asosiy itemlar: Blade of Heptaseas, Corrosion Scythe", "Oxirida: Tough Boots"} {
		if !strings.Contains(text, want) {
			t.Errorf("build without its picture = %q; want %q", text, want)
		}
	}
}

// A counter pick names the heroes that lower the hero's win rate, by how
// much, and those it beats.
func TestCounterIsAnsweredInCode(t *testing.T) {
	answer, asked := mlbbReply(t, "counter", mlbbHeroJSON, "Ling")

	want := "⚔️ **Ling** ga qarshi eng yaxshi piklar (uning win rate'i qanchaga tushadi):\n" +
		"- **Khufra** (-3.2%)\n- **Saber** (-2.1%)\n\nLing o'zi yengadi: Eudora"
	if answer.photo || answer.text != want {
		t.Errorf("counter = %+v\nwant %q", answer, want)
	}
	if len(asked) != 1 || asked[0] != `mlbb {"action":"hero","hero":"Ling"}` {
		t.Errorf("tools asked = %q", asked)
	}

	noCounters := `{"ok":true,"result":{"hero":"Ling","countered_by":[]}}`
	if answer, _ := mlbbReply(t, "counter", noCounters, "Ling"); answer.text != textNoCounters.uz {
		t.Errorf("no counters = %+v", answer)
	}
}

// A draft takes the enemy's heroes written with commas.
func TestDraftIsAnsweredInCode(t *testing.T) {
	answer, asked := mlbbReply(t, "draft", mlbbDraftJSON, "Ling, tigreal")

	want := "🎯 Dushman: Ling, Tigreal. Eng yaxshi piklar:\n1. **Khufra** +4.1% — Ling ga qarshi (+3.2%); Tigreal ga qarshi (+0.9%)"
	if answer.text != want {
		t.Errorf("draft = %q\nwant %q", answer.text, want)
	}
	if len(asked) != 1 || asked[0] != `mlbb {"action":"draft","enemies":["Ling","tigreal"]}` {
		t.Errorf("tools asked = %q", asked)
	}

	if answer, asked := mlbbReply(t, "draft", "", "a, b, c, d, e, f"); answer.text != textDraftHow.uz || len(asked) != 0 {
		t.Errorf("six enemies = %+v, tools %q", answer, asked)
	}
}

// Win rate takes three numbers however they are written, and says how to
// write them when they are not.
func TestWinrateIsAnsweredInCode(t *testing.T) {
	answer, asked := mlbbReply(t, "winrate", mlbbWinrateJSON, "500 48% 60%")
	if answer.text != "📈 500 o'yin, 48.0% → 60.0%: ketma-ket **150** g'alaba kerak." {
		t.Errorf("winrate = %q", answer.text)
	}
	if len(asked) != 1 || asked[0] != `mlbb {"action":"winrate","matches":500,"target_win_rate":60,"win_rate":48}` {
		t.Errorf("tools asked = %q", asked)
	}

	reached := `{"ok":true,"result":{"matches":500,"win_rate":"61.0%","target_win_rate":"60.0%","wins_in_a_row":0}}`
	if answer, _ := mlbbReply(t, "winrate", reached, "500 61 60"); answer.text != "📈 Win rate'ingiz 61.0%, kerakli 60.0% ga allaqachon yetgan." {
		t.Errorf("reached = %q", answer.text)
	}
	if answer, asked := mlbbReply(t, "winrate", "", "ko'p o'ynadim"); answer.text != textWinrateHow.uz || len(asked) != 0 {
		t.Errorf("no numbers = %+v, tools %q", answer, asked)
	}
}

func TestParseWinrate(t *testing.T) {
	for text, want := range map[string][3]float64{
		"500 48 60":        {500, 48, 60},
		"500, 48%, 60%":    {500, 48, 60},
		"500 48,5 60":      {500, 48.5, 60},
		"500,48,60":        {500, 48, 60},
		"1200 51.3% 55.5%": {1200, 51.3, 55.5},
	} {
		matches, winRate, target, ok := parseWinrate(text)
		if !ok || [3]float64{float64(matches), winRate, target} != want {
			t.Errorf("parseWinrate(%q) = %d %v %v %v; want %v", text, matches, winRate, target, ok, want)
		}
	}
	for _, text := range []string{"", "500 48", "500.5 48 60", "five 48 60", "1 2 3 4"} {
		if _, _, _, ok := parseWinrate(text); ok {
			t.Errorf("parseWinrate(%q) is ok", text)
		}
	}
}

// A player is found by the ID and the zone, as the game shows them or
// apart; without the zone, it is asked for.
func TestPlayerIsAnsweredInCode(t *testing.T) {
	for _, value := range []string{"1407157025 (15832)", "1407157025 15832"} {
		answer, asked := mlbbReply(t, "player", mlbbPlayerJSON, value)
		if answer.text != "🆔 1407157025 (15832): **Shadow**, Uzbekistan" {
			t.Errorf("%q: player = %q", value, answer.text)
		}
		if len(asked) != 1 || asked[0] != `mlbb {"action":"player","player_id":"1407157025","zone":"15832"}` {
			t.Errorf("%q: tools asked = %q", value, asked)
		}
	}
	if answer, asked := mlbbReply(t, "player", "", "1407157025"); answer.text != textPlayerZone.uz || len(asked) != 0 {
		t.Errorf("no zone = %+v, tools %q", answer, asked)
	}
	if answer, _ := mlbbReply(t, "player", `{"ok":false,"error":{"code":"execution_failed","message":"no account"}}`, "1407157025 1"); answer.text != textPlayerNotFound.uz {
		t.Errorf("not found = %+v", answer)
	}
}
