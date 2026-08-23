package daemon

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
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/sentinel"
)

const (
	staleWatchDefaultAfter = 10 * time.Minute

	staleWatchScanInterval = 30 * time.Second

	staleWatchReviewerLaunchAfter = 30 * time.Minute

	staleWatchNoProgressAfter = 120 * time.Minute
)

var neverSpawnedReaperDefaultTimeout = 30 * time.Minute

var launchStallThreshold = 30 * time.Second

var agentReadyStallThreshold = 3 * time.Minute

var forceReapGraceDefault = 90 * time.Second

var deadProcessStaleAfterDefault = 5 * time.Minute

type runStaleState struct {
	// beadID is the bead being executed in this run.
	beadID core.BeadID

	// lastEventType is the EventType of the most recent event seen for this run.
	lastEventType string

	// lastEventAt is the wall-clock time of the most recent event.
	lastEventAt time.Time

	// lastProgressAt is the wall-clock time of the most recent event that shows
	// the run MOVED, as opposed to showing that something is still there to
	// report on it. It is lastEventAt minus the daemon's own periodic beat and
	// minus the watcher's own alarms — see runProgressEvent. Zero until the
	// first such event, in which case the run's StartedAt is the reference.
	lastProgressAt time.Time

	// nextNoProgressAfter is the no-progress window for the next emission on
	// that clock. Initialised from the bead's "no_progress_after=<seconds>"
	// label (beadNoProgressAfter), falling back to w.cfg.NoProgressAfter, and
	// doubled after each emission the no-progress clock causes.
	//
	// It backs off separately from nextEmitAfter because the two clocks measure
	// different things: a wedged run that trips the no-progress window must not
	// widen the quiet window that catches a run which stops emitting entirely.
	nextNoProgressAfter time.Duration

	// emitCount is the number of run_stale events already emitted for this run.
	emitCount int

	// nextEmitAfter is the quiet-window threshold for the next emission.
	// Starts at staleAfter, doubles after each emission.
	nextEmitAfter time.Duration

	// runStartedAt is the wall-clock time when run_started was observed for
	// this run.  Zero until the event is seen.
	// Used by the launch-stall detector (hk-fra5l).
	runStartedAt time.Time

	// neverSpawnedTimeout is the per-run deadline for both the classic and
	// per-dispatch never-spawned reapers.  Initialised from the bead's
	// "never_spawned_timeout=<seconds>" label (beadNeverSpawnedTimeout);
	// falls back to w.cfg.NeverSpawnedReaperTimeout when no label is set.
	// Long-running DOT-mode implement beads should carry this label to avoid
	// false-positive context cancellations (B9, hk-8gixi).
	neverSpawnedTimeout time.Duration

	// agentReadyStallThreshold is the per-run detection window for
	// agent_ready_stall_detected.  Initialised from the bead's
	// "agent_ready_stall_threshold=<seconds>" label
	// (beadAgentReadyStallThreshold); falls back to
	// w.cfg.AgentReadyStallThreshold when no label is set.  Reasoning-model pi
	// profiles (ornith/DGX) should carry this label so their legitimate
	// ~20-min agent_ready latency does not trip the 3-min default (hk-4ir08).
	agentReadyStallThreshold time.Duration

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

	// cancelledAt is the wall-clock time the FIRST auto-cancel (kill-consumer
	// backstop, never-spawned reaper, or fast dead-process reap) was invoked
	// for this run — OR, when handle.Cancel was nil, the time that cancel was
	// attempted. Zero until any cancel has been invoked. The force-reap watchdog
	// (hk-mdus1) uses it as the grace-clock start: forceReapGrace after this
	// timestamp, a still-registered run is force-Unregistered directly.
	cancelledAt time.Time

	// forceReapFired is true once the force-reap watchdog has force-Unregistered
	// this run. Prevents a second force-reap on subsequent scan ticks (the run
	// is normally gone from the registry after the first, but a concurrent
	// re-register or slow delete must not double-emit the terminal event).
	forceReapFired bool

	// deadProcessCancelled is true once the fast dead-process reap (hk-mdus1)
	// has cancelled this run. Prevents repeated Cancel calls on subsequent ticks.
	deadProcessCancelled bool

	// lastLivenessAt is the wall-clock time of the most recent event that is
	// evidence this run is ALIVE. It is lastEventAt minus the watcher's own
	// alarms: a watchdog that treats its own alarm as a sign of life resets its
	// clock every time it fires and can never fire twice, and — because the
	// alarms are dispatched asynchronously — whether it fires at all becomes a
	// race. Zero until the first such event. Bead ref: hk-hsp9e.
	lastLivenessAt time.Time

	// phase is the run phase derived from the event stream, in the vocabulary
	// the Layer A stall detector reads. It moves forward on every event except a
	// launch, which moves it BACK — see advanceStallPhase for why a graph run
	// makes that necessary. Bead ref: hk-hsp9e.
	phase sentinel.RunPhase

	// verdictAt is the wall-clock time of the reviewer_verdict for the run's
	// CURRENT launch. It is the reference the review-stall signature measures
	// from, and it returns to zero when the run launches again, because a
	// verdict that sent the run back to the implementer is not a verdict the run
	// failed to act on. Bead ref: hk-hsp9e.
	verdictAt time.Time

	// stallEmitted records the stall signatures already reported for the run's
	// CURRENT launch. stall_detected is emitted at most once per signature per
	// launch: the detector re-derives the same hit on every scan, and without
	// this the event log floods at the scan cadence and one wedged run buries
	// every other run on the dashboard panel. The set is cleared on each launch
	// so that killing one node of a graph run does not silence every later node.
	// Bead ref: hk-hsp9e.
	stallEmitted map[core.StallSignature]bool
}

