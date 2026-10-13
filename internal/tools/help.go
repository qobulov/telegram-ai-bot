package tools

import (
	"context"
	"encoding/json"
)

// Builtin returns the tools shipped with the bot. helpText is what
// get_bot_help reports; the caller owns its wording.
func Builtin(helpText string) []Tool {
	return []Tool{
		currentTimeTool(),
		calculatorTool(),
		helpTool(helpText),
	}
}

type helpOutput struct {
	Help string `json:"help"`
}

func helpTool(helpText string) Tool {
	return Tool{
		Name:        "get_bot_help",
		Description: "Get the list of this bot's commands and capabilities. Use it when the user asks what the bot can do.",
		Schema:      json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
		Run: func(_ context.Context, raw json.RawMessage) (any, error) {
			var args struct{}
			if err := decodeArgs(raw, &args); err != nil {
				return nil, err
			}
			return helpOutput{Help: helpText}, nil
		},
	}
}
