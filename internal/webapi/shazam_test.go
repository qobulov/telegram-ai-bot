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
	"url":"https://www.shazam.com/track/1/rondo-alla-turca","genres":{"primary":"Classical"},
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

// The sound itself is uploaded with the key, never a Telegram link, and
// the song comes back with its album and links, its cover sent.
func TestRecognizeSongUploadsTheSound(t *testing.T) {
	s := newSongTest(t, ok(""))
	s.u.routes["/shazam/recognize/file"] = ok(strings.Replace(shazamRondo, "COVER", s.u.server.URL, 1))

	env, sink := s.run(t, voiceCaller)

	for _, want := range []string{`"title":"Rondo alla Turca"`, `"artist":"Wilhelm Kempff"`, `"album":"Mozart: Piano Sonatas"`, `"released":"1962"`, `"label":"DG"`, `"shazam":"https://www.shazam.com/track/1/rondo-alla-turca"`, "cover is sent"} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Fatalf("output lacks %s: %s", want, env.raw)
		}
	}
	r := s.u.requests("/shazam/recognize/file")[0]
	body, _ := io.ReadAll(r.Body)
	if r.Method != "POST" || string(body) != "OggS sound" || r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("X-Rapidapi-Key") != "rapid-test-key" {
		t.Errorf("request %s %q, type %q", r.Method, body, r.Header.Get("Content-Type"))
	}
	if drawings := sink.Take(); len(drawings) != 1 || string(drawings[0].Data) != "\xff\xd8\xffJPEG" {
		t.Errorf("drawings = %d", len(drawings))
	}
	if len(s.quota.released) != 0 || s.quota.limits[0] != songDailyLimit {
		t.Errorf("quota = %+v", s.quota)
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
