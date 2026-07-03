// Package lruttl provides a concurrency-safe LRU cache with per-entry TTL.
//
// Capacity eviction: when the cache is full, the least-recently-used entry is
// evicted to make room. A Get hit promotes the entry to most-recently-used.
// TTL expiry is wall-clock-based; expired entries are lazily removed on Get.
package lruttl

import (
	"container/list"
	"sync"
	"time"
)

type entry struct {
	key    string
	value  any
	expiry time.Time
	elem   *list.Element
}

// Cache is a concurrency-safe LRU cache with per-entry TTL expiry.
type Cache struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*entry
	lru      *list.List // front = most recently used, back = least recently used
}

// New creates a Cache that holds at most capacity live entries (minimum 1).
func New(capacity int) *Cache {
	if capacity < 1 {
		capacity = 1
	}
	return &Cache{
		capacity: capacity,
		items:    make(map[string]*entry),
		lru:      list.New(),
	}
}

// Put stores key→value with the given TTL. If key already exists, its value
// and TTL are updated and it becomes most-recently-used.
func (c *Cache) Put(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	expiry := time.Now().Add(ttl)
	if e, ok := c.items[key]; ok {
		e.value = value
		e.expiry = expiry
		c.lru.MoveToFront(e.elem)
		return
	}
	if len(c.items) >= c.capacity {
		back := c.lru.Back()
		if back != nil {
			oldest := back.Value.(*entry)
			c.lru.Remove(back)
			delete(c.items, oldest.key)
		}
	}
	e := &entry{key: key, value: value, expiry: expiry}
	e.elem = c.lru.PushFront(e)
	c.items[key] = e
}

// Get returns the value for key and true if the entry is present and not expired.
// A hit promotes the entry to most-recently-used. An expired entry is lazily removed.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiry) {
		c.lru.Remove(e.elem)
		delete(c.items, key)
		return nil, false
	}
	c.lru.MoveToFront(e.elem)
	return e.value, true
}
