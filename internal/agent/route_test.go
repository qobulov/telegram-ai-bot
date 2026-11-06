package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"telegram-ai-bot/internal/ai"
)

// routedFixture is newFixture with a router answering routerSays and a
// chat model answering chatSays.
func routedFixture(routerSays string, chatSays ai.Message, full ...ai.Message) (fixture, *scriptedModel, *scriptedModel) {
	f := newFixture(full...)
	router := &scriptedModel{replies: []ai.Message{text(routerSays)}}
	chat := &scriptedModel{replies: []ai.Message{chatSays}}
	f.agent.opts.Router, f.agent.opts.ChatModel, f.agent.opts.Name = router, chat, "Maria"
	return f, router, chat
}

// Banter goes to the chat model, with no tools and the persona, and is
// saved like any answer; the main model is never asked.
func TestBanterIsAnsweredByTheChatModel(t *testing.T) {
	f, router, chat := routedFixture("CHAT", text("Tinchlik, o'zingizda-chi?"), text("main"))
	req := request(1, "Mari, nima gap")
	req.Group = true

	answer, err := f.agent.Reply(context.Background(), req)

	if err != nil || answer.Text != "Tinchlik, o'zingizda-chi?" || answer.Route != RouteChat {
		t.Fatalf("answer = %+v, %v", answer, err)
	}
	if len(f.model.requests) != 0 {
		t.Errorf("the main model was asked %d times", len(f.model.requests))
	}
	sent := chat.requests[0]
	if len(sent.Tools) != 0 || !strings.Contains(sent.Messages[0].Content, "be funny") || !strings.Contains(sent.Messages[0].Content, "[tools]") {
		t.Errorf("chat request = %+v", sent)
	}
	if prompt := router.requests[0].Messages[0].Content; !strings.Contains(prompt, "called Maria") || !strings.Contains(prompt, "<<<Mari, nima gap>>>") {
		t.Errorf("router prompt = %q", prompt)
	}
	if got := f.store.history[testScope]; len(got) != 2 || got[1].Content != "Tinchlik, o'zingizda-chi?" {
		t.Errorf("saved = %+v", got)
	}
}

// Anything the router does not call CHAT, or cannot answer, takes the
// full path, and so does a turn with a picture or a voice message.
func TestTheFullPathAnswersAllElse(t *testing.T) {
	for name, setup := range map[string]func(f fixture, router *scriptedModel) Request{
		"tools": func(fixture, *scriptedModel) Request { return request(1, "dollar qancha") },
		"garbled": func(f fixture, router *scriptedModel) Request {
			router.replies = []ai.Message{text("maybe")}
			return request(1, "hm")
		},
		"failed": func(f fixture, router *scriptedModel) Request {
			router.err = errors.New("503")
			return request(1, "salom")
		},
		"picture": func(fixture, *scriptedModel) Request {
			req := request(1, "<photo> bu kim")
			req.Attachments = []ai.ContentPart{{Type: "image_url"}}
			return req
		},
	} {
		routerSays := "TOOLS"
		if name != "tools" {
			routerSays = "CHAT"
		}
		f, router, chat := routedFixture(routerSays, text("chat"), text("main"))
		req := setup(f, router)

		answer, err := f.agent.Reply(context.Background(), req)

		if err != nil || answer.Text != "main" || answer.Route != RouteFull || len(chat.requests) != 0 {
			t.Errorf("%s: answer = %+v, %v; chat asked %d times", name, answer, err, len(chat.requests))
		}
		if name == "picture" && len(router.requests) != 0 {
			t.Errorf("picture: the router was asked")
		}
	}
}

// The chat model giving the turn back, writing a tool call as text or
// failing sends the turn on to the full path, marked escalated.
func TestTheChatModelCanGiveTheTurnBack(t *testing.T) {
	for name, says := range map[string]ai.Message{
		"marker":    text("[tools]"),
		"round":     text(" (Tools) "),
		"tool text": text("[music_search query:Yulduz]"),
	} {
		f, _, _ := routedFixture("CHAT", says, text("main"))
		answer, err := f.agent.Reply(context.Background(), request(1, "Yulduzning qo'shig'ini tashla"))
		if err != nil || answer.Text != "main" || answer.Route != RouteEscalated {
			t.Errorf("%s: answer = %+v, %v", name, answer, err)
		}
		if got := f.store.history[testScope]; len(got) != 2 || got[1].Content != "main" {
			t.Errorf("%s: saved = %+v, want only the full answer", name, got)
		}
	}

	f, _, chat := routedFixture("CHAT", text("unused"), text("main"))
	chat.err = errors.New("quota")
	if answer, err := f.agent.Reply(context.Background(), request(1, "salom")); err != nil || answer.Route != RouteEscalated {
		t.Errorf("failed chat model: answer = %+v, %v", answer, err)
	}
}

