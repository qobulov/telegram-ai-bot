package webapi

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"telegram-ai-bot/internal/tools"
)

// runWithPicture runs a tool in a turn whose message came with picture.
func runWithPicture(t *testing.T, u *upstream, name, args string, picture []byte) envelope {
	t.Helper()
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: 1, Human: true, Picture: picture}, &tools.Sink{})
	var env envelope
	output := newRegistry(t, u.options()).Execute(ctx, 1, name, args)
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("output %q", output)
	}
	env.raw = output
	return env
}

const traceMoeRabbit = `{"error":"","result":[{"anilist":{"title":{"native":"ご注文はうさぎですか？？","romaji":"Gochuumon wa Usagi desu ka??","english":"Is the Order a Rabbit?? Season 2"},"isAdult":false},"episode":1,"from":272.18,"similarity":0.992}]}`

func TestSceneIsFoundFromTheUploadedPicture(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/tracemoe/search": ok(traceMoeRabbit)})

	env := runWithPicture(t, u, "anime", `{"action":"scene"}`, []byte("\xff\xd8\xffJPEG"))

	if !env.OK || !strings.Contains(env.raw, `"anime":"Is the Order a Rabbit?? Season 2"`) || !strings.Contains(env.raw, `"at":"4:32"`) || !strings.Contains(env.raw, `"episode":"1"`) || strings.Contains(env.raw, "note") {
		t.Fatalf("env = %s", env.raw)
	}
	r := u.requests("/tracemoe/search")[0]
	body, _ := io.ReadAll(r.Body)
	if r.Method != "POST" || string(body) != "\xff\xd8\xffJPEG" || r.Header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("request %s %q, type %q", r.Method, body, r.Header.Get("Content-Type"))
	}
}

func TestSceneNeedsAPictureAndRefusesAdultOnes(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/tracemoe/search": ok(strings.Replace(traceMoeRabbit, `"isAdult":false`, `"isAdult":true`, 1))})
	wantError(t, runWithPicture(t, u, "anime", `{"action":"scene"}`, nil), "invalid_arguments", "no picture")
	wantError(t, runWithPicture(t, u, "anime", `{"action":"scene"}`, []byte("\xff\xd8\xffJPEG")), "invalid_arguments", "adult")

	weak := newUpstream(t, map[string]stub{"/tracemoe/search": ok(strings.Replace(traceMoeRabbit, `"similarity":0.992`, `"similarity":0.71`, 1))})
	if env := runWithPicture(t, weak, "anime", `{"action":"scene"}`, []byte("\xff\xd8\xffJPEG")); !env.OK || !strings.Contains(env.raw, "only a guess") {
		t.Errorf("weak match = %s", env.raw)
	}
}

func TestRussianSearchFromShikimori(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/shikimori/api/animes":    ok(`[{"id":20,"name":"Naruto","russian":"Наруто","url":"/animes/z20-naruto","kind":"tv","score":"8.03","status":"released","episodes":220,"aired_on":"2002-10-03"}]`),
		"/shikimori/api/animes/20": ok(`{"description":"На деревню напал [character=7407]Девятихвостый лис[/character].","genres":[{"russian":"Экшен"}],"studios":[{"name":"Studio Pierrot"}]}`),
	})

	env := run(t, u.options(), "anime", `{"action":"search","query":"naruto","language":"ru"}`)

	if !env.OK || !strings.Contains(env.raw, `"russian":"Наруто"`) || !strings.Contains(env.raw, `"about":"На деревню напал Девятихвостый лис."`) || !strings.Contains(env.raw, `"link":"`+u.server.URL+`/shikimori/animes/z20-naruto"`) {
		t.Errorf("env = %s", env.raw)
	}
}

func TestMangaFromAniList(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/anilist": ok(`{"data":{"Page":{"media":[{"title":{"romaji":"One Piece","english":"One Piece"},"averageScore":88,"chapters":1100,"volumes":108,"startDate":{"year":1997},"status":"RELEASING","siteUrl":"https://anilist.co/manga/30013"}]}}}`)})

	env := run(t, u.options(), "anime", `{"action":"manga","query":"one piece"}`)

	if !env.OK || !strings.Contains(env.raw, `"chapters":1100`) || !strings.Contains(env.raw, `"year":1997`) {
		t.Errorf("env = %s", env.raw)
	}
	if body, _ := io.ReadAll(u.requests("/anilist")[0].Body); !strings.Contains(string(body), "type: MANGA") {
		t.Errorf("query = %s", body)
	}
}
