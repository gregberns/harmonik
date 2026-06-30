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
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

const (
	// staleWatchDefaultAfter is the default quiet window after which run_stale
	// is first emitted (M = 10 min per bead spec).
	staleWatchDefaultAfter = 10 * time.Minute

	// staleWatchScanInterval is how often the background goroutine wakes to
	// check all active runs. Keep small relative to staleWatchDefaultAfter so
	// we don't miss a window; 30 s gives ≤30 s detection latency.
	staleWatchScanInterval = 30 * time.Second

	// staleWatchReviewerLaunchAfter is the minimum quiet window applied when
	// the most recent event for a run is reviewer_launched.  The reviewer
	// session can run silently for up to 30 min before emitting a verdict, so
	// the base 10-min threshold produces 100% false-positive run_stale events
	// for that node type (logmine F38, hk-0z2).  30 min matches the reviewer's
	// observed worst-case latency.
	staleWatchReviewerLaunchAfter = 30 * time.Minute
)

// neverSpawnedReaperDefaultTimeout is the default deadline for the never-spawned
// reaper (hk-0z5x): when launch_initiated has been observed but agent_ready has
// not arrived within this window, the stale watcher cancels the per-run context
// so waitWithSocketGrace unblocks, the bead reopens, and the queue group drains.
//
// 30 min aligns with the observed incident timeline (run_stale emitted twice at
// 10 min and 20 min; operator intervened past 30 min). The reaper fires before
// a third run_stale would be needed, replacing manual daemon-restart with
// automatic per-run abort.
//
// Declared as var so tests can override without waiting real wall time.
var neverSpawnedReaperDefaultTimeout = 30 * time.Minute

// launchStallThreshold is the maximum time allowed between run_started and the
// first launch_initiated event.  If launch_initiated does not appear within
// this window the stale watcher emits launch_stall_detected once per run.
//
// 30 s is generous: under normal operation the pre-exec messages are emitted
// synchronously (milliseconds) between run_started and Launch().  A gap this
// wide therefore indicates a structural failure (tmux window creation failed,
// pre-exec emission gap) rather than normal latency.
//
// Declared as var so tests can override without waiting real wall time.
var launchStallThreshold = 30 * time.Second