// The router reads the last few lines of the conversation, cut short.
func TestRouterReadsTheRecentConversation(t *testing.T) {
	history := []ai.Message{
		{Role: ai.RoleUser, Content: "birinchi"},
		{Role: ai.RoleUser, Content: "CoD MW4 haqida aytib ber"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "1"}}},
		{Role: ai.RoleTool, Content: `{"ok":true}`},
		{Role: ai.RoleAssistant, Content: "Infinity Ward " + strings.Repeat("x", 400)},
		{Role: ai.RoleUser, Content: "qachon chiqqan"},
	}
	got := recentLines(history, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 || lines[0] != "user: CoD MW4 haqida aytib ber" || !strings.HasPrefix(lines[1], "bot: Infinity Ward") || lines[2] != "user: qachon chiqqan" {
		t.Errorf("recent lines = %q", got)
	}
	if n := len([]rune(lines[1])); n > routerLineRunes+len("bot: ")+1 {
		t.Errorf("a long line was not cut: %d runes", n)
	}
	if recentLines(nil, 3) != "(none)" {
		t.Error("no history is not (none)")
	}
}

// A question about a game's heroes skips the router and takes the full
// path; words in the quote do not count, only the asker's own.
func TestGameQuestionsSkipTheRouter(t *testing.T) {
	for msg, full := range map[string]bool{
		"Ali: voyd ni uzum market versiyasi mlbb dagi kim": true,
		"Ali: Хирара легаси борми":                         true,
		"Ali: dotada kim kuchli":                           true,
		"[Your earlier message that the user is replying to.]\nMLBB build\n[End of quoted message]\n\nAli: rahmat": false,
		"Ali: superhero filmi zo'r": false,
		"[Quoted message from Husniddin that the user is replying to.]\n1586840150 (16698)\n[End of quoted message]\n\nAli: Nikiz nma, mari": true,
		"Ali: mani id 1159019237(13690) kimligini top": true,
		"Ali: 2026 (10) yil":                           false,
	} {
		f, router, _ := routedFixture("CHAT", text("chat"), text("full"))
		req := request(1, msg)
		req.Group = true

		answer, err := f.agent.Reply(context.Background(), req)

		if err != nil || (answer.Route == RouteFull) != full || (len(router.requests) == 0) != full {
			t.Errorf("%q: route %q, router asked %d times; want full %v", msg, answer.Route, len(router.requests), full)
		}
	}
}

// Asking for a place on the map skips the router and takes the full path,
// where send_location is; "where are you" stays small talk for the router.
func TestMapRequestsSkipTheRouter(t *testing.T) {
	for msg, full := range map[string]bool{
		"Ali: Xivaning lokatsiyasini tashla":    true,
		"Ali: Nukusni xaritada ko'rsat":         true,
		"Ali: где находится Бухара":             true,
		"Ali: Chust qayerda joylashgan":         true,
		"Ali: скинь локацию Самарканда":         true,
		"Ali: Mari qayerdasan":                  false,
		"Ali: bugun kayfiyat qalay, aytchi-chi": false,
	} {
		f, router, _ := routedFixture("CHAT", text("chat"), text("full"))
		req := request(1, msg)
		req.Group = true

		answer, err := f.agent.Reply(context.Background(), req)

		if err != nil || (answer.Route == RouteFull) != full || (len(router.requests) == 0) != full {
			t.Errorf("%q: route %q, router asked %d times; want full %v", msg, answer.Route, len(router.requests), full)
		}
	}
}

// A game question carries the reminder to check heroes in the request, but
// the saved question is the plain text.
func TestGameQuestionsRemindToCheckHeroes(t *testing.T) {
	f, _, _ := routedFixture("CHAT", text("chat"), text("full"))
	req := request(1, "Ali: voyd ni mlbb dagi versiyasi kim")
	req.Group = true

	if _, err := f.agent.Reply(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	sent := f.model.requests[0].Messages
	if question := sent[len(sent)-1].Content; !strings.Contains(question, heroCheckNote) {
		t.Errorf("question sent = %q", question)
	}
	if saved := f.store.history[testScope]; len(saved) == 0 || strings.Contains(saved[0].Content, heroCheckNote) {
		t.Errorf("saved = %+v", saved)
	}
}
