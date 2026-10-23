package service

import (
	"sync"
	"time"
)

// Cached answers from the database, so that each message of a busy group
// does not ask the same thing again: the database bills every query. Each
// serverless instance keeps its own, and a new one asks afresh.
const (
	// muteMemory is how long a group's mute state is trusted for deciding
	// whether chatter is logged; whether the bot speaks is always asked.
	muteMemory = 30 * time.Second
	// memberMemory is how long a member already recorded, under the same
	// name, is not recorded again; their last_seen_at may lag by as much.
	memberMemory = 6 * time.Hour
	// subscriberMemory is how long a user found in the required channel is
	// not asked about again; one who left is answered that long at most.
	subscriberMemory = 10 * time.Minute
	// maxRemembered bounds each cache; past it, the expired are dropped, and
	// if none is, everything.
	maxRemembered = 10_000
)

// recently remembers values for ttl. It is safe for concurrent use.
type recently[K comparable, V any] struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	items map[K]remembered[V]
}

type remembered[V any] struct {
	value V
	until time.Time
}

func newRecently[K comparable, V any](ttl time.Duration, now func() time.Time) *recently[K, V] {
	return &recently[K, V]{ttl: ttl, now: now, items: map[K]remembered[V]{}}
}

func (r *recently[K, V]) get(key K) (V, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[key]
	if !ok || r.now().After(item.until) {
		var zero V
		return zero, false
	}
	return item.value, true
}

func (r *recently[K, V]) set(key K, value V) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if len(r.items) >= maxRemembered {
		for k, item := range r.items {
			if now.After(item.until) {
				delete(r.items, k)
			}
		}
		if len(r.items) >= maxRemembered {
			clear(r.items)
		}
	}
	r.items[key] = remembered[V]{value: value, until: now.Add(r.ttl)}
}

// memberKey is a member of a group.
type memberKey struct{ chatID, userID int64 }

// memberName is how a member was last recorded.
type memberName struct{ firstName, username string }
