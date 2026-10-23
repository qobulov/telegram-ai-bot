package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/tools"
)

const (
	wolframTTL      = 6 * time.Hour
	maxWolframQuery = 200
	// wolframChars is how much of Wolfram|Alpha's answer is asked for.
	wolframChars  = 2500
	wolframSource = "Wolfram|Alpha"
)

type wolframArgs struct {
	Query string `json:"query"`
}

func (a *apis) wolframTool() tools.Tool {
	return tools.Tool{
		Name: "wolfram",
		Description: "Wolfram|Alpha: solves and computes exactly: equations, integrals, limits, physics and chemistry (constants, formulas, properties of substances), statistics, units, dates. " +
			"Ask in short English, as \"integrate x^2 sin x\" or \"boiling point of ethanol\". Use it for any homework calculation.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {"query": {"type": "string", "description": "The question in short English."}},
			"required": ["query"],
			"additionalProperties": false
		}`),
		Timeout: patientTimeout,
		Run:     a.wolfram,
	}
}

func (a *apis) wolfram(ctx context.Context, raw json.RawMessage) (any, error) {
	var args wolframArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	query := strings.Join(strings.Fields(args.Query), " ")
	if query == "" || utf8.RuneCountInString(query) > maxWolframQuery {
		return nil, argumentError("query must be 1 to %d characters", maxWolframQuery)
	}
	link := a.urls.Wolfram + "/v1/llm-api?maxchars=" + strconv.Itoa(wolframChars) + "&input=" + url.QueryEscape(query)
	body, err := remember(a.cache, "wolfram:"+link, wolframTTL, func() ([]byte, error) {
		// The AppID goes in a header, so that it is in no URL a log shows.
		return a.patient.get(ctx, call{service: wolframSource, url: link, header: http.Header{"Authorization": {"Bearer " + a.wolframAppID}}})
	})
	var status *statusError
	if errors.As(err, &status) && status.status == http.StatusNotImplemented {
		return nil, argumentError("Wolfram|Alpha did not understand %q; rephrase it in short English, or answer yourself", query)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"answer": wolframText(string(body)), "source": wolframSource}, nil
}

// wolframText drops the lines of an answer the model cannot use: plot
// images and Wolfram Language code.
func wolframText(answer string) string {
	var lines []string
	for _, line := range strings.Split(answer, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "image:") || strings.HasPrefix(trimmed, "Wolfram Language code:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
