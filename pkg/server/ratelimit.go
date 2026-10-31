package server

import (
	"sync"
	"time"
)

// Limiter is a per-key sliding-window rate limiter: a key may perform at most
// limit actions in any trailing window of the configured duration.
//
// A sliding window is used instead of a fixed bucket because it cannot be gamed
// at a boundary: a fixed window would let a client spend its whole allowance at
// the end of one window and again at the start of the next, doubling the burst
// the write endpoints are protected against.
//
// Expired state is dropped lazily from Allow, at most once per window. A
// background ticker with Stop() was deliberately not used: NewAPI has no
// lifecycle hook to stop such a goroutine, so every discarded handler (tests,
// future embedders) would leak one. A lazy sweep has the same bounded memory
// behaviour without owning a resource.
//
// Limiter is safe for concurrent use.
type Limiter struct {
	limit  int
	window time.Duration

	// now is a seam for tests; production uses time.Now.
	now func() time.Time

	mu sync.Mutex
	// hits holds the still-valid timestamps of every key that acted recently,
	// oldest first. A key's slice never grows past limit, so the map is the
	// only unbounded dimension and the sweep below bounds it.
	hits      map[string][]time.Time
	lastSweep time.Time
}

// NewLimiter returns a limiter allowing limit actions per key in any trailing
// window. A non-positive limit or window cannot express a policy, so such a
// limiter rejects every action (fail closed) rather than silently disabling the
// protection.
func NewLimiter(limit int, window time.Duration) *Limiter {
	return newLimiterWithClock(limit, window, time.Now)
}

// newLimiterWithClock is NewLimiter with an injectable clock, so tests can move
// time instead of sleeping through the window.
func newLimiterWithClock(limit int, window time.Duration, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}

	return &Limiter{
		limit:     limit,
		window:    window,
		now:       now,
		hits:      make(map[string][]time.Time),
		lastSweep: now(),
	}
}

// Allow reports whether key may act now, and records the action when it may.
//
// The window is half-open: an action taken exactly window ago has expired, so a
// client that waited a full window is allowed again immediately.
func (l *Limiter) Allow(key string) bool {
	if l.limit < 1 || l.window <= 0 {
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	windowStart := now.Add(-l.window)
	hits := l.hits[key]
	// Timestamps are appended in order, so the expired ones always form a
	// prefix; dropping them with a single index keeps each Allow O(expired).
	cut := 0
	for cut < len(hits) && !hits[cut].After(windowStart) {
		cut++
	}
	hits = hits[cut:]

	if len(hits) >= l.limit {
		// Store the pruned slice: repeated rejections must not keep timestamps
		// from long-expired actions alive.
		l.hits[key] = hits
		return false
	}

	l.hits[key] = append(hits, now)
	return true
}

// sweep removes keys whose newest action has left the window. It runs from
// Allow at most once per window, which bounds the map by the number of distinct
// keys that acted within the last window (plus however many arrive before the
// next sweep runs) without a timer of its own.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now

	windowStart := now.Add(-l.window)
	for key, hits := range l.hits {
		// hits is oldest-first, so the last element is the newest action. A key
		// with no action left in the window can never be limited again until it
		// acts, and acting re-creates the entry.
		if len(hits) == 0 || !hits[len(hits)-1].After(windowStart) {
			delete(l.hits, key)
		}
	}
}
