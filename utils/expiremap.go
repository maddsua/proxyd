package utils

import (
	"sync"
	"time"
)

type ExpireMap[T any] struct {
	mtx       sync.Mutex
	entries   map[string]*ExpireMapEntry[T]
	lastSweep time.Time
}

func (em *ExpireMap[T]) Get(key string) *ExpireMapEntry[T] {

	em.mtx.Lock()
	defer em.mtx.Unlock()

	return em.getNoLock(key)
}

func (em *ExpireMap[T]) getNoLock(key string) *ExpireMapEntry[T] {

	if em.entries == nil {
		return nil
	}

	if entry := em.entries[key]; entry != nil && entry.Expires.After(time.Now()) {
		return entry
	}

	return nil
}

func (em *ExpireMap[T]) Set(key string, val T, ttl time.Duration) *ExpireMapEntry[T] {

	em.mtx.Lock()
	defer em.mtx.Unlock()

	return em.setNoLock(key, val, ttl)
}

func (em *ExpireMap[T]) SetNoExist(key string, val T, ttl time.Duration) *ExpireMapEntry[T] {

	em.mtx.Lock()
	defer em.mtx.Unlock()

	if stored := em.getNoLock(key); stored != nil {
		return stored
	}

	return em.setNoLock(key, val, ttl)
}

func (em *ExpireMap[T]) setNoLock(key string, val T, ttl time.Duration) *ExpireMapEntry[T] {

	if em.entries == nil {
		em.entries = map[string]*ExpireMapEntry[T]{}
		em.lastSweep = time.Now()
	} else if time.Since(em.lastSweep) > time.Minute {
		em.sweep()
	}

	entry := &ExpireMapEntry[T]{Val: val, TTL: ttl}
	entry.Bump()

	em.entries[key] = entry

	return entry
}

func (em *ExpireMap[T]) sweep() {

	for key, val := range em.entries {
		if val == nil || val.Expires.Before(time.Now()) {
			delete(em.entries, key)
		}
	}

	em.lastSweep = time.Now()
}

func (em *ExpireMap[T]) Del(key string) {

	em.mtx.Lock()
	defer em.mtx.Unlock()

	delete(em.entries, key)
}

func (em *ExpireMap[T]) Clear() {

	em.mtx.Lock()
	defer em.mtx.Unlock()

	em.entries = nil
}

type ExpireMapEntry[T any] struct {
	Val     T
	TTL     time.Duration
	Expires time.Time
}

func (entry *ExpireMapEntry[T]) Bump() {
	entry.Expires = time.Now().Add(entry.TTL)
}
