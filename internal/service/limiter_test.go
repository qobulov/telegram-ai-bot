package service

import (
	"testing"
	"time"
)

func TestLimiterAllowsBurstThenDenies(t *testing.T) {
	l := newLimiter(3)
	now := time.Unix(1000, 0)

	for i := range 3 {
		if got := l.check(1, now); got != allowed {
			t.Fatalf("message %d: decision = %v, want allowed", i+1, got)
		}
	}
	if got := l.check(1, now); got != deniedFirst {
		t.Errorf("4th message: decision = %v, want deniedFirst", got)
	}
	if got := l.check(1, now); got != deniedAgain {
		t.Errorf("5th message: decision = %v, want deniedAgain", got)
	}
}

func TestLimiterRefillsOverTime(t *testing.T) {
	l := newLimiter(3) // one token every 20 seconds
	now := time.Unix(1000, 0)
	for range 4 {
		l.check(1, now)
	}

	if got := l.check(1, now.Add(10*time.Second)); got == allowed {
		t.Error("allowed before a token was refilled")
	}
	if got := l.check(1, now.Add(21*time.Second)); got != allowed {
		t.Errorf("decision after refill = %v, want allowed", got)
	}
	if got := l.check(1, now.Add(21*time.Second)); got != deniedFirst {
		t.Errorf("decision after using the refilled token = %v, want deniedFirst (warn again)", got)
	}
}

func TestLimiterNeverExceedsCapacity(t *testing.T) {
	l := newLimiter(2)
	now := time.Unix(1000, 0)
	l.check(1, now)
	later := now.Add(time.Hour)

	results := []decision{l.check(1, later), l.check(1, later), l.check(1, later)}
	if results[0] != allowed || results[1] != allowed || results[2] != deniedFirst {
		t.Errorf("decisions after a long idle period = %v", results)
	}
}

func TestLimiterTracksUsersSeparately(t *testing.T) {
	l := newLimiter(1)
	now := time.Unix(1000, 0)
	l.check(1, now)

	if got := l.check(2, now); got != allowed {
		t.Errorf("second user: decision = %v, want allowed", got)
	}
}

func TestLimiterPrunesIdleUsers(t *testing.T) {
	l := newLimiter(1)
	now := time.Unix(1000, 0)
	l.check(1, now)
	l.check(2, now.Add(5*time.Minute))

	l.prune(now.Add(5 * time.Minute))

	if _, tracked := l.buckets[1]; tracked {
		t.Error("idle user was not pruned")
	}
	if _, tracked := l.buckets[2]; !tracked {
		t.Error("active user was pruned")
	}
}
