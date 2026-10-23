package guards

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServices answers as CAS, lols.bot, URLhaus and Sightengine do, by
// path, and keeps the last request's form and headers.
func fakeServices(t *testing.T, answers map[string]string) (*Checker, *http.Request) {
	t.Helper()
	seen := &http.Request{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			_ = r.ParseMultipartForm(1 << 20)
		} else {
			_ = r.ParseForm()
		}
		*seen = *r
		answer, ok := answers[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(server.Close)
	return New(Options{
		CAS: server.URL + "/cas", Lols: server.URL + "/lols", URLhaus: server.URL + "/urlhaus", Sightengine: server.URL + "/se",
		URLhausKey: "uh-key", SightengineUser: "se-user", SightengineSecret: "se-secret",
	}), seen
}

func TestSpammerChecksBothLists(t *testing.T) {
	c, _ := fakeServices(t, map[string]string{
		"/cas/check?user_id=1": `{"ok":true,"result":{"offenses":3}}`,
		"/cas/check?user_id=2": `{"ok":false,"description":"Record not found."}`,
		"/lols/account?id=2":   `{"ok":true,"user_id":2,"banned":true}`,
		"/cas/check?user_id=3": `{"ok":false,"description":"Record not found."}`,
		"/lols/account?id=3":   `{"ok":true,"user_id":3,"banned":false}`,
		"/lols/account?id=4":   `{"ok":true,"user_id":4,"banned":false}`,
	})
	for id, want := range map[int64]string{1: "CAS", 2: "lols.bot", 3: "", 4: ""} {
		source, bad, err := c.Spammer(context.Background(), id)
		if err != nil || source != want || bad != (want != "") {
			t.Errorf("Spammer(%d) = %q, %v, %v; want %q", id, source, bad, err, want)
		}
	}
	if _, _, err := c.Spammer(context.Background(), 5); err == nil {
		t.Error("both lists failing was taken for a clean account")
	}
}

func TestMaliciousHostSendsTheKeyAndReadsTheVerdict(t *testing.T) {
	c, seen := fakeServices(t, map[string]string{"/urlhaus/v1/host/": `{"query_status":"ok","blacklists":{"spamhaus_dbl":"not listed","surbl":"not listed"},
		"urls":[{"url_status":"offline","threat":"malware_download"},{"url_status":"online","threat":"malware_download"}]}`})
	reason, bad, err := c.MaliciousHost(context.Background(), "evil.example")
	if err != nil || !bad || reason != "URLhaus: malware_download" {
		t.Fatalf("MaliciousHost = %q, %v, %v", reason, bad, err)
	}
	if seen.Header.Get("Auth-Key") != "uh-key" || seen.PostForm.Get("host") != "evil.example" {
		t.Errorf("request header %v, form %v", seen.Header, seen.PostForm)
	}
}

func TestMaliciousHostKeepsUnknownAndOfflineHosts(t *testing.T) {
	for name, answer := range map[string]string{
		"unknown": `{"query_status":"no_results"}`,
		"offline": `{"query_status":"ok","blacklists":{"spamhaus_dbl":"not listed","surbl":"not listed"},"urls":[{"url_status":"offline","threat":"malware_download"}]}`,
	} {
		c, _ := fakeServices(t, map[string]string{"/urlhaus/v1/host/": answer})
		if _, bad, err := c.MaliciousHost(context.Background(), "x.example"); bad || err != nil {
			t.Errorf("%s host: bad %v, err %v", name, bad, err)
		}
	}
	c, _ := fakeServices(t, map[string]string{"/urlhaus/v1/host/": `{"query_status":"ok","blacklists":{"spamhaus_dbl":"abused_legit_malware","surbl":"not listed"},"urls":[]}`})
	if reason, bad, _ := c.MaliciousHost(context.Background(), "x.example"); !bad || reason != "URLhaus: abused_legit_malware" {
		t.Errorf("a blocklisted host = %q, %v", reason, bad)
	}
}

func TestUnsafeImageUploadsThePicture(t *testing.T) {
	cases := map[string]struct{ answer, want string }{
		"nude":  {`{"status":"success","nudity":{"sexual_activity":0.01,"erotica":0.92,"context":{}},"gore":{"prob":0.01}}`, "nudity"},
		"gory":  {`{"status":"success","nudity":{"sexual_activity":0.01},"gore":{"prob":0.88}}`, "gore"},
		"clean": {`{"status":"success","nudity":{"sexual_activity":0.01,"erotica":0.2},"gore":{"prob":0.02}}`, ""},
	}
	for name, tc := range cases {
		c, seen := fakeServices(t, map[string]string{"/se/1.0/check.json": tc.answer})
		reason, bad, err := c.UnsafeImage(context.Background(), []byte("JPEG"))
		if err != nil || reason != tc.want || bad != (tc.want != "") {
			t.Errorf("%s: %q, %v, %v", name, reason, bad, err)
		}
		if seen.MultipartForm == nil || seen.FormValue("api_user") != "se-user" || seen.FormValue("models") != "nudity-2.1,gore-2.0" || len(seen.MultipartForm.File["media"]) != 1 {
			t.Errorf("%s: the picture was not uploaded with the keys", name)
		}
	}
	c, _ := fakeServices(t, map[string]string{"/se/1.0/check.json": `{"status":"failure","error":{"message":"Monthly limit reached"}}`})
	if _, _, err := c.UnsafeImage(context.Background(), []byte("JPEG")); err == nil || !strings.Contains(err.Error(), "Monthly limit") {
		t.Errorf("a refusal = %v", err)
	}
}

func TestKeysDecideWhatCanBeChecked(t *testing.T) {
	if c := New(Options{}); c.CanCheckLinks() || c.CanCheckImages() {
		t.Error("checks without keys")
	}
	if c := New(Options{URLhausKey: "k", SightengineUser: "u"}); !c.CanCheckLinks() || c.CanCheckImages() {
		t.Error("the picture check needs both Sightengine keys")
	}
}
