package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type timeResult struct {
	Time     string `json:"time"`
	Timezone string `json:"timezone"`
}

func runTime(t *testing.T, args string) (timeResult, envelope) {
	t.Helper()
	registry := mustRegistry(t, Builtin("")...)
	env := decodeEnvelope(t, registry.Execute(context.Background(), userID, "get_current_time", args))
	var result timeResult
	if env.OK {
		if err := json.Unmarshal(env.Result, &result); err != nil {
			t.Fatalf("decode result: %v", err)
		}
	}
	return result, env
}

func TestCurrentTimeDefaultsToUTC(t *testing.T) {
	result, env := runTime(t, `{}`)
	if !env.OK || result.Timezone != "UTC" {
		t.Fatalf("envelope = %+v, result = %+v", env, result)
	}
	parsed, err := time.Parse(time.RFC3339, result.Time)
	if err != nil {
		t.Fatalf("time %q is not RFC 3339: %v", result.Time, err)
	}
	if !strings.HasSuffix(result.Time, "Z") {
		t.Errorf("time %q is not in UTC", result.Time)
	}
	if drift := time.Since(parsed); drift < -time.Minute || drift > time.Minute {
		t.Errorf("time %q is not the current time", result.Time)
	}
}

func TestCurrentTimeAcceptsIANATimezone(t *testing.T) {
	result, env := runTime(t, `{"timezone":"Asia/Tashkent"}`)
	if !env.OK || result.Timezone != "Asia/Tashkent" {
		t.Fatalf("envelope = %+v, result = %+v", env, result)
	}
	if !strings.HasSuffix(result.Time, "+05:00") {
		t.Errorf("time %q does not carry the Tashkent offset", result.Time)
	}
}

func TestCurrentTimeRejectsBadTimezones(t *testing.T) {
	for _, args := range []string{
		`{"timezone":"Mars/Olympus"}`,
		`{"timezone":"Local"}`,
		`{"timezone":"../../etc/passwd"}`,
		`{"timezone":5}`,
		`{"timezone":"UTC","extra":1}`,
	} {
		if _, env := runTime(t, args); env.OK || env.Error.Code != "invalid_arguments" {
			t.Errorf("args %s: envelope = %+v, want invalid_arguments", args, env)
		}
	}
}