// agentReadyStallThreshold is the maximum time allowed between launch_initiated
// and agent_ready.  If agent_ready does not appear within this window the stale
// watcher emits agent_ready_stall_detected once per run.
//
// This is the safety-net detector for the launch_initiated → agent_ready blind
// spot (hk-1s1or): once launch_initiated arrives the launch-stall check
// (launchStallThreshold) is suppressed, and the never-spawned reaper (hk-0z5x)
// only CANCELS the run after a much larger window (~30 min). Between those two
// there was NO observable event for a hung launch→ready transition. This
// threshold is deliberately SEPARATE from launchStallThreshold (different phase,
// larger window) and from NeverSpawnedReaperTimeout (the reaper still cancels at
// its own, larger deadline): it emits a detection event in a bounded few-minute
// window so the hang is visible and recoverable long before the 30-min reaper.
//
// 3 min is generous: under normal operation agent_ready follows launch_initiated
// within seconds (the relay synthesizes it on first SessionStart). A 3-min gap
// therefore indicates the agent process never started or its session was
// orphaned, not normal latency.
//
// Declared as var so tests can override without waiting real wall time.
var agentReadyStallThreshold = 3 * time.Minute

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

	// runStartedAt is the wall-clock time when run_started was observed for
	// this run.  Zero until the event is seen.
	// Used by the launch-stall detector (hk-fra5l).
	runStartedAt time.Time

	// launchInitiatedSeen is true once launch_initiated has been observed.
	// When true, the launch-stall check is suppressed for this run.
	// Used by the launch-stall detector (hk-fra5l).
	launchInitiatedSeen bool

	// launchStallEmitted is true once a launch_stall_detected event has been
	// emitted for this run.  The event is emitted at most once per run.
	launchStallEmitted bool

	// agentReadyStallEmitted is true once an agent_ready_stall_detected event has
	// been emitted for this run.  The event is emitted at most once per run.
	// Used by the launch_initiated → agent_ready stall detector (hk-1s1or).
	agentReadyStallEmitted bool

	// launchInitiatedAt is the wall-clock time when launch_initiated was first
	// observed. Zero until the event is seen. Used as the reference timestamp
	// for the never-spawned reaper (hk-0z5x) so that subsequent events (e.g.
	// daemon heartbeats) do not reset the deadline.
	launchInitiatedAt time.Time

	// agentReadySeen is true once agent_ready has been observed for this run.
	// Used by the never-spawned reaper: when launchInitiatedSeen is true but
	// agentReadySeen remains false past NeverSpawnedReaperTimeout, the reaper
	// cancels the per-run context so the queue group can drain (hk-0z5x).
	agentReadySeen bool

	// neverSpawnedFired is true once the never-spawned reaper has fired for
	// this run.  Prevents repeated cancel calls on subsequent scan ticks.
	// Bead ref: hk-0z5x.
	neverSpawnedFired bool

	// lastLaunchInitiatedAt is the wall-clock time of the MOST RECENT
	// launch_initiated event.  Unlike launchInitiatedAt (first-only), this
	// updates on every launch_initiated so the per-dispatch reaper below can
	// reference the most recent node's start time (hk-sj6a).
	lastLaunchInitiatedAt time.Time

	// agentReadySeenSinceLastLaunch is reset to false on every launch_initiated
	// and set to true when agent_ready arrives.  The per-dispatch reaper uses
	// this to detect a DOT reviewer session that stalls before agent_ready even
	// when a prior node (implementer) already set agentReadySeen (hk-sj6a).
	agentReadySeenSinceLastLaunch bool

	// killConsumerFired is true once the kill-consumer backstop (hk-tn36) has
	// fired for this run. Prevents repeated Cancel calls on subsequent scan
	// ticks. Set to true on the first run_stale emission.
	killConsumerFired bool
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

	// Gate is the INACTIVE poll gate (SS-007, hk-w6q7).  When non-nil and
	// gate.IsInactive() == true, scan returns early without doing work.
	// Nil means ungated (always scan).
	Gate *PollGate

	// StaleAfter is the base quiet window. Zero → staleWatchDefaultAfter (10 min).
	StaleAfter time.Duration

	// ReviewerLaunchStaleAfter is the minimum quiet window applied when the
	// most recent event for a run is reviewer_launched.  Zero →
	// staleWatchReviewerLaunchAfter (30 min).  This floor prevents
	// false-positive run_stale events during the reviewer's normal silent
	// execution window (logmine F38, hk-0z2).
	ReviewerLaunchStaleAfter time.Duration

	// ScanInterval is how often the background goroutine scans active runs.
	// Zero → staleWatchScanInterval (30 s).
	ScanInterval time.Duration

	// NeverSpawnedReaperTimeout is the deadline for the never-spawned reaper
	// (hk-0z5x): when launch_initiated has been observed but agent_ready has
	// not arrived within this window, the stale watcher calls handle.Cancel()
	// to abort the per-run context.
	// Zero → neverSpawnedReaperDefaultTimeout (30 min).
	NeverSpawnedReaperTimeout time.Duration

	// AgentReadyStallThreshold is the bounded detection window for the
	// launch_initiated → agent_ready blind spot (hk-1s1or): when launch_initiated
	// has been observed but agent_ready has not arrived within this window, the
	// stale watcher emits agent_ready_stall_detected once per run.  This is a
	// detection-only event (it does NOT cancel the run — the never-spawned reaper
	// still cancels at its own, larger NeverSpawnedReaperTimeout deadline); it
	// makes the hang observable in a bounded few-minute window.
	// Zero → agentReadyStallThreshold (3 min).
	AgentReadyStallThreshold time.Duration

	// Now is the wall-clock source. Nil → time.Now.
	Now func() time.Time
}

// StaleWatcher subscribes to the event bus to track the most recent event time
// per run_id and emits run_stale when a run goes silent.
type StaleWatcher struct {
	cfg  StaleWatcherConfig
	gate *PollGate // nil = ungated; see SS-007 / hk-w6q7

	mu     sync.Mutex
	states map[core.RunID]*runStaleState
}

