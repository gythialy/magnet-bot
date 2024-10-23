package pkg

import (
	"sync"
	"time"
)

type Store struct {
	mu    sync.RWMutex
	items map[string]storeItem
}

type storeItem struct {
	value      interface{}
	expiration time.Time
}

func NewStore() *Store {
	return &Store{
		items: make(map[string]storeItem),
	}
}

func (s *Store) Set(key string, value interface{}, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = storeItem{
		value:      value,
		expiration: time.Now().Add(duration),
	}
}

func (s *Store) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, found := s.items[key]
	if !found {
		return nil, false
	}
	if time.Now().After(item.expiration) {
		delete(s.items, key)
		return nil, false
	}
	return item.value, true
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}
