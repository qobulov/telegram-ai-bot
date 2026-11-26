package webapi

import (
	"context"
	"encoding/json"

	"telegram-ai-bot/internal/tools"
)

// party_game gives a question for Truth or Dare and Would You Rather, from
// the Truth or Dare Bot's API, as the owners asked on 2026-11-26. Only its
// PG questions are asked for: its PG-13 ones are about sex and exes, unfit
// for groups of teenagers.

const (
	partyService = "the Truth or Dare API"
	// partyRating keeps the questions fit for anyone.
	partyRating = "pg"
	maxQuestion = 300

	partyNote = "Put the question in the chat's language in your own words, short, addressed to the player: the asker, or the member they replied to or tagged; " +
		"to pick someone at random, take one from group_members who wrote lately. Anyone may skip with a joke. "
	dareNote = "A dare must be done in this chat and harm no one: never one that has them share a private photo, a number or an address, contact or block anyone, spend money or leave; " +
		"if this one is like that, give your own harmless dare of the same kind instead."
	truthNote = "If the question is too personal for a group, ask a lighter one of your own instead."
	wyrNote   = "Post it with create_poll as a two-option poll, the options in the chat's language, then joke about the result when asked."
)

var partyKinds = map[string]string{"truth": "truth", "dare": "dare", "would_you_rather": "wyr"}

type partyArgs struct {
	Kind string `json:"kind"`
}

func (a *apis) partyTool() tools.Tool {
	return tools.Tool{
		Name: "party_game",
		Description: "Truth or Dare and Would You Rather for the group (\"haqiqat\", \"jur'at\", \"truth or dare o'ynaymiz\", \"nimani tanlarding\", \"правда или действие\"): " +
			"a fresh question of the kind asked. Call it every time; never ask one from memory.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"kind": {"type": "string", "enum": ["truth", "dare", "would_you_rather"]}
			},
			"required": ["kind"],
			"additionalProperties": false
		}`),
		Run: a.party,
	}
}

func (a *apis) party(ctx context.Context, raw json.RawMessage) (any, error) {
	var args partyArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	path, ok := partyKinds[args.Kind]
	if !ok {
		return nil, argumentError("kind must be truth, dare or would_you_rather")
	}
	var answer struct {
		Question string `json:"question"`
	}
	link := a.urls.TruthOrDare + "/v1/" + path + "?rating=" + partyRating
	if err := a.client.getJSON(ctx, call{service: partyService, url: link}, &answer); err != nil {
		return nil, err
	}
	question := cleanText(answer.Question, maxQuestion)
	if question == "" {
		return nil, argumentError("%s gave no question just now; ask one of your own of the same kind", partyService)
	}
	note := partyNote + map[string]string{"truth": truthNote, "dare": dareNote, "wyr": wyrNote}[path]
	return map[string]any{"kind": args.Kind, "question": question, "language": "English", "note": note}, nil
}
