package substrate_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// intEvent / intAction are throwaway instantiations so the generics themselves
// are covered independent of any vertical (the substrate's own L0).
type (
	intEvent  int
	intAction int
)

// ─── Run + SyntheticSource + FakeEffector ────────────────────────────────────

func TestRun_RecordsActionsInOrder(t *testing.T) {
	t.Parallel()
	src := substrate.NewSyntheticSource([]intEvent{1, 2, 3})
	// step doubles each event into two actions: 10*n and 10*n+1.
	step := func(e intEvent) []intAction { return []intAction{intAction(10 * e), intAction(10*e + 1)} }
	eff := &substrate.FakeEffector[intAction]{}

	if err := substrate.Run[intEvent, intAction](context.Background(), src, step, eff); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	got := eff.Actions()
	want := []intAction{10, 11, 20, 21, 30, 31}
	if len(got) != len(want) {
		t.Fatalf("action count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("action[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestFakeEffector_ResetAndActionsCopy(t *testing.T) {
	t.Parallel()
	eff := &substrate.FakeEffector[intAction]{}
	_ = eff.Execute(context.Background(), 1)
	_ = eff.Execute(context.Background(), 2)

	snap := eff.Actions()
	if len(snap) != 2 {
		t.Fatalf("Actions() len = %d, want 2", len(snap))
	}
	// Mutating the snapshot must not affect the effector's log.
	snap[0] = 99
	if again := eff.Actions(); again[0] != 1 {
		t.Fatalf("Actions() returned a live slice: got %d after mutation, want 1", again[0])
	}

	eff.Reset()
	if len(eff.Actions()) != 0 {
		t.Fatalf("after Reset, Actions() len = %d, want 0", len(eff.Actions()))
	}
}

func TestSyntheticSource_CancelledCtxClosedEmpty(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := substrate.NewSyntheticSource([]intEvent{1, 2, 3})
	ch := src.Events(ctx)
	n := 0
	for range ch {
		n++
	}
	if n != 0 {
		t.Fatalf("cancelled-ctx source delivered %d events, want 0", n)
	}
}

// ─── Twin + ReplayCodec ──────────────────────────────────────────────────────

// intCodec decodes decimal-integer corpus lines into intEvents. A line "skip"
// is skipped; a line "bad" is a fatal decode error. It is stateful (seq).
type intCodec struct{ seq int }

func (c *intCodec) DecodeLine(line []byte) (intEvent, bool, error) {
	s := strings.TrimSpace(string(line))
	switch s {
	case "skip":
		return 0, false, nil
	case "bad":
		return 0, false, fmt.Errorf("intCodec: bad line %q", s)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false, err
	}
	c.seq++
	return intEvent(n), true, nil
}

// ErrorEvent uses a sentinel negative value as the transport-error terminal.
func (c *intCodec) ErrorEvent(msg string) intEvent { return intEvent(-1) }

// DisconnectEvent uses a distinct sentinel for connection-lost.
func (c *intCodec) DisconnectEvent() intEvent { return intEvent(-2) }

const (
	errTerminal  = intEvent(-1)
	discTerminal = intEvent(-2)
)

// drain collects every event from a Twin under a short ctx, failing if the
// stream does not terminate promptly.
func drainTwin(t *testing.T, tw *substrate.Twin[intEvent]) []intEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var got []intEvent
	for ev := range tw.Events(ctx) {
		got = append(got, ev)
	}
	return got
}

const corpus3 = "1\n2\n3\n"

func TestTwin_FaultNone_FaithfulReplay(t *testing.T) {
	t.Parallel()
	tw := substrate.NewTwin[intEvent](strings.NewReader(corpus3), substrate.FaultConfig{}, &intCodec{})
	got := drainTwin(t, tw)
	want := []intEvent{1, 2, 3}
	if !eqEvents(got, want) {
		t.Fatalf("FaultNone got %v, want %v", got, want)
	}
}

func TestTwin_SkipAndFatal(t *testing.T) {
	t.Parallel()
	// "skip" is skipped; "bad" is fatal → ErrorEvent then close.
	tw := substrate.NewTwin[intEvent](strings.NewReader("1\nskip\n2\nbad\n3\n"), substrate.FaultConfig{}, &intCodec{})
	got := drainTwin(t, tw)
	want := []intEvent{1, 2, errTerminal}
	if !eqEvents(got, want) {
		t.Fatalf("skip/fatal got %v, want %v", got, want)
	}
}

func TestTwin_FaultDropAfter(t *testing.T) {
	t.Parallel()
	tw := substrate.NewTwin[intEvent](strings.NewReader(corpus3),
		substrate.FaultConfig{Mode: substrate.FaultDropAfter, EventN: 2}, &intCodec{})
	got := drainTwin(t, tw)
	want := []intEvent{1, 2, discTerminal}
	if !eqEvents(got, want) {
		t.Fatalf("FaultDropAfter got %v, want %v", got, want)
	}
	assertTerminal(t, got, discTerminal)
}

func TestTwin_FaultTruncate(t *testing.T) {
	t.Parallel()
	tw := substrate.NewTwin[intEvent](strings.NewReader(corpus3),
		substrate.FaultConfig{Mode: substrate.FaultTruncate, EventN: 2}, &intCodec{})
	got := drainTwin(t, tw)
	want := []intEvent{1, errTerminal}
	if !eqEvents(got, want) {
		t.Fatalf("FaultTruncate got %v, want %v", got, want)
	}
	assertTerminal(t, got, errTerminal)
}

func TestTwin_FaultDup(t *testing.T) {
	t.Parallel()
	tw := substrate.NewTwin[intEvent](strings.NewReader(corpus3),
		substrate.FaultConfig{Mode: substrate.FaultDup, EventN: 2}, &intCodec{})
	got := drainTwin(t, tw)
	want := []intEvent{1, 2, 2, 3}
	if !eqEvents(got, want) {
		t.Fatalf("FaultDup got %v, want %v", got, want)
	}
}

func TestTwin_FaultStall_TerminatesOnCtxCancel(t *testing.T) {
	t.Parallel()
	tw := substrate.NewTwin[intEvent](strings.NewReader(corpus3),
		substrate.FaultConfig{Mode: substrate.FaultStall, EventN: 2}, &intCodec{})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan []intEvent, 1)
	go func() {
		var got []intEvent
		for ev := range tw.Events(ctx) {
			got = append(got, ev)
		}
		done <- got
	}()
	select {
	case got := <-done:
		// Stall delivers event 1 then blocks before event 2; ctx-timeout closes.
		if !eqEvents(got, []intEvent{1}) {
			t.Fatalf("FaultStall got %v, want [1] before ctx-timeout close", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FaultStall did not terminate after ctx cancel (hang)")
	}
}

func assertTerminal(t *testing.T, got []intEvent, terminal intEvent) {
	t.Helper()
	if len(got) == 0 || got[len(got)-1] != terminal {
		t.Fatalf("stream did not end with terminal %d: %v", terminal, got)
	}
}

func eqEvents(a, b []intEvent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ─── FakeClock ───────────────────────────────────────────────────────────────

var epoch = time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)

func TestFakeClock_AdvanceMovesNow(t *testing.T) {
	t.Parallel()
	c := substrate.NewFakeClock(epoch)
	if !c.Now().Equal(epoch) {
		t.Fatalf("initial Now = %v, want %v", c.Now(), epoch)
	}
	c.Advance(5 * time.Second)
	if got := c.Since(epoch); got != 5*time.Second {
		t.Fatalf("Since(epoch) = %v, want 5s", got)
	}
}

func TestFakeClock_SleepReturnsTrueWhenElapsed(t *testing.T) {
	t.Parallel()
	c := substrate.NewFakeClock(epoch)
	res := make(chan bool, 1)
	go func() { res <- c.Sleep(context.Background(), 3*time.Second) }()
	c.BlockUntil(1) // wait for the sleep to register
	c.Advance(3 * time.Second)
	select {
	case ok := <-res:
		if !ok {
			t.Fatal("Sleep returned false, want true (fully elapsed)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sleep did not wake after Advance")
	}
}

func TestFakeClock_SleepReturnsFalseOnCancel(t *testing.T) {
	t.Parallel()
	c := substrate.NewFakeClock(epoch)
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan bool, 1)
	go func() { res <- c.Sleep(ctx, time.Hour) }()
	c.BlockUntil(1)
	cancel()
	select {
	case ok := <-res:
		if ok {
			t.Fatal("Sleep returned true, want false (cancelled)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sleep did not return after ctx cancel")
	}
}

func TestFakeClock_TickerFiresAtIntervals(t *testing.T) {
	t.Parallel()
	c := substrate.NewFakeClock(epoch)
	tk := c.NewTicker(200 * time.Millisecond)
	defer tk.Stop()

	// First-tick-after-interval: no tick before one full interval.
	c.Advance(100 * time.Millisecond)
	select {
	case <-tk.C():
		t.Fatal("ticker fired before one full interval")
	default:
	}

	// Cross the first boundary at t0+200ms.
	c.Advance(100 * time.Millisecond)
	select {
	case ts := <-tk.C():
		if want := epoch.Add(200 * time.Millisecond); !ts.Equal(want) {
			t.Fatalf("first tick at %v, want %v", ts, want)
		}
	default:
		t.Fatal("ticker did not fire at first interval boundary")
	}

	// Cross the second boundary.
	c.Advance(200 * time.Millisecond)
	select {
	case ts := <-tk.C():
		if want := epoch.Add(400 * time.Millisecond); !ts.Equal(want) {
			t.Fatalf("second tick at %v, want %v", ts, want)
		}
	default:
		t.Fatal("ticker did not fire at second interval boundary")
	}
}
