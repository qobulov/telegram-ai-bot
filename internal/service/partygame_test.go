package service

import (
	"strings"
	"testing"
)

// A party game's command, bare in a group, goes to the model as the words it
// stands for.
func TestPartyCommandsAskTheModel(t *testing.T) {
	for command, want := range map[string]string{"/haqiqat": "haqiqat ber", "/jurat": "jur'at ber", "/tanla": "would you rather", "/tod@MyBot": "truth or dare"} {
		f := newFixture()
		f.run(inGroup(groupMessage(command)))
		if req := onlyRequest(t, f).req; !strings.Contains(req.Text, want) {
			t.Errorf("%s asked %q, want it to say %q", command, req.Text, want)
		}
	}
}
