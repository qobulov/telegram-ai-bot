package ai

import (
	"fmt"
	"testing"
)

// Spent credits (402) and a quota named in a 429 are the account's limit;
// a plain 429 or 503 is the provider being busy, and other errors neither.
func TestIsOutOfCredits(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&APIError{Status: 402, Message: "Your prepayment credits are depleted."}, true},
		{fmt.Errorf("turn: %w", &APIError{Status: 402}), true},
		{&APIError{Status: 429, Message: "You exceeded your current quota, please check your plan."}, true},
		{&APIError{Status: 429, Message: "RESOURCE_EXHAUSTED"}, true},
		{&APIError{Status: 429, Message: "Too many requests"}, false},
		{&APIError{Status: 503, Message: "overloaded"}, false},
		{&APIError{Status: 500, Message: "quota"}, false},
		{fmt.Errorf("network down"), false},
	}
	for _, c := range cases {
		if got := IsOutOfCredits(c.err); got != c.want {
			t.Errorf("IsOutOfCredits(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