type forceReapCB struct {
	fn func(runID core.RunID, handle *RunHandle)
}
type runDeadCB struct {
	fn func(runID core.RunID, handle *RunHandle) bool
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

	// NoProgressAfter is the window a run may pass with no event that shows it
	// moved. It measures from the last such event, NOT from the last event of
	// any kind, so the daemon's own 5-minute agent_heartbeat does not refresh
	// it. Zero → staleWatchNoProgressAfter (90 min). Per-bead override via the
	// "no_progress_after=<seconds>" label (beadNoProgressAfter).
	NoProgressAfter time.Duration

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

	// ForceReapGrace is the grace period the force-reap watchdog waits after a
	// run's Cancel was invoked before force-Unregistering a still-registered
	// RunHandle (hk-mdus1). Zero → forceReapGraceDefault (90 s).
	ForceReapGrace time.Duration

	// DeadProcessStaleAfter gates the fast dead-process reap (hk-mdus1): a run
	// must have been event-silent this long AND be process-dead before the
	// dead-process probe cancels it. Zero → deadProcessStaleAfterDefault (2 min).
	DeadProcessStaleAfter time.Duration

	// ForceReap, when non-nil, is invoked by the force-reap watchdog immediately
	// before the RunHandle is force-Unregistered (hk-mdus1). It is the seam the
	// daemon wires to emit a terminal run_failed for the wedged run and to drive
	// its owning queue item terminal (evaluateGroupAdvanceWithOutcome) — work
	// the wedged per-run goroutine can no longer do itself. Nil (e.g. unit-test
	// mode with no ProjectDir / queueStore) → the watchdog still frees the slot
	// via Registry.Unregister; only the terminal-event/queue-advance is skipped.
	// May also be set post-construction via SetForceReap.
	ForceReap func(runID core.RunID, handle *RunHandle)

	// RunProcessDead, when non-nil, reports whether the agent process/pane for a
	// run is already gone (hk-mdus1 fast dead-process reap). The daemon wires it
	// to the tmux substrate's #{pane_pid} liveness (processDead / WindowPanePID).
	// Nil → the fast reap is inert and detection falls back to the stale
	// thresholds. May also be set post-construction via SetRunProcessDead.
	RunProcessDead func(runID core.RunID, handle *RunHandle) bool

	// RunSilenceStall is the quiet window that fires the heartbeat-gap
	// signature. Zero → stallRunSilenceDefault.
	RunSilenceStall time.Duration

	// ReviewFinalizeStall is the window allowed between reviewer_verdict and the
	// run's terminal event. Zero → stallReviewFinalizeDefault.
	ReviewFinalizeStall time.Duration

	// RunMaxAge is the absolute ceiling on how long a run may stay non-terminal.
	// Zero → stallRunMaxAgeDefault. Per-bead override via the
	// "run_max_age=<seconds>" label.
	RunMaxAge time.Duration

	// StallFeed routes the detector's findings to the stalled run's dispatch
	// machine, which turns them into the agent kill. Nil detects and reports but
	// never kills — which is the posture to use when only the reporting half is
	// wanted.
	StallFeed *runloop.StallFeed
}

