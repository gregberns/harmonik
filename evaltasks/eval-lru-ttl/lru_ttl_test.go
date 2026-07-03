package lruttl

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestLRUTTL is the tamper-proof acceptance suite for the LRU+TTL cache.
// Implementers must NOT edit this file — only lru_ttl.go.
//
// Cache contract:
//   - New(capacity) creates a cache holding at most capacity live entries.
//   - Put(key, value, ttl) stores the entry; if key exists, updates value and resets TTL.
//   - Get(key) (value, ok) returns the value if present and not expired; ok=false on miss/expiry.
//   - Capacity eviction: when at capacity, the least-recently-used entry is evicted first.
//   - LRU promotion: a Get hit marks the entry as most-recently-used.
//   - TTL expiry is wall-clock-based; expired entries return ok=false and are lazily removed.
func TestLRUTTL(t *testing.T) {
	t.Run("expiry-then-get-miss", func(t *testing.T) {
		c := New(10)
		c.Put("x", 42, 50*time.Millisecond)
		if v, ok := c.Get("x"); !ok || v != 42 {
			t.Fatalf("before expiry: want (42, true), got (%v, %v)", v, ok)
		}
		time.Sleep(100 * time.Millisecond)
		if _, ok := c.Get("x"); ok {
			t.Fatal("after TTL expiry: expected miss, got hit")
		}
	})

	t.Run("ttl-refresh-on-put", func(t *testing.T) {
		c := New(10)
		// First Put: 80ms TTL.
		c.Put("k", "v1", 80*time.Millisecond)
		time.Sleep(50 * time.Millisecond) // 50ms in — still within first TTL
		// Second Put: refresh TTL to 200ms from now, update value.
		c.Put("k", "v2", 200*time.Millisecond)
		time.Sleep(100 * time.Millisecond) // 150ms from first Put; 100ms from refresh
		// Without refresh the entry would have expired (150ms > 80ms);
		// with refresh it has ~100ms remaining.
		v, ok := c.Get("k")
		if !ok {
			t.Fatal("after TTL refresh: expected hit, got miss")
		}
		if v != "v2" {
			t.Fatalf(`expected value "v2", got %v`, v)
		}
	})

	t.Run("lru-eviction-independent-of-ttl", func(t *testing.T) {
		const ttl = time.Hour // far from expiring — evictions are purely LRU capacity pressure
		c := New(2)
		c.Put("a", 1, ttl)
		c.Put("b", 2, ttl)
		// Promote "a" to most-recently-used; "b" becomes LRU.
		c.Get("a")
		// Adding "c" exceeds capacity — "b" (LRU) must be evicted despite its TTL.
		c.Put("c", 3, ttl)
		if _, ok := c.Get("a"); !ok {
			t.Error(`"a" should be present (promoted by Get)`)
		}
		if _, ok := c.Get("c"); !ok {
			t.Error(`"c" should be present (just inserted)`)
		}
		if _, ok := c.Get("b"); ok {
			t.Error(`"b" should have been evicted (was LRU)`)
		}
	})

	t.Run("concurrent-100-goroutines", func(t *testing.T) {
		c := New(50)
		const goroutines = 100
		const opsPerGoroutine = 200
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < opsPerGoroutine; j++ {
					key := fmt.Sprintf("k%d", (id*opsPerGoroutine+j)%100)
					c.Put(key, id*opsPerGoroutine+j, 500*time.Millisecond)
					c.Get(key)
				}
			}(i)
		}
		wg.Wait()
	})
}
