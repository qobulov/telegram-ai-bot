package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"telegram-ai-bot/internal/tools"
)

// fakePins records the pins it is asked to send.
type fakePins struct {
	sent []Pin
	err  error
}

func (f *fakePins) SendPin(_ context.Context, pin Pin) error {
	f.sent = append(f.sent, pin)
	return f.err
}

// locate runs send_location for caller through a registry, as the agent
// does.
func locate(t *testing.T, opts Options, caller tools.Caller, args string) envelope {
	t.Helper()
	ctx := tools.WithTurn(context.Background(), caller, &tools.Sink{})
	output := newRegistry(t, opts).Execute(ctx, caller.UserID, "send_location", args)
	var env envelope
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("tool output is not JSON: %q", output)
	}
	env.raw = output
	return env
}

// A place in the tables is sent as a pin under its name, to the turn's
// chat: as a reply in a group, which keeps it in a forum topic, and as no
// reply in a private chat. The geocoder is not asked.
func TestSendLocationPinsThePlace(t *testing.T) {
	u := newUpstream(t, weatherRoutes(t))
	pins := &fakePins{}
	opts := u.options()
	opts.Pins = pins

	got := result[struct {
		Sent  bool   `json:"sent"`
		Place string `json:"place"`
	}](t, locate(t, opts, tools.Caller{UserID: 5, ChatID: -100123, Human: true, MessageID: 60}, `{"place":"Samarkand"}`))
	result[struct{}](t, locate(t, opts, tools.Caller{UserID: 5, ChatID: 5, Human: true, MessageID: 61}, `{"place":"Paris"}`))

	if !got.Sent || got.Place != "Samarqand" || len(pins.sent) != 2 {
		t.Fatalf("result = %+v, pins = %+v", got, pins.sent)
	}
	group, private := pins.sent[0], pins.sent[1]
	if group.ChatID != -100123 || group.ReplyToMessageID != 60 || group.Title != "Samarqand" || group.Lat != 39.65456 || group.Lon != 66.96445 || group.Address != "39.6546, 66.9645" {
		t.Errorf("group pin = %+v", group)
	}
	if private.ChatID != 5 || private.ReplyToMessageID != 0 || private.Title != "Parij" {
		t.Errorf("private pin = %+v", private)
	}
	if calls := u.requests(geocodingPath); len(calls) != 0 {
		t.Errorf("the geocoder was asked %d times for places in the tables", len(calls))
	}
}

// A place nobody knows, a country, or a missing name is refused with the
// reason, and nothing is sent; a pin Telegram refuses is reported without
// its details.
func TestSendLocationRefusals(t *testing.T) {
	u := newUpstream(t, weatherRoutes(t))
	pins := &fakePins{}
	opts := u.options()
	opts.Pins = pins
	caller := tools.Caller{UserID: 5, ChatID: -100123, Human: true, MessageID: 60}

	wantError(t, locate(t, opts, caller, `{"place":"Qwertyabad"}`), "invalid_arguments", "Qwertyabad")
	wantError(t, locate(t, opts, caller, `{"place":"Fransiya"}`), "invalid_arguments", "country")
	wantError(t, locate(t, opts, caller, `{"place":"  "}`), "invalid_arguments")
	if len(pins.sent) != 0 {
		t.Errorf("pins = %+v, want none", pins.sent)
	}

	pins.err = errors.New("telegram: 400 Bad Request: chat not found")
	env := locate(t, opts, caller, `{"place":"Xiva"}`)
	if env.OK || env.Error.Message != "the pin could not be sent; say so, and name the place in words" {
		t.Errorf("refused pin = %s", env.raw)
	}
}

// Without a way to send pins there is no tool.
func TestSendLocationNeedsPins(t *testing.T) {
	opts := newUpstream(t, nil).options()
	for _, tool := range Tools(opts) {
		if tool.Name == "send_location" {
			t.Fatal("send_location offered without Pins")
		}
	}
	opts.Pins = &fakePins{}
	if names := toolNames(Tools(opts)); names[len(names)-len("send_location"):] != "send_location" {
		t.Errorf("tools = %s, want send_location last", names)
	}
}