// beadStaleAfter parses a "stale_after=<seconds>" or "stale_after:<seconds>"
// label from labels and returns the corresponding duration. Returns
// defaultAfter when no such label is present or the value is ≤0.
func beadStaleAfter(labels []string, defaultAfter time.Duration) time.Duration {
	for _, l := range labels {
		var val string
		switch {
		case strings.HasPrefix(l, "stale_after="):
			val = strings.TrimPrefix(l, "stale_after=")
		case strings.HasPrefix(l, "stale_after:"):
			val = strings.TrimPrefix(l, "stale_after:")
		default:
			continue
		}
		secs, err := strconv.ParseInt(val, 10, 64)
		if err != nil || secs <= 0 {
			return defaultAfter
		}
		return time.Duration(secs) * time.Second
	}
	return defaultAfter
}

// NewStaleWatcher creates a StaleWatcher from cfg. Call Subscribe before
// bus.Seal; call StartWatcher after Seal to launch the background goroutine.
func NewStaleWatcher(cfg StaleWatcherConfig) *StaleWatcher {
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = staleWatchDefaultAfter
	}
	if cfg.ReviewerLaunchStaleAfter <= 0 {
		cfg.ReviewerLaunchStaleAfter = staleWatchReviewerLaunchAfter
	}
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = staleWatchScanInterval
	}
	if cfg.NeverSpawnedReaperTimeout <= 0 {
		cfg.NeverSpawnedReaperTimeout = neverSpawnedReaperDefaultTimeout
	}
	if cfg.AgentReadyStallThreshold <= 0 {
		cfg.AgentReadyStallThreshold = agentReadyStallThreshold
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &StaleWatcher{
		cfg:    cfg,
		gate:   cfg.Gate,
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

	// hk-fra5l: track run_started and launch_initiated for the launch-stall
	// detector.  Both checks are inexpensive string comparisons.
	if core.EventType(typeStr) == core.EventTypeRunStarted && st.runStartedAt.IsZero() {
		st.runStartedAt = now
	}
	if core.EventType(typeStr) == core.EventTypeLaunchInitiated {
		st.launchInitiatedSeen = true
		// hk-0z5x: record the first launch_initiated timestamp as the reference
		// for the never-spawned reaper deadline. Subsequent events (daemon
		// heartbeats, etc.) must not reset this timestamp.
		if st.launchInitiatedAt.IsZero() {
			st.launchInitiatedAt = now
		}
		// hk-sj6a: update the most-recent launch timestamp and reset the
		// per-dispatch flag so the per-dispatch reaper can fire for a DOT
		// reviewer node that stalls before agent_ready.
		st.lastLaunchInitiatedAt = now
		st.agentReadySeenSinceLastLaunch = false
	}
	// hk-0z5x: track agent_ready so the never-spawned reaper knows when the
	// implementer has successfully started (suppresses the reaper for normal runs).
	if core.EventType(typeStr) == core.EventTypeAgentReady {
		st.agentReadySeen = true
		// hk-sj6a: mark agent_ready received for the current dispatch.
		st.agentReadySeenSinceLastLaunch = true
	}

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
	// SS-007: StaleWatcher is OFF at INACTIVE — skip when the fleet is idle.
	if w.gate != nil && w.gate.IsInactive() {
		return
	}
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
		//
		// Apply per-bead stale_after=<seconds> label override if present.
		st = &runStaleState{
			nextEmitAfter: beadStaleAfter(handle.Labels, w.cfg.StaleAfter),
		}
		w.states[runID] = st
	}
	// Hydrate BeadID from the RunHandle (available even before first event).
	if st.beadID == "" {
		st.beadID = handle.BeadID
	}

	// hk-fra5l: launch-stall detection — emit launch_stall_detected (once per
	// run) when run_started has been seen but launch_initiated has not arrived
	// within launchStallThreshold (30 s).  This fires independently of the main
	// staleness threshold so it is detectable even on short-lived runs.
	//
	// Capture fields needed for the stall check under the mutex.
	runStartedAt := st.runStartedAt
	launchInitiatedSeen := st.launchInitiatedSeen
	launchStallEmitted := st.launchStallEmitted
	beadIDForStall := st.beadID
	if !runStartedAt.IsZero() && !launchInitiatedSeen && !launchStallEmitted &&
		now.Sub(runStartedAt) > launchStallThreshold {
		st.launchStallEmitted = true
		w.mu.Unlock()
		w.emitLaunchStallDetected(ctx, runID, beadIDForStall, now.Sub(runStartedAt))
		w.mu.Lock()
		// Re-acquire st after unlock/lock cycle.
		st = w.states[runID]
		if st == nil {
			w.mu.Unlock()
			return
		}
	}

	// hk-1s1or: launch_initiated → agent_ready stall detection.  Once
	// launch_initiated arrives the launch-stall check above is suppressed
	// (launchInitiatedSeen=true), and the never-spawned reaper below only CANCELS
	// the run after the much larger NeverSpawnedReaperTimeout (~30 min).  Between
	// those two there was no observable event for a hung launch→ready transition.
	// Emit agent_ready_stall_detected exactly once per run when agent_ready has
	// not arrived within AgentReadyStallThreshold (a few minutes) so the hang is
	// detectable long before the reaper fires.  This is detection-only — it does
	// NOT cancel the run; the never-spawned reaper still owns the abort.
	//
	// Reference timestamp is launchInitiatedAt (the FIRST launch_initiated) so
	// that subsequent events (e.g. daemon heartbeats) do not delay the window.
	if st.launchInitiatedSeen && !st.agentReadySeen && !st.agentReadyStallEmitted &&
		!st.launchInitiatedAt.IsZero() &&
		now.Sub(st.launchInitiatedAt) > w.cfg.AgentReadyStallThreshold {
		st.agentReadyStallEmitted = true
		beadIDForARS := st.beadID
		arsStall := now.Sub(st.launchInitiatedAt)
		w.mu.Unlock()
		w.emitAgentReadyStallDetected(ctx, runID, beadIDForARS, arsStall)
		w.mu.Lock()
		// Re-acquire st after unlock/lock cycle.
		st = w.states[runID]
		if st == nil {
			w.mu.Unlock()
			return
		}
	}

	// hk-0z5x: never-spawned reaper — detect runs that received launch_initiated
	// (the tmux window was created) but never received agent_ready (the claude
	// process never started or the -default session was orphaned).  After
	// NeverSpawnedReaperTimeout the stale watcher cancels the per-run context
	// so waitWithSocketGrace unblocks, the bead is reopened, and the queue group
	// drains.  This fires once per run independently of the main run_stale logic.
	//
	// Reference timestamp is launchInitiatedAt (not lastEventAt) so that
	// subsequent events (e.g. daemon heartbeats) do not delay the deadline.
	launchInitiatedAt := st.launchInitiatedAt
	agentReadySeen := st.agentReadySeen
	neverSpawnedFired := st.neverSpawnedFired
	if st.launchInitiatedSeen && !agentReadySeen && !neverSpawnedFired && !launchInitiatedAt.IsZero() &&
		now.Sub(launchInitiatedAt) > w.cfg.NeverSpawnedReaperTimeout {
		st.neverSpawnedFired = true
		beadIDForNSR := st.beadID
		w.mu.Unlock()
		w.fireNeverSpawnedReaper(ctx, runID, beadIDForNSR, handle, now.Sub(launchInitiatedAt))
		w.mu.Lock()
		// Re-acquire st after unlock/lock cycle.
		st = w.states[runID]
		if st == nil {
			w.mu.Unlock()
			return
		}
	}

	// hk-sj6a: per-dispatch never-spawned reaper for DOT runs with sequential
	// nodes.  agentReadySeen is permanently true once any node gets agent_ready
	// (e.g. the implementer), permanently suppressing the classic check above for
	// all subsequent dispatches.  This per-dispatch variant uses
	// lastLaunchInitiatedAt (most-recent dispatch) and
	// agentReadySeenSinceLastLaunch (reset on each launch_initiated) so that a
	// reviewer session that stalls before agent_ready is still reaped.
	//
	// Fires at most once per run (shares neverSpawnedFired with the classic
	// check above).  Only triggers when agentReadySeen=true (at least one prior
	// dispatch succeeded), distinguishing the DOT multi-node case from the
	// single-dispatch path already handled by the classic check.
	lastLaunchInitiatedAt := st.lastLaunchInitiatedAt
	agentReadySinceLastLaunch := st.agentReadySeenSinceLastLaunch
	if agentReadySeen && !agentReadySinceLastLaunch && !neverSpawnedFired &&
		!lastLaunchInitiatedAt.IsZero() &&
		now.Sub(lastLaunchInitiatedAt) > w.cfg.NeverSpawnedReaperTimeout {
		st.neverSpawnedFired = true
		beadIDForNSR2 := st.beadID
		w.mu.Unlock()
		w.fireNeverSpawnedReaper(ctx, runID, beadIDForNSR2, handle, now.Sub(lastLaunchInitiatedAt))
		w.mu.Lock()
		// Re-acquire st after unlock/lock cycle.
		st = w.states[runID]
		if st == nil {
			w.mu.Unlock()
			return
		}
	}

	// Determine the reference time for age calculation. When an event has been
	// observed, use the last event time. When no event has arrived yet, fall
	// back to the run's StartedAt (guaranteed non-zero from RunHandle).
	refTime := st.lastEventAt
	if refTime.IsZero() {
		refTime = handle.StartedAt
	}

	age := now.Sub(refTime)

	// hk-0z2 (logmine F38): gate the threshold by last event type.  When the
	// most recent event is reviewer_launched, the reviewer session runs silently
	// for up to 30 min before emitting a verdict.  Apply a higher floor so we
	// do not emit false-positive run_stale events during that window.
	effectiveThreshold := st.nextEmitAfter
	if core.EventType(st.lastEventType) == core.EventTypeReviewerLaunched &&
		effectiveThreshold < w.cfg.ReviewerLaunchStaleAfter {
		effectiveThreshold = w.cfg.ReviewerLaunchStaleAfter
	}

	if age < effectiveThreshold {
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
	// hk-tn36: kill-consumer backstop fires on the FIRST run_stale emission.
	// Capture and set the flag under the same lock region as emitCount so the
	// backstop fires exactly once even under concurrent scan ticks.
	shouldKillConsumer := !st.killConsumerFired
	if shouldKillConsumer {
		st.killConsumerFired = true
	}
	// Double the window for the next emission (exponential backoff).
	// Use effectiveThreshold as the base so that the reviewer-launch gate
	// floor is accounted for in the schedule: if the gate raised the
	// threshold to 30 min, the next window is 60 min (not 20 min).
	st.nextEmitAfter = effectiveThreshold * 2
	w.mu.Unlock()

	// HC-064..HC-067 / hk-xrygh: before emitting run_stale, drive the session
	// lifecycle FSM to StateFailed(silent_hang) if the machine is in a live
	// (non-terminal) state. This ensures the Ready→Failed(silent_hang)
	// transition event fires BEFORE run_stale, satisfying the acceptance
	// criterion that "run_stale carries the lifecycle snapshot" and that the
	// silent-hang is visible as a deterministic FSM event first.
	var lifecycleStateStr, lifecycleEnteredAtStr string
	if m := handle.GetMachine(); m != nil {
		cur := m.Current()
		if !cur.IsTerminal() {
			// Drive to StateFailed(silent_hang). The transition call is
			// idempotent: if the machine is already in a terminal state (due
			// to a concurrent path) the error is silently ignored.
			from := cur
			if tErr := m.Transition(hclifecycle.StateFailed, hclifecycle.ReasonSilentHang,
				"run_stale", fmt.Sprintf("session silent for %ds", ageSeconds)); tErr == nil {
				// Successfully transitioned — emit lifecycle_transition event.
				w.emitSilentHangTransition(ctx, runID, m, from)
			}
		}
		// Re-read current state (may now be Failed).
		cur2 := m.Current()
		lifecycleStateStr = cur2.String()
		lifecycleEnteredAtStr = m.EnteredAt().UTC().Format(time.RFC3339)
	}

	// hk-fra5l: probe the worktree HEAD so orphan commits (implementer did
	// work but no lifecycle events were recorded) are visible in the run_stale
	// payload.  A non-empty WorktreeCommitSHA in the snapshot means the
	// worktree branch has at least one commit — operators can cherry-pick it.
	// The probe is best-effort: errors leave the field empty.
	worktreeCommitSHA := probeWorktreeHEAD(ctx, handle.WorktreePath)

	// Denormalize owning-epic attribution from RunHandle (hk-7evda, logmine F13).
	var owningEpicIDPtr, owningEpicAssigneePtr *string
	if handle.OwningEpicID != "" {
		id := handle.OwningEpicID
		owningEpicIDPtr = &id
	}
	if handle.OwningEpicAssignee != "" {
		a := handle.OwningEpicAssignee
		owningEpicAssigneePtr = &a
	}

	pl := core.RunStalePayload{
		RunID:              runID.String(),
		BeadID:             string(beadID),
		AgeSeconds:         ageSeconds,
		LastEventType:      lastEventType,
		LastEventAt:        lastEventAtStr,
		EmitCount:          emitCount,
		OwningEpicID:       owningEpicIDPtr,
		OwningEpicAssignee: owningEpicAssigneePtr,
		Snapshot: &core.RunStaleSnapshot{
			ActiveRunCount:     activeRunCount,
			GoroutineCount:     goroutineCount,
			LifecycleState:     lifecycleStateStr,
			LifecycleEnteredAt: lifecycleEnteredAtStr,
			WorktreeCommitSHA:  worktreeCommitSHA,
		},
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: marshal run_stale for run %s: %v\n", runID, err)
		return
	}
	_ = w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeRunStale, b)

	// hk-tn36: kill-consumer backstop — cancel the per-run context on the
	// first run_stale emission so a wedged consumer is reaped even when
	// pasteInjectQuitOnCommit's HB-staleness kill (8 min) did not fire (e.g.
	// eventCh=nil, or the kill goroutine itself stalled). If the 8-min kill
	// already fired, handle.Cancel() is a no-op (context already cancelled).
	if shouldKillConsumer {
		w.killConsumerBackstop(runID, beadID, handle, time.Duration(ageSeconds)*time.Second)
	}
}

