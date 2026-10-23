package webapi

import (
	"strings"
	"testing"
)

func TestWolframAnswersWithTheAppIDInAHeader(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/wolfram/v1/llm-api":                  ok("Query:\n\"integrate x^2 sin x\"\n\nIndefinite integral:\nintegral x^2 sin(x) dx = 2 x sin(x) - (x^2 - 2) cos(x) + constant\n\nPlot:\nimage: https://public6.wolframalpha.com/files/x.png\nWolfram Language code: Plot[x]\n"),
		"/wolfram/v1/llm-api?input=qwzx blarg": {status: 501, body: "Wolfram|Alpha did not understand your input"},
	})
	opts := u.options()
	opts.WolframAppID = "APP-ID"

	env := run(t, opts, "wolfram", `{"query":"integrate  x^2 sin x"}`)

	if !env.OK || !strings.Contains(env.raw, "2 x sin(x) - (x^2 - 2) cos(x)") || strings.Contains(env.raw, "image:") || strings.Contains(env.raw, "Wolfram Language code") {
		t.Fatalf("env = %s", env.raw)
	}
	r := u.requests("/wolfram/v1/llm-api")[0]
	if r.Header.Get("Authorization") != "Bearer APP-ID" || strings.Contains(r.URL.String(), "APP-ID") || r.URL.Query().Get("input") != "integrate x^2 sin x" {
		t.Errorf("request %s, header %q", r.URL, r.Header.Get("Authorization"))
	}
	wantError(t, run(t, opts, "wolfram", `{"query":"qwzx blarg"}`), "invalid_arguments", "rephrase")
}

func TestWolframNeedsItsAppID(t *testing.T) {
	if strings.Contains(toolNames(Tools(newUpstream(t, nil).options())), "wolfram") {
		t.Error("the wolfram tool is there without an AppID")
	}
}
