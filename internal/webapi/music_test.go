package webapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"telegram-ai-bot/internal/tools"
)

const deezerShohruhxon = `{"data":[
	{"title":"Yurak","link":"https://www.deezer.com/track/1","duration":201,"preview":"https://cdns-preview.dzcdn.net/1.mp3","artist":{"name":"Shohruhxon"},"album":{"title":"Yurak"}},
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

func TestMusicSearchFindsSongsAndSendsAPreview(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/deezer/search": ok(deezerShohruhxon)})

	env, sink := musicRun(t, u, `{"query":"Shohruhxon  Yurak"}`)

	out := result[musicOutput](t, env)
	if len(out.Tracks) != 2 || out.Tracks[0] != (musicTrack{Title: "Yurak", Artist: "Shohruhxon", Album: "Yurak", Duration: "3:21", Link: "https://www.deezer.com/track/1"}) || out.Source != "Deezer" || out.Note == "" {
		t.Errorf("output = %+v", out)
	}
	if audios := sink.TakeAudios(); len(audios) != 1 || audios[0] != (tools.Audio{URL: "https://cdns-preview.dzcdn.net/1.mp3", Title: "Yurak", Performer: "Shohruhxon"}) {
		t.Errorf("audios = %+v", audios)
	}
	if q := u.requests("/deezer/search")[0].URL.Query(); q.Get("q") != "Shohruhxon Yurak" || q.Get("limit") != "5" {
		t.Errorf("query = %v", q)
	}
}

func TestMusicSearchWithoutPreview(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/deezer/search": ok(deezerShohruhxon)})
	env, sink := musicRun(t, u, `{"query":"Shohruhxon","preview":false}`)
	if out := result[musicOutput](t, env); out.Note != "" || len(sink.TakeAudios()) != 0 {
		t.Errorf("a preview was sent: %+v", out)
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
	if env.OK || strings.Contains(env.raw, "cdns-preview") {
		t.Errorf("a refusal was taken for an answer: %s", env.raw)
	}
	env, _ = musicRun(t, u, `{"query":"   "}`)
	wantError(t, env, "invalid_arguments", "query")
}
