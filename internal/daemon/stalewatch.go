package daemon

// stalewatch.go — periodic stale-run detector for the harmonik daemon.
//
// RunStaleWatcher subscribes to the event bus as a wildcard observer to track
// the most recent event time per run_id. A background goroutine scans the
// RunRegistry every scanInterval and emits run_stale when a run has been
// silent for M minutes (staleAfter). Re-emission follows an exponential
// backoff schedule: M, 2M, 4M, … (doublings).
//
// Configuration:
//   - staleAfter: base quiet window (default: 10 min). Configurable via
//     Config.StaleAfterSeconds (per-daemon) or a per-bead label
//     "stale_after=<seconds>" (per-bead override via beadStaleAfter).
//   - scanInterval: how often the background goroutine wakes (default: 30 s).
//
// The watcher must be constructed and Subscribed BEFORE bus.Seal (EV-009).
// StartWatcher is called after Seal to launch the background goroutine.
//
// Spec ref: specs/event-model.md §8.12.1 (run_stale).
// Bead ref: hk-wkzlc.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

const (
	// staleWatchDefaultAfter is the default quiet window after which run_stale
	// is first emitted (M = 10 min per bead spec).
	staleWatchDefaultAfter = 10 * time.Minute

	// staleWatchScanInterval is how often the background goroutine wakes to
	// check all active runs. Keep small relative to staleWatchDefaultAfter so
	// we don't miss a window; 30 s gives ≤30 s detection latency.
	staleWatchScanInterval = 30 * time.Second
)

// runStaleState tracks per-run staleness accounting inside StaleWatcher.
type runStaleState struct {
	// beadID is the bead being executed in this run.
	beadID core.BeadID

	// lastEventType is the EventType of the most recent event seen for this run.
	lastEventType string

	// lastEventAt is the wall-clock time of the most recent event.
	lastEventAt time.Time

	// emitCount is the number of run_stale events already emitted for this run.
	emitCount int

	// nextEmitAfter is the quiet-window threshold for the next emission.
	// Starts at staleAfter, doubles after each emission.
	nextEmitAfter time.Duration
}

// StaleWatcherConfig holds the construction-time parameters for StaleWatcher.
type StaleWatcherConfig struct {
	// SubscribeBus is the daemon event bus used to register the wildcard observer
	// subscription. Required. MUST be called before bus.Seal (EV-009).
	SubscribeBus eventbus.EventBus

	// Emitter is the event emitter used to publish run_stale events. Required.
	Emitter handlercontract.EventEmitter

	// Registry is the in-flight run registry. Required.
	Registry *RunRegistry

	// StaleAfter is the base quiet window. Zero → staleWatchDefaultAfter (10 min).
	StaleAfter time.Duration

	// ScanInterval is how often the background goroutine scans active runs.
	// Zero → staleWatchScanInterval (30 s).
	ScanInterval time.Duration

	// Now is the wall-clock source. Nil → time.Now.
	Now func() time.Time
}

// StaleWatcher subscribes to the event bus to track the most recent event time
// per run_id and emits run_stale when a run goes silent.
type StaleWatcher struct {
	cfg StaleWatcherConfig

	mu     sync.Mutex
	states map[core.RunID]*runStaleState
}

// NewStaleWatcher creates a StaleWatcher from cfg. Call Subscribe before
// bus.Seal; call StartWatcher after Seal to launch the background goroutine.
func NewStaleWatcher(cfg StaleWatcherConfig) *StaleWatcher {
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = staleWatchDefaultAfter
	}
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = staleWatchScanInterval
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &StaleWatcher{
		cfg:    cfg,
		states: make(map[core.RunID]*runStaleState),
	}
}

// Subscribe registers the watcher as a wildcard observer on SubscribeBus.
// MUST be called before bus.Seal (EV-009).
func (w *StaleWatcher) Subscribe() error {
	sub := core.Subscription{
		ConsumerID:    "stale-watcher",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler:       w.observe,
	}
	if _, err := w.cfg.SubscribeBus.Subscribe(sub); err != nil {
		return fmt.Errorf("StaleWatcher.Subscribe: %w", err)
	}
	return nil
}

