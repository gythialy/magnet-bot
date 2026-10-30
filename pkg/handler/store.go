package handler

import (
	"sync"
	"time"
)

// DefaultCacheDuration is how long a Store entry lives when the caller does
// not ask for a specific TTL.
const DefaultCacheDuration = 10 * time.Minute

// Store is a small TTL cache keyed by string.
//
// Entries are evicted by a timer armed in Set, so a key that is written but
// never read again (a webhook that never arrives) cannot leak. All access is
// serialised by a single mutex: Get may delete an expired entry, so a read
// lock would be wrong.
type Store[T any] struct {
	mu    sync.Mutex
	items map[string]storeItem[T]
}

type storeItem[T any] struct {
	value      T
	expiration time.Time
}

// NewStore returns an empty Store.
func NewStore[T any]() *Store[T] {
	return &Store[T]{items: make(map[string]storeItem[T])}
}

// Set stores value under key for the given duration.
func (s *Store[T]) Set(key string, value T, duration time.Duration) {
	s.mu.Lock()
	s.items[key] = storeItem[T]{value: value, expiration: time.Now().Add(duration)}
	s.mu.Unlock()

	// deleteIfExpired re-checks the deadline, so a value overwritten by a
	// newer Set survives an older timer.
	time.AfterFunc(duration, func() { s.deleteIfExpired(key) })
}

// Get returns the value stored under key and reports whether it is still live.
func (s *Store[T]) Get(key string) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, found := s.items[key]
	if !found {
		var zero T
		return zero, false
	}
	if time.Now().After(item.expiration) {
		delete(s.items, key)
		var zero T
		return zero, false
	}
	return item.value, true
}

// Delete removes key.
func (s *Store[T]) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}

func (s *Store[T]) deleteIfExpired(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.items[key]; ok && time.Now().After(item.expiration) {
		delete(s.items, key)
	}
}
