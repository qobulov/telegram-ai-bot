package service

import (
	"strings"
	"testing"
)

// /ping and /id are answered in code.
func TestUtilityCommands(t *testing.T) {
	f := newFixture()
	sent := f.run(from(20, "/id", 0), from(20, "/id", 30), from(20, "/ping", 0))
	texts := make([]string, len(sent))
	for i, m := range sent {
		texts[i] = m.text
	}
	all := strings.Join(texts, "\n")
	for _, want := range []string{"Sizning ID: `20`", "Chat ID", "Sam: `30`", "Pong"} {
		if !strings.Contains(all, want) {
			t.Errorf("answers lack %q:\n%s", want, all)
		}
	}
}
