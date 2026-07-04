package evallrucache_test

import (
	"testing"

	evallrucache "github.com/gregberns/harmonik/evaltasks/eval-lru-cache"
)

func TestLRU(t *testing.T) {
	t.Parallel()

	t.Run("basic_eviction", func(t *testing.T) {
		t.Parallel()
		c := evallrucache.NewLRU(2)
		c.Put(1, 10)
		c.Put(2, 20)
		c.Put(3, 30) // evicts key 1 (LRU)
		if _, ok := c.Get(1); ok {
			t.Error("key 1 should have been evicted")
		}
		if v, ok := c.Get(2); !ok || v != 20 {
			t.Errorf("Get(2) = %d,%v, want 20,true", v, ok)
		}
		if v, ok := c.Get(3); !ok || v != 30 {
			t.Errorf("Get(3) = %d,%v, want 30,true", v, ok)
		}
	})

	t.Run("get_refreshes_recency", func(t *testing.T) {
		t.Parallel()
		c := evallrucache.NewLRU(2)
		c.Put(1, 10)
		c.Put(2, 20)
		c.Get(1)    // 1 is now MRU, 2 is LRU
		c.Put(3, 30) // evicts key 2 (LRU)
		if _, ok := c.Get(2); ok {
			t.Error("key 2 should have been evicted (it was LRU after Get(1))")
		}
		if v, ok := c.Get(1); !ok || v != 10 {
			t.Errorf("Get(1) = %d,%v, want 10,true", v, ok)
		}
	})

	t.Run("put_updates_existing", func(t *testing.T) {
		t.Parallel()
		c := evallrucache.NewLRU(2)
		c.Put(1, 10)
		c.Put(2, 20)
		c.Put(1, 99) // update + refresh recency; 2 is now LRU
		c.Put(3, 30) // evicts key 2
		if _, ok := c.Get(2); ok {
			t.Error("key 2 should have been evicted")
		}
		if v, ok := c.Get(1); !ok || v != 99 {
			t.Errorf("Get(1) = %d,%v, want 99,true", v, ok)
		}
	})

	t.Run("miss", func(t *testing.T) {
		t.Parallel()
		c := evallrucache.NewLRU(3)
		if _, ok := c.Get(42); ok {
			t.Error("Get on empty cache should return false")
		}
	})
}
