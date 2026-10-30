package handler

import (
	"sync"
	"testing"
	"time"
)

func TestStoreGetAndExpiry(t *testing.T) {
	s := NewStore[int]()

	s.Set("k", 7, 20*time.Millisecond)
	if v, ok := s.Get("k"); !ok || v != 7 {
		t.Fatalf("Get(k) = (%d, %v), want (7, true)", v, ok)
	}
	if _, ok := s.Get("missing"); ok {
		t.Fatal("Get(missing) reported a value")
	}

	time.Sleep(50 * time.Millisecond)
	if _, ok := s.Get("k"); ok {
		t.Fatal("expired entry was still returned")
	}
}

// TestStoreEvictsUnreadEntry guards the leak: a conversion whose webhook never
// arrives is written but never read again.
func TestStoreEvictsUnreadEntry(t *testing.T) {
	s := NewStore[int]()
	s.Set("k", 1, 10*time.Millisecond)

	time.Sleep(60 * time.Millisecond)

	s.mu.Lock()
	_, found := s.items["k"]
	s.mu.Unlock()
	if found {
		t.Fatal("unread entry was never evicted")
	}
}

// TestStoreConcurrentAccess must stay clean under -race: Get deletes expired
// entries, so it needs the write lock.
func TestStoreConcurrentAccess(t *testing.T) {
	s := NewStore[int]()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() { s.Set("k", i, time.Millisecond) })
		wg.Go(func() { s.Get("k") })
		wg.Go(func() { s.Delete("k") })
	}
	wg.Wait()
}
