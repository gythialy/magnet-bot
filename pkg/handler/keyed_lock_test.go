package handler

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestKeyedLock verifies the in-process keyed lock semantics: a held key
// rejects every concurrent TryLock, and Unlock releases it for later callers.
func TestKeyedLock(t *testing.T) {
	l := NewKeyedLock()
	const key = "user:resource"

	// Fresh key is acquirable.
	if !l.TryLock(key) {
		t.Fatal("expected to acquire a fresh key")
	}

	// While held, every concurrent attempt must fail.
	var failed atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if l.TryLock(key) {
				t.Error("concurrent TryLock should fail while the key is held")
				l.Unlock(key)
				return
			}
			failed.Add(1)
		})
	}
	wg.Wait()
	if got := failed.Load(); got != 20 {
		t.Fatalf("expected all 20 concurrent attempts to fail, got %d", got)
	}

	// After Unlock the key is acquirable again.
	l.Unlock(key)
	if !l.TryLock(key) {
		t.Fatal("expected the key to be acquirable again after Unlock")
	}
	l.Unlock(key)

	// Unlock of a never-held key is a no-op.
	l.Unlock("never-held")
}
