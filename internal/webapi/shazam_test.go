package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/tools"
)

// shazamRondo is the part of a real Shazam answer the tool reads, its cover
// moved to the test server.
const shazamRondo = `{"matches":[{"id":"1"}],"track":{"title":"Rondo alla Turca","subtitle":"Wilhelm Kempff",
	"url":"https://www.shazam.com/track/1/rondo-alla-turca","isrc":"DEA1","genres":{"primary":"Classical"},
	"images":{"coverart":"COVER/shazam-cover.jpg"},
	"sections":[{"type":"SONG","metadata":[{"title":"Album","text":"Mozart: Piano Sonatas"},{"title":"Label","text":"DG"},{"title":"Released","text":"1962"}]},{"type":"RELATED"}]}}`

type fakeFiles struct {
	files map[string][]byte
	asked []string
}

func (f *fakeFiles) DownloadFile(_ context.Context, fileID string, _ int64) ([]byte, error) {
	f.asked = append(f.asked, fileID)
	data, ok := f.files[fileID]
	if !ok {
		return nil, errors.New("no such file")
	}
	return data, nil
}

// fakeSongQuota grants limit recognitions a user, -1 for any number.
type fakeSongQuota struct {
	taken    map[int64]int
	limits   []int
	released []int64
	next     int64
}

func (q *fakeSongQuota) ReserveSong(_ context.Context, userID, _ int64, _ time.Time, limit int) (int64, bool, error) {
	q.limits = append(q.limits, limit)
	if limit >= 0 && q.taken[userID] >= limit {
		return 0, false, nil
	}
	q.taken[userID]++
	q.next++
	return q.next, true, nil
}

func (q *fakeSongQuota) ReleaseSong(_ context.Context, id int64) error {
	q.released = append(q.released, id)
	return nil
}

type songTest struct {
	u     *upstream
	files *fakeFiles
	quota *fakeSongQuota
}

func newSongTest(t *testing.T, answer stub) songTest {
	u := newUpstream(t, map[string]stub{
		"/shazam/recognize/file": answer,
		"/shazam-cover.jpg":      ok("\xff\xd8\xffJPEG"),
	})
	return songTest{
		u:     u,
		files: &fakeFiles{files: map[string][]byte{"voice-1": []byte("OggS sound")}},
		quota: &fakeSongQuota{taken: map[int64]int{}},
	}
}

func (s songTest) run(t *testing.T, caller tools.Caller) (envelope, *tools.Sink) {
	t.Helper()
	opts := s.u.options()
	opts.BaseURLs.Shazam = s.u.server.URL + "/shazam"
	opts.ShazamKey, opts.Files, opts.SongQuota = "rapid-test-key", s.files, s.quota
	sink := &tools.Sink{}
	ctx := tools.WithTurn(context.Background(), caller, sink)
	var env envelope
	output := newRegistry(t, opts).Execute(ctx, caller.UserID, "recognize_song", `{}`)
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("output %q", output)
	}
	env.raw = output
	return env, sink
}

var voiceCaller = tools.Caller{UserID: 7, ChatID: -100, Human: true, Sound: &tools.Sound{FileID: "voice-1", Size: 10}}

// The sound itself is uploaded with the key, never a Telegram link, and the
// song is the whole answer, written in code: the cover, a caption with no
// Markdown link, link buttons and the preview of the recording of that ISRC.
func TestRecognizeSongUploadsTheSound(t *testing.T) {
	s := newSongTest(t, ok(""))
	s.u.routes["/shazam/recognize/file"] = ok(strings.Replace(shazamRondo, "COVER", s.u.server.URL, 1))
	s.u.routes["/deezer/track/isrc:DEA1"] = ok(`{"title":"Rondo","link":"https://www.deezer.com/track/9","preview":"` + s.u.server.URL + `/preview.mp3","artist":{"name":"Kempff"}}`)
	s.u.routes["/preview.mp3"] = ok("ID3 preview")

	env, sink := s.run(t, voiceCaller)

	if !env.OK || !strings.Contains(env.raw, `"title":"Rondo alla Turca"`) || !strings.Contains(env.raw, `"album":"Mozart: Piano Sonatas"`) {
		t.Fatalf("output = %s", env.raw)
	}
	r := s.u.requests("/shazam/recognize/file")[0]
	body, _ := io.ReadAll(r.Body)
	if r.Method != "POST" || string(body) != "OggS sound" || r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("X-Rapidapi-Key") != "rapid-test-key" {
		t.Errorf("request %s %q, type %q", r.Method, body, r.Header.Get("Content-Type"))
	}
	if drawings := sink.Take(); len(drawings) != 1 || string(drawings[0].Data) != "\xff\xd8\xffJPEG" {
		t.Errorf("drawings = %d", len(drawings))
	}
	caption, finished := sink.Finished()
	if !finished || caption != "🎵 **Rondo alla Turca**\n👤 Wilhelm Kempff\n💿 Mozart: Piano Sonatas · 1962" {
		t.Errorf("caption = %q", caption)
	}
	var labels []string
	for _, row := range sink.Buttons() {
		for _, button := range row {
			labels = append(labels, button.Text)
			if !strings.HasPrefix(button.URL, "https://") {
				t.Errorf("button %+v", button)
			}
		}
	}
	if strings.Join(labels, ",") != "Deezer,Spotify,YouTube,Shazam" {
		t.Errorf("buttons = %v", labels)
	}
	audios := sink.TakeAudios()
	if len(audios) != 1 || string(audios[0].Data) != "ID3 preview" || !audios[0].AfterAnswer {
		t.Errorf("audios = %+v", audios)
	}
	if len(s.quota.released) != 0 || s.quota.limits[0] != songDailyLimit {
		t.Errorf("quota = %+v", s.quota)
	}
}

