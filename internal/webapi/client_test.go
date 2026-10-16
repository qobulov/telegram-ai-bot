package webapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(timeout time.Duration) *client {
	return newClient(&http.Client{Timeout: timeout}, "MariaBot-test/1.0")
}

func TestClientIdentifiesItselfAndSendsKeysInHeaders(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/data": ok(`{"n":1}`)})
	header := http.Header{}
	header.Set("X-Auth-Token", "secret")

	var into struct{ N int }
	err := testClient(time.Second).getJSON(context.Background(), call{service: "Test API", url: u.server.URL + "/data", header: header}, &into)

	if err != nil || into.N != 1 {
		t.Fatalf("getJSON = %v, %+v", err, into)
	}
	request := u.requests("/data")[0]
	if got := request.Header.Get("User-Agent"); got != "MariaBot-test/1.0" {
		t.Errorf("User-Agent = %q", got)
	}
	if request.Header.Get("X-Auth-Token") != "secret" || strings.Contains(request.URL.RawQuery, "secret") {
		t.Errorf("the key must travel in a header only: %s %v", request.URL, request.Header)
	}
}

func TestClientErrors(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/busy":        status(http.StatusTooManyRequests),
		"/unavailable": status(http.StatusServiceUnavailable),
		"/broken":      status(http.StatusInternalServerError),
		"/huge":        ok(`"` + strings.Repeat("a", defaultBodyLimit) + `"`),
		"/garbage":     ok(`<html>not json</html>`),
		"/slow":        slow,
	})
	tests := []struct {
		path    string
		want    string
		timeout bool
	}{
		{path: "/busy", want: "Test API is busy, try again in a minute"},
		{path: "/unavailable", want: "Test API is busy, try again in a minute"},
		{path: "/broken", want: "Test API answered with status 500"},
		{path: "/huge", want: "Test API sent more than 64 KB"},
		{path: "/garbage", want: "Test API sent an answer that could not be read"},
		{path: "/slow", want: "Test API did not answer in time", timeout: true},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			var into any
			err := testClient(150*time.Millisecond).getJSON(context.Background(), call{service: "Test API", url: u.server.URL + test.path + "?key=secret"}, &into)

			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if got := errors.Is(err, context.DeadlineExceeded); got != test.timeout {
				t.Errorf("errors.Is(DeadlineExceeded) = %v, want %v", got, test.timeout)
			}
		})
	}
}

func TestClientErrorsNeverShowTheURL(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	address := server.URL
	server.Close()

	_, err := testClient(time.Second).get(context.Background(), call{service: "Test API", url: address + "/x?key=secret"})

	if err == nil || err.Error() != "Test API could not be reached" {
		t.Errorf("error = %v", err)
	}
}

func TestClientHonoursTheCallersDeadline(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/slow": slow})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := testClient(5*time.Second).get(ctx, call{service: "Test API", url: u.server.URL + "/slow"})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want a timeout", err)
	}
}

func TestClientFollowsOnlyShortSameHostRedirects(t *testing.T) {
	elsewhere := newUpstream(t, map[string]stub{"/data": ok(`{}`)})
	mux := http.NewServeMux()
	mux.Handle("/one", http.RedirectHandler("/two", http.StatusFound))
	mux.Handle("/two", http.RedirectHandler("/data", http.StatusFound))
	mux.Handle("/three", http.RedirectHandler("/one", http.StatusFound))
	mux.Handle("/away", http.RedirectHandler(elsewhere.server.URL+"/data", http.StatusFound))
	mux.HandleFunc("/data", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	server := httptest.NewServer(mux)
	defer server.Close()

	for path, wantOK := range map[string]bool{"/one": true, "/three": false, "/away": false} {
		_, err := testClient(time.Second).get(context.Background(), call{service: "Test API", url: server.URL + path})
		if (err == nil) != wantOK {
			t.Errorf("%s: error = %v, want success %v", path, err, wantOK)
		}
	}
	if elsewhere.total() != 0 {
		t.Error("a redirect reached another host")
	}
}

func TestRedirectsKeepTheHostAndScheme(t *testing.T) {
	request := func(address string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, address, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := request("https://api.football-data.org/v4/competitions/PL")
	tests := []struct {
		to     string
		wantOK bool
	}{
		{to: "https://api.football-data.org/v4/competitions/PL/", wantOK: true},
		{to: "http://api.football-data.org/v4/competitions/PL", wantOK: false},
		{to: "https://evil.example/v4/competitions/PL", wantOK: false},
		{to: "https://api.football-data.org:8443/v4/competitions/PL", wantOK: false},
	}
	for _, test := range tests {
		err := sameHostRedirects(request(test.to), []*http.Request{first})
		if (err == nil) != test.wantOK {
			t.Errorf("redirect to %s: error = %v, want allowed %v", test.to, err, test.wantOK)
		}
	}
}

func TestClientBodyLimitCanBeRaised(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/feed": ok(strings.Repeat("a", 100<<10))})

	body, err := testClient(time.Second).get(context.Background(), call{service: "Feed", url: u.server.URL + "/feed", limit: largeBodyLimit})

	if err != nil || len(body) != 100<<10 {
		t.Errorf("get = %d bytes, %v", len(body), err)
	}
}

func TestDefaultClientHasShortTimeouts(t *testing.T) {
	c := newClient(nil, "x")
	transport, isTransport := c.http.Transport.(*http.Transport)
	if c.http.Timeout != 4*time.Second || !isTransport {
		t.Fatalf("client = %+v", c.http)
	}
	if transport.TLSHandshakeTimeout != 2*time.Second || transport.ResponseHeaderTimeout != 3*time.Second || transport.MaxIdleConnsPerHost != 4 {
		t.Errorf("transport = %+v", transport)
	}
	if c.http.CheckRedirect == nil {
		t.Error("redirects are not checked")
	}
}