// killConsumerBackstop is called (at most once per run) on the first run_stale
// emission. It marks the RunHandle as aborted and calls handle.Cancel() to
// unblock waitWithSocketGrace so the bead reopens and the queue group drains.
//
// Defense-in-depth: when pasteInjectQuitOnCommit's HB-staleness kill works
// correctly, the run is already gone before run_stale fires at M minutes; when
// it does NOT (e.g. eventCh=nil or the kill goroutine stalled), this backstop
// ensures the slot is freed at M minutes.
//
// If handle.Cancel is nil the method logs a warning and returns without
// aborting — the operator must restart the daemon to reap the stuck run.
//
// Bead ref: hk-tn36.
func (w *StaleWatcher) killConsumerBackstop(
	runID core.RunID,
	beadID core.BeadID,
	handle *RunHandle,
	age time.Duration,
) {
	fmt.Fprintf(os.Stderr,
		"daemon: stalewatch: kill-consumer backstop: bead %s run %s age %s — run_stale fired; aborting run\n",
		beadID, runID, age.Round(time.Second))
	if handle.Cancel == nil {
		fmt.Fprintf(os.Stderr,
			"daemon: stalewatch: kill-consumer backstop: bead %s run %s: Cancel is nil (run registered without per-run context); cannot auto-abort — operator restart required\n",
			beadID, runID)
		return
	}
	handle.aborted.Store(true)
	handle.Cancel()
}

