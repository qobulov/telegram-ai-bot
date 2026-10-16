package webapi

import (
	"sync"
	"time"
)

// maxCacheEntries keeps the cache small; a warm instance serves a few
// groups, not a city.
const maxCacheEntries = 500

// cache keeps successful answers for a while in this instance's memory.
// Instances do not share it, which is fine for a few groups: it exists to
// spare the free APIs repeated questions within minutes. It is safe for
// concurrent use.
type cache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]cacheEntry
}

type cacheEntry struct {
	value   any
	expires time.Time
}

func newCache(now func() time.Time) *cache {
	return &cache{now: now, entries: make(map[string]cacheEntry)}
}

func (c *cache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, found := c.entries[key]
	if !found || !c.now().Before(entry.expires) {
		return nil, false
	}
	return entry.value, true
}

func (c *cache) put(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxCacheEntries {
		c.prune()
	}
	c.entries[key] = cacheEntry{value: value, expires: c.now().Add(ttl)}
}

// prune drops expired entries and, if that is not enough, arbitrary ones.
// The caller holds the lock.
func (c *cache) prune() {
	now := c.now()
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
		}
	}
	for key := range c.entries {
		if len(c.entries) < maxCacheEntries {
			return
		}
		delete(c.entries, key)
	}
}

// remember returns the cached value for key, or calls fetch and caches its
// answer if it succeeds. Cached values are shared: callers must not modify
// them.
func remember[T any](c *cache, key string, ttl time.Duration, fetch func() (T, error)) (T, error) {
	if value, found := c.get(key); found {
		if typed, isT := value.(T); isT {
			return typed, nil
		}
	}
	value, err := fetch()
	if err != nil {
		return value, err
	}
	c.put(key, value, ttl)
	return value, nil
}
