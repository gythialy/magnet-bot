package server

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// limiterTestClock is a manually advanced clock. The limiter takes its time source as a
// function, so tests move time instead of sleeping through the window.
type limiterTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newLimiterTestClock(start time.Time) *limiterTestClock {
	return &limiterTestClock{now: start}
}

func (c *limiterTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *limiterTestClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

// TestLimiterAllowsUpToLimitThenRejects pins the basic budget: exactly limit
// actions pass inside one window, the next one is refused.
func TestLimiterAllowsUpToLimitThenRejects(t *testing.T) {
	clock := newLimiterTestClock(time.Unix(0, 0))
	limiter := newLimiterWithClock(3, time.Minute, clock.Now)

	for attempt := 1; attempt <= 3; attempt++ {
		if !limiter.Allow("user-1") {
			t.Fatalf("attempt %d rejected, want allowed", attempt)
		}
	}
	if limiter.Allow("user-1") {
		t.Fatal("attempt 4 allowed, want rejected")
	}
}

// TestLimiterWindowSlides proves two things a fixed-window limiter gets wrong:
// a rejection is not recorded as a hit, and the budget returns only once the
// oldest actions leave the trailing window.
func TestLimiterWindowSlides(t *testing.T) {
	clock := newLimiterTestClock(time.Unix(0, 0))
	limiter := newLimiterWithClock(2, time.Minute, clock.Now)

	for action := 1; action <= 2; action++ {
		if !limiter.Allow("user-1") {
			t.Fatalf("action %d rejected, want the first two allowed", action)
		}
	}

	// 59s later both actions are still inside the trailing minute.
	clock.Advance(59 * time.Second)
	if limiter.Allow("user-1") {
		t.Fatal("action allowed before the window elapsed")
	}

	// At exactly one minute the two original actions expire (the window is
	// half-open); the rejected attempts above must not have counted, so both
	// slots are free again.
	clock.Advance(time.Second)
	if !limiter.Allow("user-1") {
		t.Fatal("action rejected after the window elapsed")
	}
	if !limiter.Allow("user-1") {
		t.Fatal("second slot rejected after the window elapsed")
	}
	if limiter.Allow("user-1") {
		t.Fatal("a third action fit into a budget of two")
	}
}

// TestLimiterKeysAreIndependent keeps one account's burst from throttling
// another account.
func TestLimiterKeysAreIndependent(t *testing.T) {
	limiter := NewLimiter(1, time.Minute)

	if !limiter.Allow("user-a") {
		t.Fatal("first action for user-a rejected")
	}
	if limiter.Allow("user-a") {
		t.Fatal("second action for user-a allowed")
	}
	if !limiter.Allow("user-b") {
		t.Fatal("user-b was throttled by user-a's usage")
	}
}

// TestLimiterInvalidConfigFailsClosed: a limiter that cannot express a policy
// must reject instead of silently allowing everything.
func TestLimiterInvalidConfigFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int
		window time.Duration
	}{
		{"zero limit", 0, time.Minute},
		{"negative limit", -1, time.Minute},
		{"zero window", 10, 0},
		{"negative window", 10, -time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limiter := newLimiterWithClock(tc.limit, tc.window, time.Now)
			if limiter.Allow("user-1") {
				t.Fatal("Allow() = true for an invalid configuration, want false")
			}
		})
	}
}

// TestLimiterSweepsIdleKeys checks the lazy cleanup: a key whose last action
// left the window is dropped from the map once the sweep runs, so idle accounts
// do not accumulate forever.
func TestLimiterSweepsIdleKeys(t *testing.T) {
	clock := newLimiterTestClock(time.Unix(0, 0))
	limiter := newLimiterWithClock(10, time.Minute, clock.Now)

	if !limiter.Allow("idle") {
		t.Fatal("first action rejected")
	}

	// Two windows later the sweep runs during this Allow and must drop "idle".
	clock.Advance(2 * time.Minute)
	if !limiter.Allow("active") {
		t.Fatal("action after the sweep rejected")
	}

	limiter.mu.Lock()
	_, idlePresent := limiter.hits["idle"]
	_, activePresent := limiter.hits["active"]
	limiter.mu.Unlock()

	if idlePresent {
		t.Error("idle key survived the sweep")
	}
	if !activePresent {
		t.Error("active key was dropped by the sweep")
	}
}

// TestLimiterIsConcurrencySafe runs the limiter from many goroutines at once
// (the suite runs with -race). The shared budget must be spent exactly once per
// allowed call, and distinct keys must all be admitted.
func TestLimiterIsConcurrencySafe(t *testing.T) {
	const (
		workers    = 100
		sharedName = "shared"
	)

	shared := NewLimiter(25, time.Minute)
	perKey := NewLimiter(1, time.Minute)

	var (
		sharedAllowed atomic.Int64
		perAllowed    atomic.Int64
		wg            sync.WaitGroup
	)
	for i := range workers {
		wg.Go(func() {
			if shared.Allow(sharedName) {
				sharedAllowed.Add(1)
			}
			if perKey.Allow("user-" + strconv.Itoa(i)) {
				perAllowed.Add(1)
			}
		})
	}
	wg.Wait()

	if got := sharedAllowed.Load(); got != 25 {
		t.Fatalf("shared key allowed %d calls, want 25", got)
	}
	if got := perAllowed.Load(); got != workers {
		t.Fatalf("distinct keys allowed %d calls, want %d", got, workers)
	}
}
