package daemon

import (
	"fmt"
	"sync/atomic"
)

// ConcurrencyController holds the runtime-mutable max_concurrent ceiling.
//
// It is shared between the workloop (reads the ceiling each dispatch tick via
// Get) and the socket RPC handler (updates it via Set). All methods are safe
// for concurrent access via the embedded atomic.
//
// The controller is created once by daemon.Start from cfg.MaxConcurrent,
// wired into workLoopDeps.concurrencyCtrl, and passed to the HandlerAdapter
// so that queue-set-concurrency RPC ops can adjust the ceiling with no daemon
// restart. The workloop reads Get() on every capacity-gate check, so a raise
// takes effect on the next gate evaluation; a lower lets in-flight runs
// complete naturally and stops new dispatch once running count drops below n.
//
// Bead ref: hk-ohiaf.
type ConcurrencyController struct {
	val atomic.Int32
}

// NewConcurrencyController returns a ConcurrencyController initialised with
// initial (floor 1). Called by daemon.Start to wire the initial
// --max-concurrent value into the workloop and the RPC handler.
func NewConcurrencyController(initial int) *ConcurrencyController {
	c := &ConcurrencyController{}
	v := initial
	if v < 1 {
		v = 1
	}
	c.val.Store(int32(v))
	return c
}

// Get returns the current concurrency ceiling. Safe for concurrent access.
func (c *ConcurrencyController) Get() int {
	return int(c.val.Load())
}

// Set updates the concurrency ceiling to n and returns the previous value.
// Returns an error when n < 1.
func (c *ConcurrencyController) Set(n int) (old int, err error) {
	if n < 1 {
		return 0, fmt.Errorf("max_concurrent must be >= 1, got %d", n)
	}
	old = int(c.val.Swap(int32(n)))
	return old, nil
}