// Without its ISRC on Deezer, a search's result goes only if it is that song;
// another song's preview is not sent, and the song is still answered.
func TestRecognizeSongPreviewOnlyOfThatSong(t *testing.T) {
	for name, deezer := range map[string]string{
		"same":  `{"data":[{"title":"rondo ALLA turca","link":"https://www.deezer.com/track/9","preview":"PREVIEW","artist":{"name":"Wilhelm Kempff"}}]}`,
		"notes": `{"data":[{"title":"Rondo alla Turca (Remastered)","link":"https://www.deezer.com/track/7","preview":"PREVIEW","artist":{"name":"Wilhelm Kempff"}}]}`,
		"other": `{"data":[{"title":"Rondo alla Turca","link":"https://www.deezer.com/track/8","preview":"PREVIEW","artist":{"name":"Someone Else"}}]}`,
		"none":  `{"data":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newSongTest(t, ok(shazamRondo))
			s.u.routes["/deezer/search"] = ok(strings.Replace(deezer, "PREVIEW", s.u.server.URL+"/preview.mp3", 1))
			s.u.routes["/preview.mp3"] = ok("ID3 preview")

			env, sink := s.run(t, voiceCaller)

			if _, finished := sink.Finished(); !env.OK || !finished {
				t.Fatalf("env = %s", env.raw)
			}
			want := 0
			if name == "same" || name == "notes" {
				want = 1
			}
			if got := len(sink.TakeAudios()); got != want {
				t.Errorf("audios = %d; want %d", got, want)
			}
		})
	}
}

// A name's marks are not read as formatting, and a link that is not https is
// left out: Telegram would refuse the whole message for it.
func TestSongCaptionAndButtonsAreSafe(t *testing.T) {
	if got := songCaption("a*b [x](tg://user?id=1)", "`me`", "", ""); got != "🎵 **ab (x)(tg://user?id=1)**\n👤 me" {
		t.Errorf("caption = %q", got)
	}
	for _, row := range songButtons("intent://shazam", "", "Me", "Song") {
		for _, button := range row {
			if button.Text == "Shazam" || button.Text == "Deezer" {
				t.Errorf("button %+v", button)
			}
		}
	}
}

// Without a sound, or with one over what a bot may download, nothing is
// downloaded or counted.
func TestRecognizeSongNeedsASoundItCanDownload(t *testing.T) {
	s := newSongTest(t, ok(shazamRondo))
	noSound := voiceCaller
	noSound.Sound = nil
	wantError(t, first(s.run(t, noSound)), "invalid_arguments", "no voice message")
	big := voiceCaller
	big.Sound = &tools.Sound{FileID: "voice-1", Size: maxSoundBytes + 1}
	wantError(t, first(s.run(t, big)), "invalid_arguments", "20 MB")
	if len(s.files.asked) != 0 || len(s.quota.limits) != 0 {
		t.Errorf("downloaded %v, quota %+v", s.files.asked, s.quota)
	}
}

// A song Shazam does not know is said so and still counted; a failed
// request gives the recognition back.
func TestRecognizeSongNotFoundAndFailures(t *testing.T) {
	s := newSongTest(t, ok(`{"matches":[],"tagid":"x"}`))
	if env, _ := s.run(t, voiceCaller); !env.OK || !strings.Contains(env.raw, `"found":false`) || len(s.quota.released) != 0 {
		t.Errorf("not found = %s, released %v", env.raw, s.quota.released)
	}

	failing := newSongTest(t, status(500))
	if env, _ := failing.run(t, voiceCaller); env.OK || len(failing.quota.released) != 1 {
		t.Errorf("failure = %s, released %v", env.raw, failing.quota.released)
	}
}

// Five a day each, then the daily limit; owners have none.
func TestRecognizeSongDailyLimit(t *testing.T) {
	s := newSongTest(t, ok(`{"matches":[]}`))
	for range songDailyLimit {
		if env, _ := s.run(t, voiceCaller); !env.OK {
			t.Fatalf("within the limit: %s", env.raw)
		}
	}
	wantError(t, first(s.run(t, voiceCaller)), "daily_limit", "song recognitions")

	owner := voiceCaller
	owner.Owner = true
	if env, _ := s.run(t, owner); !env.OK || s.quota.limits[len(s.quota.limits)-1] != -1 {
		t.Errorf("owner = %s, limits %v", env.raw, s.quota.limits)
	}
}

// The tool is offered only with a key.
func TestRecognizeSongNeedsAKey(t *testing.T) {
	opts := Options{Files: &fakeFiles{}, SongQuota: &fakeSongQuota{}}
	for _, tool := range Tools(opts) {
		if tool.Name == "recognize_song" {
			t.Fatal("recognize_song offered without a key")
		}
	}
}

func first(env envelope, _ *tools.Sink) envelope { return env }
