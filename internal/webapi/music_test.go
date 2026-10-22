package webapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"telegram-ai-bot/internal/tools"
)

// deezerShohruhxon is a search answer; previews are at {{base}}.
const deezerShohruhxon = `{"data":[
	{"title":"Yurak","link":"https://www.deezer.com/track/1","duration":201,"preview":"{{base}}/preview/1.mp3","artist":{"name":"Shohruhxon"},"album":{"title":"Yurak"}},
	{"title":"Parizod","link":"https://www.deezer.com/track/2","duration":185,"preview":"","artist":{"name":"Shohruhxon"},"album":{"title":"Parizod"}}],"total":2}`

// musicRun runs music_search in a turn, so that a preview lands in sink.
func musicRun(t *testing.T, u *upstream, args string) (envelope, *tools.Sink) {
	t.Helper()
	sink := &tools.Sink{}
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true}, sink)
	var env envelope
	output := newRegistry(t, u.options()).Execute(ctx, 1, "music_search", args)
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("output %q", output)
	}
	env.raw = output
	return env, sink
}

// musicUpstream answers a search with deezerShohruhxon and its preview with
// preview.
func musicUpstream(t *testing.T, preview stub) *upstream {
	u := newUpstream(t, map[string]stub{"/preview/1.mp3": preview})
	u.routes["/deezer/search"] = ok(strings.ReplaceAll(deezerShohruhxon, "{{base}}", u.server.URL))
	return u
}

func TestMusicSearchFindsSongsAndSendsAPreview(t *testing.T) {
	u := musicUpstream(t, ok("MP3"))

	env, sink := musicRun(t, u, `{"query":"Shohruhxon  Yurak"}`)

	out := result[musicOutput](t, env)
	if len(out.Tracks) != 2 || out.Tracks[0] != (musicTrack{Title: "Yurak", Artist: "Shohruhxon", Album: "Yurak", Duration: "3:21", Link: "https://www.deezer.com/track/1"}) || out.Source != "Deezer" || out.Note != previewNote {
		t.Errorf("output = %+v", out)
	}
	if audios := sink.TakeAudios(); len(audios) != 1 || string(audios[0].Data) != "MP3" || audios[0].Title != "Yurak" || audios[0].Performer != "Shohruhxon" {
		t.Errorf("audios = %+v", audios)
	}
	if q := u.requests("/deezer/search")[0].URL.Query(); q.Get("q") != "Shohruhxon Yurak" || q.Get("limit") != "5" {
		t.Errorf("query = %v", q)
	}
}

func TestMusicSearchWithoutPreview(t *testing.T) {
	u := musicUpstream(t, ok("MP3"))
	env, sink := musicRun(t, u, `{"query":"Shohruhxon","preview":false}`)
	if out := result[musicOutput](t, env); out.Note != "" || len(sink.TakeAudios()) != 0 || len(u.requests("/preview/1.mp3")) != 0 {
		t.Errorf("a preview was sent: %+v", out)
	}
}

// A preview that cannot be fetched leaves the songs, and the model is told
// to answer with the link.
func TestMusicSearchPreviewUnavailable(t *testing.T) {
	u := musicUpstream(t, status(403))
	env, sink := musicRun(t, u, `{"query":"Shohruhxon"}`)
	if out := result[musicOutput](t, env); len(out.Tracks) != 2 || out.Note != noPreviewNote || len(sink.TakeAudios()) != 0 {
		t.Errorf("output = %+v", out)
	}
}

func TestMusicSearchNothingFoundOrRefused(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/deezer/search":            ok(`{"data":[],"total":0}`),
		"/deezer/search?q=throttle": ok(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`),
	})
	env, sink := musicRun(t, u, `{"query":"Ozoda Nursaidova"}`)
	wantError(t, env, "invalid_arguments", "no song")
	if len(sink.TakeAudios()) != 0 {
		t.Error("a preview was sent for nothing found")
	}
	env, _ = musicRun(t, u, `{"query":"throttle"}`)
	if env.OK || strings.Contains(env.raw, "preview/1.mp3") {
		t.Errorf("a refusal was taken for an answer: %s", env.raw)
	}
	env, _ = musicRun(t, u, `{"query":"   "}`)
	wantError(t, env, "invalid_arguments", "query")
}
