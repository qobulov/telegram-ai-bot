package tools

import (
	"math"
	"strings"
	"testing"
)

func TestEvaluate(t *testing.T) {
	cases := map[string]float64{
		"1+2*3":           7,
		"(1+2)*3":         9,
		"-4+10":           6,
		"2*-3":            -6,
		"10/4":            2.5,
		"1.5+1.5":         3,
		" 2 * ( 3 + 4 ) ": 14,
		"10-2-3":          5,
		"100/10/2":        5,
		"-(2+3)":          -5,
		"+5":              5,
		".5*4":            2,
		"((((7))))":       7,
	}
	for expression, want := range cases {
		t.Run(expression, func(t *testing.T) {
			got, err := Evaluate(expression)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if math.Abs(got-want) > 1e-9 {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestEvaluateRejectsInvalidInput(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"blank":             "   ",
		"dangling operator": "2 +",
		"unclosed paren":    "(1",
		"stray paren":       "1)",
		"power operator":    "2 ** 3",
		"letters":           "abc",
		"function call":     "sqrt(4)",
		"exponent overflow": "1e999",
		"two numbers":       "1 2",
		"double dot":        "1..2",
		"lone dot":          ".",
		"shell injection":   "1; rm -rf /",
		"deep nesting":      strings.Repeat("(", 200) + "1" + strings.Repeat(")", 200),
		"too long":          strings.Repeat("1+", 250) + "1",
		"deep unary":        strings.Repeat("-", 100) + "1",
	}
	for name, expression := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := Evaluate(expression); err == nil {
				t.Errorf("Evaluate(%q) = %v, want error", expression, got)
			}
		})
	}
}

func TestEvaluateRejectsDivisionByZero(t *testing.T) {
	for _, expression := range []string{"1/0", "1/(2-2)", "0/0", "5/0.0"} {
		if _, err := Evaluate(expression); err != ErrDivisionByZero {
			t.Errorf("Evaluate(%q) err = %v, want ErrDivisionByZero", expression, err)
		}
	}
}

func TestEvaluateRejectsOverflow(t *testing.T) {
	huge := strings.Repeat("9", 150)
	if got, err := Evaluate(huge + "*" + huge + "*" + huge); err == nil {
		t.Errorf("got %v, want overflow error", got)
	}
}
