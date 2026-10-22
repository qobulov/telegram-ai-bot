package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	clientTimeout = 4 * time.Second
	// patientTimeout is for services that think before answering, such as
	// a web search, which took over 3 s.
	patientTimeout = 10 * time.Second
	maxRedirects   = 2
	// defaultBodyLimit bounds a JSON answer. The "on this day" list and
	// football answers are larger and ask for largeBodyLimit; news feeds
	// have a limit of their own.
	defaultBodyLimit = 64 << 10
	largeBodyLimit   = 256 << 10
	// errorBodyLimit is how much of a failed answer is kept for the tools
	// that read an API's own explanation.
	errorBodyLimit = 4 << 10
)

// client makes GET requests to the public APIs. Its errors name the
// service and never the URL, because tool errors reach the model and the
// logs, and a URL can carry a key.
type client struct {
	http      *http.Client
	userAgent string
}

// call is one GET request, or a POST when it has a body.
type call struct {
	// service names the API in errors, e.g. "Open-Meteo".
	service string
	url     string
	// header carries keys; they never go in the URL.
	header http.Header
	// limit bounds the body. Zero means defaultBodyLimit.
	limit int64
	// body, when set, makes the call a POST of this JSON.
	body []byte
}

// newClient wraps base, or a client with short timeouts when base is nil,
// so that redirects can only stay on the same host.
func newClient(base *http.Client, userAgent string) *client {
	if base == nil {
		base = defaultHTTPClient(clientTimeout)
	}
	wrapped := *base
	wrapped.CheckRedirect = sameHostRedirects
	return &client{http: &wrapped, userAgent: userAgent}
}

// patientClient is newClient for slow services, waiting patientTimeout.
func patientClient(base *http.Client, userAgent string) *client {
	if base == nil {
		base = defaultHTTPClient(patientTimeout)
	}
	return newClient(base, userAgent)
}

// defaultHTTPClient gives up after timeout, and on headers a second before.
func defaultHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   2 * time.Second,
			ResponseHeaderTimeout: timeout - time.Second,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

// sameHostRedirects keeps a redirect from taking a request, and its key
// headers, to another host, or from https down to plain http on the same
// host: Go copies custom headers such as X-Auth-Token on a same-host
// redirect, so either would hand the key to whoever is listening.
func sameHostRedirects(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return errors.New("too many redirects")
	}
	if req.URL.Host != via[0].URL.Host {
		return errors.New("redirected to another host")
	}
	if req.URL.Scheme != via[0].URL.Scheme {
		return errors.New("redirected to another scheme")
	}
	return nil
}

func (c *client) get(ctx context.Context, cl call) ([]byte, error) {
	method, payload := http.MethodGet, io.Reader(nil)
	if cl.body != nil {
		method, payload = http.MethodPost, bytes.NewReader(cl.body)
	}
	req, err := http.NewRequestWithContext(ctx, method, cl.url, payload)
	if err != nil {
		return nil, fmt.Errorf("%s could not be asked", cl.service)
	}
	if cl.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, values := range cl.header {
		req.Header[name] = values
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportError(cl.service, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		return nil, &busyError{service: cl.service}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return nil, &statusError{service: cl.service, status: resp.StatusCode, body: body}
	}
	limit := cl.limit
	if limit <= 0 {
		limit = defaultBodyLimit
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, transportError(cl.service, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s sent more than %d KB", cl.service, limit>>10)
	}
	return body, nil
}

func (c *client) getJSON(ctx context.Context, cl call, into any) error {
	body, err := c.get(ctx, cl)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%s sent an answer that could not be read", cl.service)
	}
	return nil
}

// busyError is an API saying it is rate limiting or overloaded. Retrying
// at once would only make it worse.
type busyError struct{ service string }

func (e *busyError) Error() string { return e.service + " is busy, try again in a minute" }

// statusError is any other answer outside 2xx. body holds the start of
// the answer, for APIs that explain themselves.
type statusError struct {
	service string
	status  int
	body    []byte
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s answered with status %d", e.service, e.status)
}

// timeoutError unwraps to context.DeadlineExceeded, so the registry
// reports it as a timeout.
type timeoutError struct{ service string }

func (e *timeoutError) Error() string { return e.service + " did not answer in time" }
func (e *timeoutError) Unwrap() error { return context.DeadlineExceeded }

// transportError replaces err, whose *url.Error carries the full URL, with
// one that names only the service.
func transportError(service string, err error) error {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return &timeoutError{service: service}
	}
	return fmt.Errorf("%s could not be reached", service)
}
