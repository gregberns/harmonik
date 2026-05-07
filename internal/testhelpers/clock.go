// Package testhelpers provides test utilities for harmonik packages.
// It is imported only from *_test.go files or files with //go:build test-only
// constraints; it MUST NOT be imported from production code.
package testhelpers

import (
	"sync"
	"time"
)

// Clock is the minimal interface that deterministic-clock consumers depend on.
// Production code that needs the current time should accept a Clock rather than
// calling time.Now() directly so tests can substitute a FakeClock.
type Clock interface {
	// Now returns the current (possibly simulated) time.
	Now() time.Time
}

// FakeClock is a deterministic, goroutine-safe clock for use in tests.
// All FakeClock instances start at the epoch defined by NewFakeClock.
// Callers advance time explicitly via Advance; no wall-clock ticking occurs.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock returns a FakeClock set to t0.
// Pass a fixed, deterministic instant so test output is reproducible:
//
//	clk := testhelpers.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
func NewFakeClock(t0 time.Time) *FakeClock {
	return &FakeClock{now: t0}
}

// Now returns the clock's current simulated time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d. d MUST be non-negative; a negative
// value is a programming error and will panic.
func (c *FakeClock) Advance(d time.Duration) {
	if d < 0 {
		panic("testhelpers: FakeClock.Advance called with negative duration")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