// observe is the bus observer callback. It MUST NOT block (EV-012).
func (w *StaleWatcher) observe(_ context.Context, evt core.Event) error {
	if evt.RunID == nil {
		// Event carries no run_id — not trackable.
		return nil
	}
	runID := *evt.RunID

	now := w.cfg.Now()
	typeStr := string(evt.Type)

	w.mu.Lock()
	st, ok := w.states[runID]
	if !ok {
		// First event seen for this run — initialise state.
		st = &runStaleState{
			nextEmitAfter: w.cfg.StaleAfter,
		}
		w.states[runID] = st
	}
	st.lastEventType = typeStr
	st.lastEventAt = now
	w.mu.Unlock()
	return nil
}

// StartWatcher launches the background scan goroutine. Returns immediately;
// the goroutine runs until ctx is cancelled.
func (w *StaleWatcher) StartWatcher(ctx context.Context) {
	go w.loop(ctx)
}

// loop is the background goroutine body.
func (w *StaleWatcher) loop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scan(ctx)
		}
	}
}

// scan checks every registered in-flight run for staleness.
func (w *StaleWatcher) scan(ctx context.Context) {
	now := w.cfg.Now()
	handles := w.cfg.Registry.snapshotWithKeys()
	goroutineCount := runtime.NumGoroutine()
	activeRunCount := len(handles)

	for runID, handle := range handles {
		w.checkRun(ctx, runID, handle, now, goroutineCount, activeRunCount)
	}

	// Prune state entries for runs that are no longer in the registry.
	w.mu.Lock()
	for runID := range w.states {
		if _, active := handles[runID]; !active {
			delete(w.states, runID)
		}
	}
	w.mu.Unlock()
}

// checkRun evaluates a single run for staleness and emits if warranted.
func (w *StaleWatcher) checkRun(
	ctx context.Context,
	runID core.RunID,
	handle *RunHandle,
	now time.Time,
	goroutineCount, activeRunCount int,
) {
	w.mu.Lock()
	st, ok := w.states[runID]
	if !ok {
		// No events seen for this run yet. Initialise with empty lastEventAt so
		// the payload carries consistent (both-empty) last_event fields. The age
		// is computed from handle.StartedAt as the reference point.
		st = &runStaleState{
			nextEmitAfter: w.cfg.StaleAfter,
		}
		w.states[runID] = st
	}
	// Hydrate BeadID from the RunHandle (available even before first event).
	if st.beadID == "" {
		st.beadID = handle.BeadID
	}

	// Determine the reference time for age calculation. When an event has been
	// observed, use the last event time. When no event has arrived yet, fall
	// back to the run's StartedAt (guaranteed non-zero from RunHandle).
	refTime := st.lastEventAt
	if refTime.IsZero() {
		refTime = handle.StartedAt
	}

	age := now.Sub(refTime)
	if age < st.nextEmitAfter {
		w.mu.Unlock()
		return
	}

	// Stale threshold crossed — capture snapshot fields under the lock.
	st.emitCount++
	emitCount := st.emitCount
	ageSeconds := int64(age.Seconds())
	lastEventType := st.lastEventType
	lastEventAtStr := ""
	if !st.lastEventAt.IsZero() {
		lastEventAtStr = st.lastEventAt.UTC().Format(time.RFC3339)
	}
	beadID := st.beadID
	// Double the window for the next emission (exponential backoff).
	st.nextEmitAfter *= 2
	w.mu.Unlock()

	pl := core.RunStalePayload{
		RunID:         runID.String(),
		BeadID:        string(beadID),
		AgeSeconds:    ageSeconds,
		LastEventType: lastEventType,
		LastEventAt:   lastEventAtStr,
		EmitCount:     emitCount,
		Snapshot: &core.RunStaleSnapshot{
			ActiveRunCount: activeRunCount,
			GoroutineCount: goroutineCount,
		},
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: marshal run_stale for run %s: %v\n", runID, err)
		return
	}
	_ = w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeRunStale, b)
}
