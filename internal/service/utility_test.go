package service

import (
	"strings"
	"testing"
)

func TestCalc(t *testing.T) {
	for expr, want := range map[string]string{
		"2+2": "4", "(12+8)*3/4": "15", "2^10": "1 024", "10 % 3": "1", "-3*-3": "9", "1,5 x 2": "3", "7/2": "3.5", "2^3^2": "512",
	} {
		v, err := calc(expr)
		if err != nil || formatNumber(v) != want {
			t.Errorf("calc(%q) = %v %v, want %s", expr, formatNumber(v), err, want)
		}
	}
	for _, bad := range []string{"2+", "abc", "(1+2", "1/0", "rm -rf"} {
		if _, err := calc(bad); err == nil {
			t.Errorf("calc(%q) took", bad)
		}
	}
}

// /ping, /id and /calc are answered in code; /weather goes to the model as
// a question about the weather.
func TestUtilityCommands(t *testing.T) {
	f := newFixture()
	sent := f.run(from(20, "/calc 2*21", 0), from(20, "/id", 0), from(20, "/id", 30), from(20, "/ping", 0))
	texts := make([]string, len(sent))
	for i, m := range sent {
		texts[i] = m.text
	}
	all := strings.Join(texts, "\n")
	for _, want := range []string{"= 42", "Sizning ID: `20`", "Chat ID", "Sam: `30`", "Pong"} {
		if !strings.Contains(all, want) {
			t.Errorf("answers lack %q:\n%s", want, all)
		}
	}
	if q, ok := weatherQuestion("/weather Samarqand"); !ok || q != "Samarqand ob-havosi qanday?" {
		t.Errorf("weather = %q %v", q, ok)
	}
}