// emitLaunchStallDetected emits a launch_stall_detected warning event.
// Called at most once per run by checkRun when run_started has been seen for
// longer than launchStallThreshold without a subsequent launch_initiated.
//
// Bead: hk-fra5l.
func (w *StaleWatcher) emitLaunchStallDetected(ctx context.Context, runID core.RunID, beadID core.BeadID, stall time.Duration) {
	pl := core.LaunchStallDetectedPayload{
		RunID:        runID.String(),
		BeadID:       string(beadID),
		StallSeconds: int64(stall.Seconds()),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: marshal launch_stall_detected for run %s: %v\n", runID, err)
		return
	}
	_ = w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeLaunchStallDetected, b)
}

// emitAgentReadyStallDetected emits an agent_ready_stall_detected warning event.
// Called at most once per run by checkRun when launch_initiated has been seen
// for longer than AgentReadyStallThreshold without a subsequent agent_ready.
//
// Bead: hk-1s1or.
func (w *StaleWatcher) emitAgentReadyStallDetected(ctx context.Context, runID core.RunID, beadID core.BeadID, stall time.Duration) {
	pl := core.AgentReadyStallDetectedPayload{
		RunID:        runID.String(),
		BeadID:       string(beadID),
		StallSeconds: int64(stall.Seconds()),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: marshal agent_ready_stall_detected for run %s: %v\n", runID, err)
		return
	}
	_ = w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeAgentReadyStallDetected, b)
}

