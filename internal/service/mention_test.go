package service

import "testing"

// A member is tagged by ID, their name stripped of the @username the log
// adds and of what would break the tag; one with no name gets 👤.
func TestMentionMember(t *testing.T) {
	if got := mentionMember(9, "[Bo]b (x) @bob"); got != "[Bob x](tg://user?id=9)" {
		t.Errorf("named = %q", got)
	}
	if got := mentionMember(9, ""); got != "[👤](tg://user?id=9)" {
		t.Errorf("no name = %q", got)
	}
}
