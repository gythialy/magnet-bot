package handler

import "sync"

// KeyedLock is a keyed in-process lock. Callers mark a resource (a project
// URL, an alarm credit code, ...) as being handled with TryLock, so a
// concurrent invocation in the same process skips it instead of duplicating
// the work. Unlock releases the key once the work is done.
//
// KeyedLock is process-local by design. For cross-instance safety (multiple
// bot processes sharing the same DB), pair it with a DB-backed claim such as
// dal.InsertIfAbsent, where the table's unique constraint is the authority.
type KeyedLock struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

// NewKeyedLock returns an empty KeyedLock.
func NewKeyedLock() *KeyedLock {
	return &KeyedLock{keys: make(map[string]struct{})}
}

// TryLock marks key as held and reports whether the caller acquired it.
// A second concurrent TryLock for the same key returns false.
func (l *KeyedLock) TryLock(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.keys[key]; ok {
		return false
	}
	l.keys[key] = struct{}{}
	return true
}

// Unlock releases a previously acquired key. It is safe to call for a key
// that is not held (no-op).
func (l *KeyedLock) Unlock(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.keys, key)
}