// fireNeverSpawnedReaper is called (at most once per run) when launch_initiated
// was observed but agent_ready never arrived within NeverSpawnedReaperTimeout.
// It marks the RunHandle as aborted and calls handle.Cancel() to unblock
// waitWithSocketGrace (which is stuck on sess.Wait for an orphaned session).
// beadRunOne's per-run abort check then reopens the bead and emits run_failed
// so the queue group drains automatically.
//
// If handle.Cancel is nil (run registered before the per-run context was wired),
// the method logs a warning and takes no further action — the operator must
// restart the daemon to reap the stuck run.
//
// Bead ref: hk-0z5x.
func (w *StaleWatcher) fireNeverSpawnedReaper(
	_ context.Context,
	runID core.RunID,
	beadID core.BeadID,
	handle *RunHandle,
	elapsed time.Duration,
) {
	fmt.Fprintf(os.Stderr,
		"daemon: stalewatch: never-spawned reaper: bead %s run %s elapsed %s — launch_initiated but no agent_ready; aborting run\n",
		beadID, runID, elapsed.Round(time.Second))
	if handle.Cancel == nil {
		fmt.Fprintf(os.Stderr,
			"daemon: stalewatch: never-spawned reaper: bead %s run %s: Cancel is nil (run registered without per-run context); cannot auto-abort — operator restart required\n",
			beadID, runID)
		return
	}
	handle.aborted.Store(true)
	handle.Cancel()
}

