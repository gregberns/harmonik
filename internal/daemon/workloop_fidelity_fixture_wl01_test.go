package daemon_test

import (
	"context"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// wl01Trace records the external effects this characterization freezes. It is
// deliberately smaller than workLoopDeps: tests name semantic effects only.
type wl01Trace struct {
	mu     sync.Mutex
	events []string
}

func (w *wl01Trace) add(event string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
}

func (w *wl01Trace) snapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.events...)
}

// wl01SingleOwnerEffects groups effects that have one owner in each bounded
// scenario. Outcome variants share an owner so a compensating success/failure,
// close/reopen, or terminal/revert pair cannot hide duplicate ownership.
var wl01SingleOwnerEffects = map[string]string{
	"queue.reservation.durable":   "queue.reservation",
	"ledger.claim":                "ledger.claim",
	"registry.register":           "registry.register",
	"executor.started":            "executor.started",
	"executor.terminal.success":   "executor.terminal",
	"executor.terminal.failure":   "executor.terminal",
	"executor.terminal.cancelled": "executor.terminal",
	"queue.item_terminal.success": "queue.terminal",
	"queue.item_terminal.failure": "queue.terminal",
	"queue.revert_pending":        "queue.terminal",
	"ledger.close":                "ledger.terminal",
	"ledger.reopen":               "ledger.terminal",
	"registry.unregister":         "registry.unregister",
	"shutdown.begin":              "shutdown.begin",
	"shutdown.queue_cancel":       "shutdown.queue_cancel",
	"shutdown.return":             "shutdown.return",
	"restart.adopt_session":       "restart.adopt_session",
}

func wl01DuplicateSingleOwnerEffect(events []string) (string, bool) {
	seen := make(map[string]struct{}, len(wl01SingleOwnerEffects))
	for _, event := range events {
		owner, singleOwner := wl01SingleOwnerEffects[event]
		if !singleOwner {
			continue
		}
		if _, duplicate := seen[owner]; duplicate {
			return owner, true
		}
		seen[owner] = struct{}{}
	}
	return "", false
}

func wl01AssertTrace(t *testing.T, got, required, forbidden []string) {
	t.Helper()
	if owner, duplicate := wl01DuplicateSingleOwnerEffect(got); duplicate {
		t.Fatalf("trace contains duplicate single-owner effect %q: got %v", owner, got)
	}
	pos := 0
	for _, want := range required {
		for pos < len(got) && got[pos] != want {
			pos++
		}
		if pos == len(got) {
			t.Fatalf("trace missing ordered effect %q: got %v", want, got)
		}
		pos++
	}
	for _, forbiddenEvent := range forbidden {
		for _, event := range got {
			if event == forbiddenEvent {
				t.Fatalf("trace contains forbidden effect %q: got %v", forbiddenEvent, got)
			}
		}
	}
}

// wl01Ledger is a controlled adapter for the ledger boundary. It records only
// Ready/Show/Claim/terminal effects; production owns selection, registration,
// executor launch, and queue mutation.
type wl01Ledger struct {
	trace        *wl01Trace
	ready        []core.BeadRecord
	queuePath    bool
	claimCheck   func(core.RunID) error
	claimErr     error
	readyCalled  chan struct{}
	claimCalled  chan struct{}
	closed       chan struct{}
	holdShowCall int
	showEntered  chan struct{}
	showRelease  <-chan struct{}
	showCalls    int
	onceReady    sync.Once
	onceClosed   sync.Once
	showMu       sync.Mutex
	showOnce     sync.Once
}

func (w *wl01Ledger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	w.trace.add("source.br_ready.ready")
	w.onceReady.Do(func() { close(w.readyCalled) })
	ready := w.ready
	w.ready = nil
	return ready, nil
}

func (w *wl01Ledger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	w.showMu.Lock()
	w.showCalls++
	hold := w.holdShowCall > 0 && w.showCalls == w.holdShowCall
	w.showMu.Unlock()
	if w.queuePath {
		w.trace.add("source.queue.select")
	} else {
		w.trace.add("source.br_ready.show")
	}
	if hold {
		w.showOnce.Do(func() { close(w.showEntered) })
		<-w.showRelease
		if err := ctx.Err(); err != nil {
			return core.BeadRecord{}, err
		}
	}
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (w *wl01Ledger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, runID core.RunID, _ core.TransitionID, _ core.BeadID) error {
	if w.claimCheck != nil {
		if err := w.claimCheck(runID); err != nil {
			w.trace.add("ledger.claim")
			select {
			case w.claimCalled <- struct{}{}:
			default:
			}
			return err
		}
	}
	w.trace.add("ledger.claim")
	select {
	case w.claimCalled <- struct{}{}:
	default:
	}
	return w.claimErr
}

func (w *wl01Ledger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, needsAttention bool) error {
	if needsAttention {
		w.trace.add("executor.terminal.failure")
	} else {
		w.trace.add("executor.terminal.success")
	}
	w.trace.add("ledger.close")
	if w.closed != nil {
		w.onceClosed.Do(func() { close(w.closed) })
	}
	return nil
}

func (w *wl01Ledger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	w.trace.add("ledger.reopen")
	return nil
}

var _ interface {
	Ready(context.Context) ([]core.BeadRecord, error)
	ShowBead(context.Context, core.BeadID) (core.BeadRecord, error)
	ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error
	CloseBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, bool) error
	ReopenBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, string) error
} = (*wl01Ledger)(nil)
