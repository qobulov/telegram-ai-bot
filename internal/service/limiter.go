package service

import (
	"sync"
	"time"
)

// decision is the outcome of a rate-limit check.
type decision int

const (
	allowed decision = iota
	// deniedFirst is the first refusal since the user was last allowed; the
	// user should be told. Later refusals are deniedAgain and get no reply,
	// so that flooding the bot cannot make it flood Telegram in return.
	deniedFirst
	deniedAgain
)

const (
	limiterIdleAfter  = 2 * time.Minute
	limiterPruneEvery = 1000
)

// limiter is a per-user token bucket: each user may send perMinute messages
// in a burst, and regains them evenly over a minute.
type limiter struct {
	mu        sync.Mutex
	perMinute float64
	buckets   map[int64]*bucket
	checks    int
}

type bucket struct {
	tokens float64
	last   time.Time
	warned bool
}

func newLimiter(perMinute int) *limiter {
	return &limiter{perMinute: float64(perMinute), buckets: map[int64]*bucket{}}
}

func (l *limiter) check(userID int64, now time.Time) decision {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.checks++; l.checks%limiterPruneEvery == 0 {
		l.pruneLocked(now)
	}
	b, ok := l.buckets[userID]
	if !ok {
		b = &bucket{tokens: l.perMinute, last: now}
		l.buckets[userID] = b
	}
	l.refill(b, now)
	return b.take()
}

func (l *limiter) refill(b *bucket, now time.Time) {
	elapsed := now.Sub(b.last).Minutes()
	b.tokens = min(l.perMinute, b.tokens+elapsed*l.perMinute)
	b.last = now
}

func (b *bucket) take() decision {
	if b.tokens >= 1 {
		b.tokens--
		b.warned = false
		return allowed
	}
	if b.warned {
		return deniedAgain
	}
	b.warned = true
	return deniedFirst
}

// refund gives back the token the last allowed check took.
func (l *limiter) refund(userID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, ok := l.buckets[userID]; ok {
		b.tokens = min(l.perMinute, b.tokens+1)
	}
}

// prune forgets users who have been idle long enough for their bucket to be
// full again, so the map does not grow without bound.
func (l *limiter) prune(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
}

func (l *limiter) pruneLocked(now time.Time) {
	for userID, b := range l.buckets {
		if now.Sub(b.last) > limiterIdleAfter {
			delete(l.buckets, userID)
		}
	}
}
