package webapi

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func TestCacheEntriesExpire(t *testing.T) {
	clock := &clock{now: testNow}
	c := newCache(clock.Now)
	c.put("rate", 12011.65, time.Minute)

	if value, found := c.get("rate"); !found || value != 12011.65 {
		t.Errorf("get = %v, %v", value, found)
	}
	clock.now = clock.now.Add(time.Minute)
	if _, found := c.get("rate"); found {
		t.Error("an expired entry was returned")
	}
}

func TestCacheStaysSmall(t *testing.T) {
	clock := &clock{now: testNow}
	c := newCache(clock.Now)
	c.put("old", 1, time.Second)
	clock.now = clock.now.Add(time.Minute)
	for i := range maxCacheEntries + 50 {
		c.put(fmt.Sprint(i), i, time.Hour)
	}

	if len(c.entries) > maxCacheEntries {
		t.Errorf("cache holds %d entries, want at most %d", len(c.entries), maxCacheEntries)
	}
	if _, found := c.entries["old"]; found {
		t.Error("an expired entry survived pruning")
	}
}

func TestRememberCachesOnlySuccess(t *testing.T) {
	c := newCache(func() time.Time { return testNow })
	calls := 0
	failing := func() (string, error) { calls++; return "", errors.New("down") }
	working := func() (string, error) { calls++; return "up", nil }

	if _, err := remember(c, "k", time.Minute, failing); err == nil {
		t.Fatal("the error was lost")
	}
	for range 2 {
		if value, err := remember(c, "k", time.Minute, working); err != nil || value != "up" {
			t.Fatalf("remember = %q, %v", value, err)
		}
	}
	if calls != 2 {
		t.Errorf("fetched %d times, want 2 (the failure is not cached, the success is)", calls)
	}
}
