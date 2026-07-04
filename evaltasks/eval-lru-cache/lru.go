// Package evallrucache provides an O(1) LRU cache for eval grading.
// This is the pre-committed reference; the model-under-test overwrites it.
package evallrucache

import "container/list"

// LRU is a fixed-capacity least-recently-used cache of int→int.
type LRU struct {
	cap   int
	items map[int]*list.Element
	order *list.List
}

type entry struct {
	key, val int
}

// NewLRU creates a new LRU cache with the given capacity.
func NewLRU(capacity int) *LRU {
	return &LRU{
		cap:   capacity,
		items: make(map[int]*list.Element),
		order: list.New(),
	}
}

// Get returns the value for key and marks it as most-recently-used.
// Returns (0, false) when key is absent.
func (c *LRU) Get(key int) (int, bool) {
	el, ok := c.items[key]
	if !ok {
		return 0, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*entry).val, true
}

// Put inserts or updates key→val and marks it as most-recently-used.
// If over capacity, the least-recently-used entry is evicted.
func (c *LRU) Put(key, val int) {
	if el, ok := c.items[key]; ok {
		el.Value.(*entry).val = val
		c.order.MoveToFront(el)
		return
	}
	if c.order.Len() == c.cap {
		lru := c.order.Back()
		if lru != nil {
			c.order.Remove(lru)
			delete(c.items, lru.Value.(*entry).key)
		}
	}
	el := c.order.PushFront(&entry{key, val})
	c.items[key] = el
}
