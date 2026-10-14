package cron

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const secret = "a-long-random-cron-secret"

type job struct {
	runs   int
	result any
	err    error
}

func (j *job) run(context.Context) (any, error) {
	j.runs++
	return j.result, j.err
}

func call(t *testing.T, handler http.Handler, method, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/cleanup", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func newHandler(t *testing.T, j *job) http.Handler {
	t.Helper()
	handler, err := NewHandler(Options{Secret: secret, Run: j.run})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return handler
}

func TestVercelCronRunsTheJob(t *testing.T) {
	j := &job{result: map[string]int{"messages": 3}}

	resp := call(t, newHandler(t, j), http.MethodGet, "Bearer "+secret)

	if resp.Code != http.StatusOK || j.runs != 1 {
		t.Fatalf("status = %d, runs = %d", resp.Code, j.runs)
	}
	var body map[string]int
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil || body["messages"] != 3 {
		t.Errorf("body = %s", resp.Body.String())
	}
}

func TestCallsWithoutTheSecretDoNothing(t *testing.T) {
	for name, authorization := range map[string]string{
		"none":          "",
		"wrong":         "Bearer nope",
		"no scheme":     secret,
		"basic":         "Basic " + secret,
		"prefix":        "Bearer " + secret[:10],
		"secret + more": "Bearer " + secret + "x",
	} {
		t.Run(name, func(t *testing.T) {
			j := &job{}
			resp := call(t, newHandler(t, j), http.MethodGet, authorization)
			if resp.Code != http.StatusUnauthorized || j.runs != 0 {
				t.Errorf("status = %d, runs = %d; anyone could wipe history", resp.Code, j.runs)
			}
		})
	}
}

func TestOnlyGetIsAccepted(t *testing.T) {
	j := &job{}
	resp := call(t, newHandler(t, j), http.MethodPost, "Bearer "+secret)
	if resp.Code != http.StatusMethodNotAllowed || j.runs != 0 {
		t.Errorf("status = %d, runs = %d", resp.Code, j.runs)
	}
}

func TestJobFailureIsReportedWithoutDetails(t *testing.T) {
	j := &job{err: errors.New("pq: password authentication failed for user tgbot")}

	resp := call(t, newHandler(t, j), http.MethodGet, "Bearer "+secret)

	if resp.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 so Vercel shows the run as failed", resp.Code)
	}
	if body := resp.Body.String(); body == "" || containsAny(body, "password", "tgbot") {
		t.Errorf("body = %q; details belong in the log", body)
	}
}

func TestHandlerNeedsASecret(t *testing.T) {
	if _, err := NewHandler(Options{Run: (&job{}).run}); err == nil {
		t.Error("a handler without a secret was created")
	}
}

func containsAny(text string, parts ...string) bool {
	for _, part := range parts {
		for i := 0; i+len(part) <= len(text); i++ {
			if text[i:i+len(part)] == part {
				return true
			}
		}
	}
	return false
}
