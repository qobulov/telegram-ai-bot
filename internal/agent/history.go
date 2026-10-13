package agent

import "telegram-ai-bot/internal/ai"

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
			clean = append(clean, results...)
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
