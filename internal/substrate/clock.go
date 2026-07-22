package substrate

import (
	"context"
	"time"
)

// ClockPort is the determinism port through which a vertical reads time, so a
// fake clock can replay timeouts and poll races in virtual time (RS-015).
type ClockPort interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	NewTicker(d time.Duration) Ticker
	// Sleep waits for d or until ctx is cancelled; it reports via its bool
	// return whether the full d elapsed. A bare Sleep(d) that cannot honor
	// cancellation is non-conformant.
	Sleep(ctx context.Context, d time.Duration) bool
}

// Ticker is the fake-able ticker returned by ClockPort.NewTicker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// ─── SystemClock ─────────────────────────────────────────────────────────────

// SystemClock is the real ClockPort implementation; it delegates to package
// time.
type SystemClock struct{}

// Now returns the current wall-clock time.
func (SystemClock) Now() time.Time { return time.Now() }

// Since returns the time elapsed since t.
func (SystemClock) Since(t time.Time) time.Duration { return time.Since(t) }

// NewTicker returns a real ticker firing every d.
func (SystemClock) NewTicker(d time.Duration) Ticker { return &systemTicker{t: time.NewTicker(d)} }

// Sleep waits for d or until ctx cancels, reporting whether the full d elapsed.
func (SystemClock) Sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false // ctx cancelled first
	case <-t.C:
		return true // full d elapsed
	}
}

type systemTicker struct{ t *time.Ticker }

func (s *systemTicker) C() <-chan time.Time { return s.t.C }
func (s *systemTicker) Stop()               { s.t.Stop() }

// ─── After ───────────────────────────────────────────────────────────────────

// After is the ClockPort-backed analogue of time.After for use in a select:
// it returns a channel that receives once, after d has elapsed on clk. Like
// time.After (and UNLIKE a ctx-bound sleep) the deadline fires UNCONDITIONALLY —
// the reap/fallback guards that use it must bound the wait even after the run
// ctx is cancelled, so the internal Sleep is anchored to context.Background().
// Under FakeClock the wake is driven by Advance, making run-path timeouts
// (agent-ready reap, resume-ready fallback) deterministic in tests
// (RSM-013 / M3-D4). The goroutine outlives the caller by at most d, matching
// time.After's un-cancellable timer. Buffered cap 1 so the send never blocks
// when the select picked another case first.
//
// Origin: internal/daemon/workloop.go clockAfter, moved by
// plans/2026-07-21-p2-extraction/RT19b-stranded-run-path-helpers.md.
func After(clk ClockPort, d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	go func() {
		if clk.Sleep(context.Background(), d) {
			ch <- clk.Now()
		}
	}()
	return ch
}
