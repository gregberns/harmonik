package substrate

import (
	"context"
	"sync"
	"time"
)

// FakeClock is a virtual-time ClockPort. Virtual now is a field; nothing moves
// it except an explicit Advance call (not part of ClockPort). Sleepers and
// tickers register deadlines, and Advance walks the timeline in order, firing
// everything whose deadline falls in the interval it steps across. This buys
// precise interleaving control for deterministic replay (RS-015).
//
// FakeClock is safe for the single-goroutine test-driver usage: the driver
// calls Advance while a reactor goroutine registers sleeps/tickers; all shared
// state is guarded by mu.
type FakeClock struct {
	mu       sync.Mutex
	now      time.Time
	sleepers []*sleeper
	tickers  []*fakeTicker
}

// NewFakeClock constructs a FakeClock whose virtual time starts at start.
func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{now: start}
}

// Now returns the current virtual time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Since returns the virtual time elapsed since t.
func (c *FakeClock) Since(t time.Time) time.Duration { return c.Now().Sub(t) }

type sleeper struct {
	deadline time.Time
	done     chan struct{}
}

// Sleep registers a deadline now+d and blocks until either Advance reaches that
// deadline (returns true) or ctx cancels (returns false). d <= 0 returns true
// immediately.
func (c *FakeClock) Sleep(ctx context.Context, d time.Duration) bool {
	c.mu.Lock()
	if d <= 0 {
		c.mu.Unlock()
		return true
	}
	s := &sleeper{deadline: c.now.Add(d), done: make(chan struct{})}
	c.sleepers = append(c.sleepers, s)
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return false
	case <-s.done:
		return true
	}
}

// ─── Ticker ──────────────────────────────────────────────────────────────────

type fakeTicker struct {
	clock    *FakeClock
	ch       chan time.Time
	interval time.Duration
	nextFire time.Time // createTime + interval → first tick is AFTER one interval
	stopped  bool
}

// NewTicker returns a fake ticker whose first tick fires after one full
// interval (createTime + d), reproducing time.Ticker's first-tick-after-
// interval semantics (RS-015).
func (c *FakeClock) NewTicker(d time.Duration) Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTicker{
		clock:    c,
		ch:       make(chan time.Time, 1),
		interval: d,
		nextFire: c.now.Add(d),
	}
	c.tickers = append(c.tickers, t)
	return t
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }

// Stop removes the ticker from its clock and stops future fires. Like real
// time.Ticker, it does not drain the channel.
func (t *fakeTicker) Stop() {
	c := t.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	t.stopped = true
	out := c.tickers[:0]
	for _, other := range c.tickers {
		if other != t {
			out = append(out, other)
		}
	}
	c.tickers = out
}

// ─── Advance ─────────────────────────────────────────────────────────────────

// Advance moves virtual time forward by d, firing tickers at each interval
// boundary and waking sleepers whose deadline is reached, in timeline order.
// Advance is the test/harness driver; it is not part of ClockPort.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	target := c.now.Add(d)
	for {
		next, has := c.nextEventBefore(target)
		if !has {
			break
		}
		c.now = next
		c.fireTickersAt(next)
		c.wakeSleepersAt(next)
	}
	c.now = target
}

// nextEventBefore returns the earliest ticker-boundary or sleeper deadline in
// (now, target]. Caller holds mu.
func (c *FakeClock) nextEventBefore(target time.Time) (time.Time, bool) {
	var best time.Time
	has := false
	consider := func(t time.Time) {
		if t.After(c.now) && !t.After(target) {
			if !has || t.Before(best) {
				best, has = t, true
			}
		}
	}
	for _, t := range c.tickers {
		if !t.stopped {
			consider(t.nextFire)
		}
	}
	for _, s := range c.sleepers {
		consider(s.deadline)
	}
	return best, has
}

// fireTickersAt fires every live ticker whose nextFire == at, advancing its
// nextFire by one interval. The send is non-blocking (buffer cap 1), so a
// prior unconsumed tick coalesces — matching real time.Ticker. Caller holds mu.
func (c *FakeClock) fireTickersAt(at time.Time) {
	for _, t := range c.tickers {
		if t.stopped {
			continue
		}
		for !t.nextFire.After(at) { // fire once per boundary reached
			select {
			case t.ch <- at:
			default:
			}
			t.nextFire = t.nextFire.Add(t.interval)
		}
	}
}

// wakeSleepersAt closes done for every sleeper whose deadline <= at and drops
// it from the list. Caller holds mu.
func (c *FakeClock) wakeSleepersAt(at time.Time) {
	out := c.sleepers[:0]
	for _, s := range c.sleepers {
		if !s.deadline.After(at) {
			close(s.done)
		} else {
			out = append(out, s)
		}
	}
	c.sleepers = out
}

// BlockUntil spins until the number of pending sleepers plus live tickers
// equals n, so a test can deterministically wait for a reactor goroutine to
// register its sleep before advancing — avoiding the advance-before-arm race.
func (c *FakeClock) BlockUntil(n int) {
	for {
		c.mu.Lock()
		count := len(c.sleepers) + len(c.tickers)
		c.mu.Unlock()
		if count >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
