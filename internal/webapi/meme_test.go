package webapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"telegram-ai-bot/internal/tools"
)

const memeTemplatesJSON = `{"success":true,"data":{"memes":[
	{"id":"181913649","name":"Drake Hotline Bling","box_count":2},
	{"id":"112126428","name":"Distracted Boyfriend","box_count":3}]}}`

func runMeme(t *testing.T, u *upstream, args string) (envelope, *tools.Sink) {
	t.Helper()
	opts := u.options()
	opts.ImgflipKey = "imgflip-test-key"
	sink := &tools.Sink{}
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true}, sink)
	var env envelope
	output := newRegistry(t, opts).Execute(ctx, 1, "make_meme", args)
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("output %q", output)
	}
	env.raw = output
	return env, sink
}

// The template is found by part of its name, the texts go in its boxes in
// a font with Cyrillic letters, the key in a header, and the picture is
// sent.
func TestMakeMemeCaptionsATemplate(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/imgflip/get_memes":   ok(memeTemplatesJSON),
		"/imgflip/meme/b4.jpg": ok("\xff\xd8\xffJPEG"),
	})
	u.routes["/imgflip/caption_image"] = ok(`{"success":true,"data":{"url":"` + u.server.URL + `/imgflip/meme/b4.jpg"}}`)

	env, sink := runMeme(t, u, `{"template":"drake","texts":["Darsga borish","МЛББ ўйнаш"]}`)

	if drawings := sink.Take(); !env.OK || len(drawings) != 1 || drawings[0].MimeType != "image/jpeg" || !strings.Contains(env.raw, "Drake Hotline Bling") {
		t.Fatalf("env %s, drawings %d", env.raw, len(drawings))
	}
	asked := u.requests("/imgflip/caption_image")[0]
	if asked.Header.Get("Authorization") != "Bearer imgflip-test-key" || strings.Contains(asked.URL.String(), "imgflip-test-key") {
		t.Errorf("the key was not sent in the header alone: %v %v", asked.URL, asked.Header)
	}
	if err := asked.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if asked.PostForm.Get("template_id") != "181913649" || asked.PostForm.Get("boxes[1][text]") != "МЛББ ўйнаш" || asked.PostForm.Get("font") != memeFont {
		t.Errorf("form = %v", asked.PostForm)
	}
}

func TestMakeMemeRefusesWhatItCannotMake(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/imgflip/get_memes": ok(memeTemplatesJSON)})
	for args, want := range map[string]string{
		`{"template":"Shrek","texts":["a"]}`:                                "pick one of: Drake Hotline Bling, Distracted Boyfriend",
		`{"template":"Drake","texts":["a","b","c"]}`:                        "takes 1 to 2 texts",
		`{"template":"Drake","texts":["` + strings.Repeat("x", 101) + `"]}`: "at most 100 characters",
	} {
		if env, _ := runMeme(t, u, args); env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("%.40s: %s, want %q", args, env.raw, want)
		}
	}
	if len(u.requests("/imgflip/caption_image")) != 0 {
		t.Error("Imgflip was asked to caption a refused meme")
	}
}

// A link outside Imgflip in its answer is not fetched.
func TestMakeMemeFetchesOnlyFromImgflip(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/imgflip/get_memes":     ok(memeTemplatesJSON),
		"/imgflip/caption_image": ok(`{"success":true,"data":{"url":"http://169.254.169.254/latest"}}`),
	})
	if env, sink := runMeme(t, u, `{"template":"Drake","texts":["a","b"]}`); env.OK || len(sink.Take()) != 0 {
		t.Errorf("env %s", env.raw)
	}
}

func TestMakeMemeNeedsAKey(t *testing.T) {
	for _, tool := range Tools(Options{}) {
		if tool.Name == "make_meme" {
			t.Fatal("make_meme is offered without an Imgflip key")
		}
	}
}

// Left to itself the model drew Drake ten times in nineteen memes: a
// template used in a chat's last memes is refused there, with others
// offered, unless the asker names it.
func TestMakeMemeVariesTheTemplate(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/imgflip/get_memes":   ok(memeTemplatesJSON),
		"/imgflip/meme/b4.jpg": ok("\xff\xd8\xffJPEG"),
	})
	u.routes["/imgflip/caption_image"] = ok(`{"success":true,"data":{"url":"` + u.server.URL + `/imgflip/meme/b4.jpg"}}`)
	opts := u.options()
	opts.ImgflipKey = "imgflip-test-key"
	registry := newRegistry(t, opts)
	meme := func(chatID int64, words, args string) string {
		ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: chatID, Human: true, Words: words}, &tools.Sink{})
		return registry.Execute(ctx, 1, "make_meme", args)
	}
	drake := `{"template":"Drake","texts":["a","b"]}`

	if out := meme(-100, "mem qil", drake); !strings.Contains(out, `"sent":true`) {
		t.Fatalf("first meme: %s", out)
	}
	if out := meme(-100, "yana mem qil", drake); strings.Contains(out, `"sent":true`) || !strings.Contains(out, "Distracted Boyfriend (3)") || strings.Contains(out, "such as: Drake") {
		t.Errorf("Drake again: %s; want it refused, Distracted Boyfriend offered", out)
	}
	if out := meme(-100, "drake mem qil", drake); !strings.Contains(out, `"sent":true`) {
		t.Errorf("Drake asked for by name: %s; want it made", out)
	}
	if out := meme(-200, "mem qil", drake); !strings.Contains(out, `"sent":true`) {
		t.Errorf("Drake in another chat: %s; want it made", out)
	}
}