// StaleWatcher subscribes to the event bus to track the most recent event time
// per run_id and emits run_stale when a run goes silent.
type StaleWatcher struct {
	cfg  StaleWatcherConfig
	gate *PollGate // nil = ungated; see SS-007 / hk-w6q7

	mu     sync.Mutex
	states map[core.RunID]*runStaleState

	// forceReapPtr / runDeadPtr publish the two optional daemon-wired seams
	// (hk-mdus1) race-free. They are stored at construction from cfg and may be
	// re-published later via SetForceReap / SetRunProcessDead (two-phase wiring:
	// the callbacks depend on legacy aggregate, which is built after StartWatcher).
	forceReapPtr atomic.Pointer[forceReapCB]
	runDeadPtr   atomic.Pointer[runDeadCB]
}

// SetForceReap publishes (or replaces) the force-reap seam after construction.
// Safe to call while the scan goroutine is running (hk-mdus1).
func (w *StaleWatcher) SetForceReap(fn func(runID core.RunID, handle *RunHandle)) {
	w.forceReapPtr.Store(&forceReapCB{fn: fn})
}

// SetRunProcessDead publishes (or replaces) the dead-process liveness seam after
// construction. Safe to call while the scan goroutine is running (hk-mdus1).
func (w *StaleWatcher) SetRunProcessDead(fn func(runID core.RunID, handle *RunHandle) bool) {
	w.runDeadPtr.Store(&runDeadCB{fn: fn})
}

func (w *StaleWatcher) forceReapFn() func(core.RunID, *RunHandle) {
	if p := w.forceReapPtr.Load(); p != nil {
		return p.fn
	}
	return nil
}

func (w *StaleWatcher) runProcessDeadFn() func(core.RunID, *RunHandle) bool {
	if p := w.runDeadPtr.Load(); p != nil {
		return p.fn
	}
	return nil
}

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

