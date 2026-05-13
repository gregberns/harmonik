package handlercontract

import "sync"

// watchertest.go — test-seam constructors for Watcher.
//
// These functions are regular (non-test-build-tag) exports so that packages
// outside handlercontract (e.g., internal/daemon) can construct stub Watcher
// values in their own test files.
//
// The helpers are named with a "ForTest" suffix to make their test-only
// intent clear.  They should not be used in production code paths.
//
// Bead: hk-gql20.22.

// NewWatcherForTest returns a *Watcher whose Done() channel is controlled by
// the caller-supplied cancel func.  Calling the cancel func signals Done.
// The cancel func is idempotent: calling it more than once is safe.
//
// Usage:
//
//	w, closeDone := handlercontract.NewWatcherForTest()
//	// ... arrange test ...
//	closeDone() // signals w.Done()
//
// Bead ref: hk-gql20.22.
func NewWatcherForTest() (*Watcher, func()) {
	done := make(chan struct{})
	w := &Watcher{done: done}
	var once sync.Once
	cancel := func() {
		once.Do(func() { close(done) })
	}
	return w, cancel
}
