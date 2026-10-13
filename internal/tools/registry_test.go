package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const adminID, userID = int64(1), int64(2)

func isAdmin(id int64) bool { return id == adminID }

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeEnvelope(t *testing.T, output string) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("tool output is not JSON: %q", output)
	}
	return env
}

func echoTool() Tool {
	return Tool{
		Name:        "echo",
		Description: "returns its arguments",
		Schema:      json.RawMessage(`{"type":"object"}`),
		Run: func(_ context.Context, args json.RawMessage) (any, error) {
			return json.RawMessage(args), nil
		},
	}
}

func mustRegistry(t *testing.T, tools ...Tool) *Registry {
	t.Helper()
	registry, err := NewRegistry(isAdmin, tools...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

func expectError(t *testing.T, output, code string) {
	t.Helper()
	env := decodeEnvelope(t, output)
	if env.OK || env.Error.Code != code {
		t.Errorf("output = %s, want error code %q", output, code)
	}
	if env.Error.Message == "" {
		t.Errorf("output = %s, want an error message", output)
	}
}

func TestExecuteRunsRegisteredTool(t *testing.T) {
	registry := mustRegistry(t, echoTool())

	env := decodeEnvelope(t, registry.Execute(context.Background(), userID, "echo", `{"a":1}`))
	if !env.OK || string(env.Result) != `{"a":1}` {
		t.Errorf("envelope = %+v", env)
	}
}

func TestExecuteRejectsUnknownTool(t *testing.T) {
	registry := mustRegistry(t, echoTool())

	expectError(t, registry.Execute(context.Background(), userID, "run_shell", `{}`), "unknown_tool")
}

func TestExecuteTreatsEmptyArgumentsAsEmptyObject(t *testing.T) {
	registry := mustRegistry(t, echoTool())

	for _, args := range []string{"", "  "} {
		env := decodeEnvelope(t, registry.Execute(context.Background(), userID, "echo", args))
		if !env.OK || string(env.Result) != `{}` {
			t.Errorf("args %q: envelope = %+v", args, env)
		}
	}
}

func TestExecuteRejectsMalformedArguments(t *testing.T) {
	registry := mustRegistry(t, echoTool())

	for _, args := range []string{`{bad`, `"just a string"`, `[1,2]`, `null`} {
		expectError(t, registry.Execute(context.Background(), userID, "echo", args), "invalid_arguments")
	}
}

func TestAdminOnlyToolIsGatedByCallerID(t *testing.T) {
	secret := echoTool()
	secret.Name = "admin_echo"
	secret.AdminOnly = true
	registry := mustRegistry(t, echoTool(), secret)

	expectError(t, registry.Execute(context.Background(), userID, "admin_echo", `{}`), "forbidden")
	if env := decodeEnvelope(t, registry.Execute(context.Background(), adminID, "admin_echo", `{}`)); !env.OK {
		t.Errorf("admin was refused: %+v", env)
	}

	if defs := registry.Definitions(userID); len(defs) != 1 || defs[0].Function.Name != "echo" {
		t.Errorf("user definitions = %+v", defs)
	}
	if defs := registry.Definitions(adminID); len(defs) != 2 {
		t.Errorf("admin definitions = %+v", defs)
	}
}

func TestDefinitionsDescribeTools(t *testing.T) {
	registry := mustRegistry(t, echoTool())

	def := registry.Definitions(userID)[0]
	if def.Type != "function" || def.Function.Description != "returns its arguments" || string(def.Function.Parameters) != `{"type":"object"}` {
		t.Errorf("definition = %+v", def)
	}
}

func TestExecuteEnforcesTimeout(t *testing.T) {
	slow := Tool{
		Name:    "slow",
		Schema:  json.RawMessage(`{"type":"object"}`),
		Timeout: 20 * time.Millisecond,
		Run: func(context.Context, json.RawMessage) (any, error) {
			time.Sleep(2 * time.Second) // ignores cancellation on purpose
			return "late", nil
		},
	}
	registry := mustRegistry(t, slow)

	start := time.Now()
	output := registry.Execute(context.Background(), userID, "slow", `{}`)
	expectError(t, output, "timeout")
	if time.Since(start) > time.Second {
		t.Errorf("Execute took %v, want about 20ms", time.Since(start))
	}
}

func TestExecuteLimitsOutputSize(t *testing.T) {
	big := Tool{
		Name:   "big",
		Schema: json.RawMessage(`{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (any, error) {
			return strings.Repeat("x", 5000), nil
		},
	}
	registry := mustRegistry(t, big)

	expectError(t, registry.Execute(context.Background(), userID, "big", `{}`), "output_too_large")
}

func TestExecuteSurvivesPanickingTool(t *testing.T) {
	broken := Tool{
		Name:   "broken",
		Schema: json.RawMessage(`{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (any, error) {
			panic("boom")
		},
	}
	registry := mustRegistry(t, broken)

	expectError(t, registry.Execute(context.Background(), userID, "broken", `{}`), "execution_failed")
}

func TestNewRegistryRejectsBadTools(t *testing.T) {
	if _, err := NewRegistry(isAdmin, echoTool(), echoTool()); err == nil {
		t.Error("duplicate tool names accepted")
	}
	nameless := echoTool()
	nameless.Name = ""
	if _, err := NewRegistry(isAdmin, nameless); err == nil {
		t.Error("tool without a name accepted")
	}
	noRun := echoTool()
	noRun.Run = nil
	if _, err := NewRegistry(isAdmin, noRun); err == nil {
		t.Error("tool without Run accepted")
	}
}

func TestBuiltinToolsRegisterAndValidate(t *testing.T) {
	registry := mustRegistry(t, Builtin("help text")...)
	ctx := context.Background()

	names := map[string]bool{}
	for _, def := range registry.Definitions(userID) {
		names[def.Function.Name] = true
		if !json.Valid(def.Function.Parameters) {
			t.Errorf("%s has an invalid schema", def.Function.Name)
		}
	}
	for _, want := range []string{"get_current_time", "calculator", "get_bot_help"} {
		if !names[want] {
			t.Errorf("missing builtin tool %q", want)
		}
	}

	env := decodeEnvelope(t, registry.Execute(ctx, userID, "calculator", `{"expression":"2*(3+4)"}`))
	if !env.OK || !strings.Contains(string(env.Result), `"result":14`) {
		t.Errorf("calculator = %+v (%s)", env, env.Result)
	}
	expectError(t, registry.Execute(ctx, userID, "calculator", `{"expression":"1/0"}`), "invalid_arguments")
	expectError(t, registry.Execute(ctx, userID, "calculator", `{}`), "invalid_arguments")
	expectError(t, registry.Execute(ctx, userID, "calculator", `{"expression":"1","code":"rm -rf"}`), "invalid_arguments")

	env = decodeEnvelope(t, registry.Execute(ctx, userID, "get_bot_help", ``))
	if !env.OK || !strings.Contains(string(env.Result), "help text") {
		t.Errorf("get_bot_help = %+v (%s)", env, env.Result)
	}
}