func beadNoProgressAfter(labels []string, defaultAfter time.Duration) time.Duration {
	for _, l := range labels {
		var val string
		switch {
		case strings.HasPrefix(l, "no_progress_after="):
			val = strings.TrimPrefix(l, "no_progress_after=")
		case strings.HasPrefix(l, "no_progress_after:"):
			val = strings.TrimPrefix(l, "no_progress_after:")
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

func beadNeverSpawnedTimeout(labels []string, defaultTimeout time.Duration) time.Duration {
	for _, l := range labels {
		var val string
		switch {
		case strings.HasPrefix(l, "never_spawned_timeout="):
			val = strings.TrimPrefix(l, "never_spawned_timeout=")
		case strings.HasPrefix(l, "never_spawned_timeout:"):
			val = strings.TrimPrefix(l, "never_spawned_timeout:")
		default:
			continue
		}
		secs, err := strconv.ParseInt(val, 10, 64)
		if err != nil || secs <= 0 {
			return defaultTimeout
		}
		return time.Duration(secs) * time.Second
	}
	return defaultTimeout
}

func beadAgentReadyStallThreshold(labels []string, defaultThreshold time.Duration) time.Duration {
	for _, l := range labels {
		var val string
		switch {
		case strings.HasPrefix(l, "agent_ready_stall_threshold="):
			val = strings.TrimPrefix(l, "agent_ready_stall_threshold=")
		case strings.HasPrefix(l, "agent_ready_stall_threshold:"):
			val = strings.TrimPrefix(l, "agent_ready_stall_threshold:")
		default:
			continue
		}
		secs, err := strconv.ParseInt(val, 10, 64)
		if err != nil || secs <= 0 {
			return defaultThreshold
		}
		return time.Duration(secs) * time.Second
	}
	return defaultThreshold
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
	if cfg.NoProgressAfter <= 0 {
		cfg.NoProgressAfter = staleWatchNoProgressAfter
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
	if cfg.ForceReapGrace <= 0 {
		cfg.ForceReapGrace = forceReapGraceDefault
	}
	if cfg.DeadProcessStaleAfter <= 0 {
		cfg.DeadProcessStaleAfter = deadProcessStaleAfterDefault
	}
	if cfg.RunSilenceStall <= 0 {
		cfg.RunSilenceStall = stallRunSilenceDefault
	}
	if cfg.ReviewFinalizeStall <= 0 {
		cfg.ReviewFinalizeStall = stallReviewFinalizeDefault
	}
	if cfg.RunMaxAge <= 0 {
		cfg.RunMaxAge = stallRunMaxAgeDefault
	}
	w := &StaleWatcher{
		cfg:    cfg,
		gate:   cfg.Gate,
		states: make(map[core.RunID]*runStaleState),
	}
	if cfg.ForceReap != nil {
		w.forceReapPtr.Store(&forceReapCB{fn: cfg.ForceReap})
	}
	if cfg.RunProcessDead != nil {
		w.runDeadPtr.Store(&runDeadCB{fn: cfg.RunProcessDead})
	}
	return w
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

func (w *StaleWatcher) observe(_ context.Context, evt core.Event) error {
	if evt.RunID == nil {
		return nil
	}
	runID := *evt.RunID

	now := w.cfg.Now()
	typeStr := string(evt.Type)

	w.mu.Lock()
	st, ok := w.states[runID]
	if !ok {
		st = &runStaleState{
			nextEmitAfter: w.cfg.StaleAfter,
		}
		w.states[runID] = st
	}
	st.lastEventType = typeStr
	st.lastEventAt = now

	if core.EventType(typeStr) == core.EventTypeRunStarted && st.runStartedAt.IsZero() {
		st.runStartedAt = now
	}
	if core.EventType(typeStr) == core.EventTypeLaunchInitiated {
		st.launchInitiatedSeen = true
		if st.launchInitiatedAt.IsZero() {
			st.launchInitiatedAt = now
		}
		st.lastLaunchInitiatedAt = now
		st.agentReadySeenSinceLastLaunch = false
	}
	if core.EventType(typeStr) == core.EventTypeAgentReady {
		st.agentReadySeen = true
		st.agentReadySeenSinceLastLaunch = true
	}

	advanceStallPhase(st, core.EventType(typeStr), now)
	if !watcherOwnAlarm(core.EventType(typeStr)) {
		st.lastLivenessAt = now
	}
	if runProgressEvent(core.EventType(typeStr)) {
		st.lastProgressAt = now
	}

	w.mu.Unlock()
	return nil
}

func watcherOwnAlarm(evType core.EventType) bool {
	switch evType {
	case core.EventTypeRunStale,
		core.EventTypeStallDetected,
		core.EventTypeLaunchStallDetected,
		core.EventTypeAgentReadyStallDetected:
		return true
	default:
		return false
	}
}

func runProgressEvent(evType core.EventType) bool {
	if watcherOwnAlarm(evType) {
		return false
	}
	switch evType {
	case core.EventTypeAgentHeartbeat,
		core.EventTypeNoProgressDetected,
		core.EventTypeImplementerResumed:
		return false
	default:
		return true
	}
}

func advanceStallPhase(st *runStaleState, evType core.EventType, at time.Time) {
	switch evType {
	case core.EventTypeLaunchInitiated:
		if st.phase >= sentinel.RunPhaseVerdictFired && st.phase < sentinel.RunPhaseTerminal {
			st.phase = sentinel.RunPhaseInImplementation
			st.verdictAt = time.Time{}
		}
		st.stallEmitted = nil

	case core.EventTypeImplementerPhaseComplete:
		if st.phase < sentinel.RunPhaseInImplementation {
			st.phase = sentinel.RunPhaseInImplementation
		}
	case core.EventTypeReviewerVerdict:
		if st.phase < sentinel.RunPhaseVerdictFired {
			st.phase = sentinel.RunPhaseVerdictFired
			st.verdictAt = at
		}
	case core.EventTypeRunCompleted, core.EventTypeRunFailed:
		st.phase = sentinel.RunPhaseTerminal
	default:
	}
}

// StartWatcher launches the background scan goroutine. Returns immediately;
// the goroutine runs until ctx is cancelled.
func (w *StaleWatcher) StartWatcher(ctx context.Context) {
	go w.loop(ctx)
}

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

func (w *StaleWatcher) scan(ctx context.Context) {
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

	w.stallPass(ctx, now, handles)

	w.mu.Lock()
	for runID := range w.states {
		if _, active := handles[runID]; !active {
			delete(w.states, runID)
		}
	}
	w.mu.Unlock()
}

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
		st = &runStaleState{
			nextEmitAfter:            beadStaleAfter(handle.Labels, w.cfg.StaleAfter),
			nextNoProgressAfter:      beadNoProgressAfter(handle.Labels, w.cfg.NoProgressAfter),
			neverSpawnedTimeout:      beadNeverSpawnedTimeout(handle.Labels, w.cfg.NeverSpawnedReaperTimeout),
			agentReadyStallThreshold: beadAgentReadyStallThreshold(handle.Labels, w.cfg.AgentReadyStallThreshold),
		}
		w.states[runID] = st
	} else {
		if st.neverSpawnedTimeout == 0 {
			st.neverSpawnedTimeout = beadNeverSpawnedTimeout(handle.Labels, w.cfg.NeverSpawnedReaperTimeout)
		}
		if st.agentReadyStallThreshold == 0 {
			st.agentReadyStallThreshold = beadAgentReadyStallThreshold(handle.Labels, w.cfg.AgentReadyStallThreshold)
		}
		if st.nextNoProgressAfter == 0 {
			st.nextNoProgressAfter = beadNoProgressAfter(handle.Labels, w.cfg.NoProgressAfter)
		}
	}
	if st.beadID == "" {
		st.beadID = handle.BeadID
	}

	if !st.cancelledAt.IsZero() && !st.forceReapFired && now.Sub(st.cancelledAt) >= w.cfg.ForceReapGrace {
		st.forceReapFired = true
		beadIDForReap := st.beadID
		grace := now.Sub(st.cancelledAt)
		w.mu.Unlock()
		w.forceReap(ctx, runID, beadIDForReap, handle, grace)
		return
	}

	if deadFn := w.runProcessDeadFn(); deadFn != nil &&
		st.cancelledAt.IsZero() && !st.deadProcessCancelled {
		ref := st.lastEventAt
		if ref.IsZero() {
			ref = handle.StartedAt
		}
		if now.Sub(ref) >= w.cfg.DeadProcessStaleAfter && deadFn(runID, handle) {
			st.deadProcessCancelled = true
			st.cancelledAt = now
			beadIDForDead := st.beadID
			silent := now.Sub(ref)
			w.mu.Unlock()
			fmt.Fprintf(os.Stderr,
				"daemon: stalewatch: fast dead-process reap: bead %s run %s: agent process/pane gone and silent %s — cancelling; force-reap in %s if still registered\n",
				beadIDForDead, runID, silent.Round(time.Second), w.cfg.ForceReapGrace)
			if handle.Cancel != nil {
				handle.aborted.Store(true)
				handle.Cancel()
			}
			return
		}
	}

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
		st = w.states[runID]
		if st == nil {
			w.mu.Unlock()
			return
		}
	}

	if st.launchInitiatedSeen && !st.agentReadySeen && !st.agentReadyStallEmitted &&
		!st.launchInitiatedAt.IsZero() &&
		now.Sub(st.launchInitiatedAt) > st.agentReadyStallThreshold {
		st.agentReadyStallEmitted = true
		beadIDForARS := st.beadID
		arsStall := now.Sub(st.launchInitiatedAt)
		w.mu.Unlock()
		w.emitAgentReadyStallDetected(ctx, runID, beadIDForARS, arsStall)
		w.mu.Lock()
		st = w.states[runID]
		if st == nil {
			w.mu.Unlock()
			return
		}
	}

	launchInitiatedAt := st.launchInitiatedAt
	agentReadySeen := st.agentReadySeen
	neverSpawnedFired := st.neverSpawnedFired
	if st.launchInitiatedSeen && !agentReadySeen && !neverSpawnedFired && !launchInitiatedAt.IsZero() &&
		now.Sub(launchInitiatedAt) > st.neverSpawnedTimeout {
		st.neverSpawnedFired = true
		if st.cancelledAt.IsZero() {
			st.cancelledAt = now
		}
		beadIDForNSR := st.beadID
		w.mu.Unlock()
		w.fireNeverSpawnedReaper(ctx, runID, beadIDForNSR, handle, now.Sub(launchInitiatedAt))
		w.mu.Lock()
		st = w.states[runID]
		if st == nil {
			w.mu.Unlock()
			return
		}
	}

	lastLaunchInitiatedAt := st.lastLaunchInitiatedAt
	agentReadySinceLastLaunch := st.agentReadySeenSinceLastLaunch
	if agentReadySeen && !agentReadySinceLastLaunch && !neverSpawnedFired &&
		!lastLaunchInitiatedAt.IsZero() &&
		now.Sub(lastLaunchInitiatedAt) > st.neverSpawnedTimeout {
		st.neverSpawnedFired = true
		if st.cancelledAt.IsZero() {
			st.cancelledAt = now
		}
		beadIDForNSR2 := st.beadID
		w.mu.Unlock()
		w.fireNeverSpawnedReaper(ctx, runID, beadIDForNSR2, handle, now.Sub(lastLaunchInitiatedAt))
		w.mu.Lock()
		st = w.states[runID]
		if st == nil {
			w.mu.Unlock()
			return
		}
	}

	refTime := st.lastEventAt
	if refTime.IsZero() {
		refTime = handle.StartedAt
	}

	age := now.Sub(refTime)

	effectiveThreshold := st.nextEmitAfter
	if core.EventType(st.lastEventType) == core.EventTypeReviewerLaunched &&
		effectiveThreshold < w.cfg.ReviewerLaunchStaleAfter {
		effectiveThreshold = w.cfg.ReviewerLaunchStaleAfter
	}

	progressRef := st.lastProgressAt
	if progressRef.IsZero() {
		progressRef = handle.StartedAt
	}
	noProgressAge := now.Sub(progressRef)

	quiet := age >= effectiveThreshold
	wedged := noProgressAge >= st.nextNoProgressAfter

	if !quiet && !wedged {
		w.mu.Unlock()
		return
	}
	var noProgressSeconds *int64
	if wedged {
		secs := int64(noProgressAge.Seconds())
		noProgressSeconds = &secs
	}

	st.emitCount++
	emitCount := st.emitCount
	ageSeconds := int64(age.Seconds())
	if ageSeconds < 1 {
		ageSeconds = 1
	}
	lastEventType := st.lastEventType
	lastEventAtStr := ""
	if !st.lastEventAt.IsZero() {
		lastEventAtStr = st.lastEventAt.UTC().Format(time.RFC3339)
	}
	beadID := st.beadID
	shouldKillConsumer := quiet && !st.killConsumerFired
	if shouldKillConsumer {
		st.killConsumerFired = true
		if st.cancelledAt.IsZero() {
			st.cancelledAt = now
		}
	}
	if quiet {
		st.nextEmitAfter = effectiveThreshold * 2
	}
	if wedged {
		st.nextNoProgressAfter *= 2
	}
	w.mu.Unlock()

	staleReason := fmt.Sprintf("session silent for %ds", ageSeconds)
	if noProgressSeconds != nil {
		staleReason = fmt.Sprintf("session made no progress for %ds (last event %s, %ds ago)",
			*noProgressSeconds, lastEventType, ageSeconds)
	}
	var lifecycleStateStr, lifecycleEnteredAtStr string
	if m := handle.GetMachine(); m != nil {
		cur := m.Current()
		if !cur.IsTerminal() {
			from := cur
			if tErr := m.Transition(hclifecycle.StateFailed, hclifecycle.ReasonSilentHang,
				"run_stale", staleReason); tErr == nil {
				w.emitSilentHangTransition(ctx, runID, m, from)
			}
		}
		cur2 := m.Current()
		lifecycleStateStr = cur2.String()
		lifecycleEnteredAtStr = m.EnteredAt().UTC().Format(time.RFC3339)
	}

	worktreeCommitSHA := probeWorktreeHEAD(ctx, handle.WorktreePath)

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
		NoProgressSeconds:  noProgressSeconds,
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
	if emitErr := w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeRunStale, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: emit run_stale for run %s: %v\n", runID, emitErr)
	}

	if shouldKillConsumer {
		w.killConsumerBackstop(runID, beadID, handle, time.Duration(ageSeconds)*time.Second)
	}
}

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

func (w *StaleWatcher) forceReap(_ context.Context, runID core.RunID, beadID core.BeadID, handle *RunHandle, sinceCancel time.Duration) {
	fmt.Fprintf(os.Stderr,
		"daemon: stalewatch: FORCE-REAP: bead %s run %s: still registered %s after Cancel — force-Unregistering leaked slot (hk-mdus1)\n",
		beadID, runID, sinceCancel.Round(time.Second))
	if fn := w.forceReapFn(); fn != nil {
		fn(runID, handle)
	}
	w.cfg.Registry.Unregister(runID)
}

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
	if emitErr := w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeLaunchStallDetected, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: emit launch_stall_detected for run %s: %v\n", runID, emitErr)
	}
}

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
	if emitErr := w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeAgentReadyStallDetected, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: emit agent_ready_stall_detected for run %s: %v\n", runID, emitErr)
	}
}

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

func probeWorktreeHEAD(ctx context.Context, wtPath string) string {
	if wtPath == "" {
		return ""
	}
	out, err := execGitRevParse(ctx, wtPath)
	if err != nil {
		return ""
	}
	sha := strings.TrimSpace(out)
	if sha == "HEAD" || sha == "" {
		return ""
	}
	return sha
}

var execGitRevParse = func(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	return string(out), err
}

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
	var emitErr error
	if parsedUUID, parseErr := uuid.Parse(m.RunID()); parseErr == nil {
		emitErr = w.cfg.Emitter.EmitWithRunID(ctx, core.RunID(parsedUUID), core.EventTypeLifecycleTransition, payload)
	} else {
		emitErr = w.cfg.Emitter.Emit(ctx, core.EventTypeLifecycleTransition, payload)
	}
	if emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: emit lifecycle_transition for run %s: %v\n", m.RunID(), emitErr)
	}
}
