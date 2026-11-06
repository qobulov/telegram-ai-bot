package agent

import (
	"unicode/utf8"

	"telegram-ai-bot/internal/ai"
)

// maxHistoryResultRunes bounds a tool's result as the history keeps it. The
// turn that called the tool saw it whole, and a later turn mostly needs what
// the answer made of it; a few results, such as a member list or the bot's
// data, run to 15,000 characters and would fill every prompt after them.
const maxHistoryResultRunes = 1500

// cutResultNote ends a result cut for the history.
const cutResultNote = "… [cut in the history; call the tool again for the rest]"

// sanitize makes stored history safe to send to the model.
//
// History is loaded as "the last N messages", and that cut can land in the
// middle of a tool exchange. Chat APIs reject a conversation that starts with
// an assistant or tool message, or that contains a tool call without all of
// its results. So sanitize:
//   - drops everything before the first user message,
//   - drops an assistant tool-call message, and its results, unless every
//     call is answered by the tool messages directly after it,
//   - drops tool messages that answer nothing.
//
// It also cuts a long tool result to maxHistoryResultRunes, the same way
// every time, so that the history still begins the same way and stays
// cached.
func sanitize(history []ai.Message) []ai.Message {
	var clean []ai.Message
	for i := 0; i < len(history); {
		msg := history[i]
		results := followingToolMessages(history[i+1:])
		switch {
		case msg.Role == ai.RoleUser:
			clean = append(clean, msg)
		case msg.Role != ai.RoleAssistant || len(clean) == 0:
			// An orphan tool result, or a reply whose question was trimmed away.
		case len(msg.ToolCalls) == 0:
			clean = append(clean, msg)
		case answersAll(msg.ToolCalls, results):
			clean = append(clean, msg)
			for _, result := range results {
				clean = append(clean, cutResult(result))
			}
		}
		i++
		if msg.Role == ai.RoleAssistant {
			i += len(results)
		}
	}
	return clean
}

// followingToolMessages returns the run of tool messages at the start of rest.
func followingToolMessages(rest []ai.Message) []ai.Message {
	end := 0
	for end < len(rest) && rest[end].Role == ai.RoleTool {
		end++
	}
	return rest[:end]
}

// answersAll reports whether results holds exactly one result per call.
func answersAll(calls []ai.ToolCall, results []ai.Message) bool {
	if len(calls) != len(results) {
		return false
	}
	answered := make(map[string]bool, len(results))
	for _, result := range results {
		answered[result.ToolCallID] = true
	}
	for _, call := range calls {
		if !answered[call.ID] {
			return false
		}
	}
	return true
}

// cutResult is a tool result as the history keeps it: at most
// maxHistoryResultRunes, and a note when it was cut.
func cutResult(msg ai.Message) ai.Message {
	if utf8.RuneCountInString(msg.Content) <= maxHistoryResultRunes {
		return msg
	}
	msg.Content = string([]rune(msg.Content)[:maxHistoryResultRunes]) + cutResultNote
	return msg
}
