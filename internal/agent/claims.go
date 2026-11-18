package agent

import (
	"cmp"
	"encoding/json"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/ai"
)

// moderateTool is the tool whose refusals the model must not turn into
// success.
const moderateTool = "moderate"

// notDoneLine is said in place of a claim of success after moderate refused
// without a line of its own to say.
const notDoneLine = "Bajarilmadi: hech narsa o'zgarmadi."

// moderationRefusal is the line to say when every moderate call of the turn
// failed, the last refusal's said, and whether they all failed; false when
// the turn called moderate never or with success.
func moderationRefusal(messages []ai.Message) (said string, refused bool) {
	names := map[string]string{}
	for _, msg := range messages {
		for _, call := range msg.ToolCalls {
			names[call.ID] = call.Function.Name
		}
		if msg.Role != ai.RoleTool || names[msg.ToolCallID] != moderateTool {
			continue
		}
		var result struct {
			OK    bool `json:"ok"`
			Error struct {
				Said string `json:"said"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(msg.Content), &result) != nil {
			continue
		}
		if result.OK {
			return "", false
		}
		refused, said = true, cmp.Or(result.Error.Said, said)
	}
	return cmp.Or(said, notDoneLine), refused
}

// doneWords are what a reply claiming an action was carried out says, in
// lower case without apostrophes: "Bajarildi", "mutesiz", "готово", "done".
var doneWords = map[string]bool{
	"bajarildi": true, "bajardim": true, "qildim": true, "qilindi": true, "qoydim": true,
	"mutesiz": true, "mutedasiz": true, "mutelandi": true, "bloklandi": true, "blokladim": true,
	"ochirildi": true, "ochirdim": true, "chiqarildi": true, "chiqardim": true, "tayyor": true,
	"бажарилди": true, "бажардим": true, "қилдим": true, "килдим": true, "қилинди": true, "килинди": true,
	"блокланди": true, "ўчирилди": true, "учирилди": true, "ўчирдим": true, "учирдим": true,
	"чиқарилди": true, "чикарилди": true, "тайёр": true,
	"готово": true, "сделано": true, "сделала": true, "сделал": true, "выполнено": true,
	"забанен": true, "забанила": true, "замучен": true, "замутила": true, "удалено": true, "удалила": true,
	"заблокирован": true, "done": true, "muted": true, "banned": true, "deleted": true, "removed": true,
}

// claimsDone reports a reply that says something was done.
func claimsDone(reply string) bool {
	if strings.Contains(reply, "✅") {
		return true
	}
	folded := strings.NewReplacer("'", "", "ʻ", "", "ʼ", "", "’", "", "‘", "", "`", "").Replace(strings.ToLower(reply))
	for _, word := range strings.FieldsFunc(folded, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if doneWords[word] {
			return true
		}
	}
	return false
}
