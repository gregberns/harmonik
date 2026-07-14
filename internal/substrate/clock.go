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