// probeWorktreeHEAD returns the HEAD commit SHA of the git worktree at wtPath,
// or "" when wtPath is empty, the worktree has no commits, or the git probe
// fails.  Used to surface orphan commits in the run_stale payload (hk-fra5l).
func probeWorktreeHEAD(ctx context.Context, wtPath string) string {
	if wtPath == "" {
		return ""
	}
	out, err := execGitRevParse(ctx, wtPath)
	if err != nil {
		return ""
	}
	sha := strings.TrimSpace(out)
	// A bare "HEAD" output means the worktree has no commits (detached, empty).
	if sha == "HEAD" || sha == "" {
		return ""
	}
	return sha
}

// execGitRevParse runs `git -C dir rev-parse HEAD` and returns stdout.
// Declared as var so tests can stub it without spawning real git processes.
var execGitRevParse = func(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	return string(out), err
}

// emitSilentHangTransition emits a lifecycle_transition event for the
// Ready→Failed(silent_hang) transition driven by the stale watcher.
// Uses EmitWithRunID so the envelope carries the run_id for JSONL correlation.
func (w *StaleWatcher) emitSilentHangTransition(ctx context.Context, runID core.RunID, m *hclifecycle.Machine, from hclifecycle.LifecycleState) {
	p := core.LifecycleTransitionPayload{
		SessionID:      core.SessionID(m.SessionID()),
		FromState:      from.String(),
		ToState:        hclifecycle.StateFailed.String(),
		Reason:         string(hclifecycle.ReasonSilentHang),
		TransitionedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ErrCode:        "run_stale",
		ErrMsg:         "session unresponsive — stale watcher threshold exceeded",
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return
	}
	// Parse run_id from machine (it was set at Machine.New time).
	if parsedUUID, parseErr := uuid.Parse(m.RunID()); parseErr == nil {
		_ = w.cfg.Emitter.EmitWithRunID(ctx, core.RunID(parsedUUID), core.EventTypeLifecycleTransition, payload)
	} else {
		_ = w.cfg.Emitter.Emit(ctx, core.EventTypeLifecycleTransition, payload)
	}
}
