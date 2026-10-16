package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type timeArgs struct {
	Timezone string `json:"timezone"`
}

type timeOutput struct {
	Time     string `json:"time"`
	Timezone string `json:"timezone"`
	Weekday  string `json:"weekday"`
}

// currentTimeTool answers in location when no timezone is asked for: people
// asking "what time is it?" in a group mean the group's local time, and UTC
// answered them wrong. A nil location means UTC.
func currentTimeTool(location *time.Location) Tool {
	if location == nil {
		location = time.UTC
	}
	return Tool{
		Name:        "get_current_time",
		Description: "Get the current date and time. Returns the group's local time unless an IANA timezone name is given.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"timezone": {"type": "string", "description": "IANA timezone name, for example Asia/Tashkent or Europe/Moscow. Omit for the group's local time."}
			},
			"additionalProperties": false
		}`),
		Run: func(_ context.Context, raw json.RawMessage) (any, error) {
			return runCurrentTime(raw, location)
		},
	}
}

func runCurrentTime(raw json.RawMessage, fallback *time.Location) (any, error) {
	var args timeArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	location, err := loadLocation(args.Timezone, fallback)
	if err != nil {
		return nil, err
	}
	now := time.Now().In(location)
	return timeOutput{
		Time:     now.Format(time.RFC3339),
		Timezone: location.String(),
		Weekday:  now.Weekday().String(),
	}, nil
}

// loadLocation resolves an IANA timezone name, or gives fallback when none
// is named. "Local" is refused because it would expose the server's own
// timezone rather than a zone the user named.
func loadLocation(name string, fallback *time.Location) (*time.Location, error) {
	if name == "" {
		return fallback, nil
	}
	if name == "Local" {
		return nil, unknownTimezone(name)
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, unknownTimezone(name)
	}
	return location, nil
}

func unknownTimezone(name string) error {
	return &ArgumentError{Message: fmt.Sprintf("%q is not a known IANA timezone; use a name such as Asia/Tashkent", name)}
}
