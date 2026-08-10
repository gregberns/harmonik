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
//   - noProgressAfter: the SECOND clock behind run_stale (default: 90 min).
//     The quiet window above is measured from the last event of any kind, and
//     the daemon emits agent_heartbeat for a live agent process every 5 min, so
//     it cannot fire for a run that wedges with its process alive. This window
//     is measured from the last event that shows the run MOVED, so the daemon's
//     own beat does not refresh it. Per-bead override via
//     "no_progress_after=<seconds>" (beadNoProgressAfter).
//   - scanInterval: how often the background goroutine wakes (default: 30 s).
//   - neverSpawnedReaperDefaultTimeout: deadline for the never-spawned reaper
//     (default: 30 min). Per-bead override via "never_spawned_timeout=<seconds>"
//     label (beadNeverSpawnedTimeout).  Use this label on long-running DOT-mode
//     implement beads that legitimately exceed the 30-min ceiling (B9, hk-8gixi).
//   - agentReadyStallThreshold: detection window for the launch_initiated →
//     agent_ready blind spot (default: 3 min). Per-bead override via
//     "agent_ready_stall_threshold=<seconds>" label
//     (beadAgentReadyStallThreshold). Use this label on beads dispatched
//     against a reasoning-model pi profile (e.g. ornith/DGX), whose legitimate
//     agent_ready latency (~20 min) is far past the 3-min default and would
//     otherwise trip a spurious agent_ready_stall_detected on every healthy
//     run (hk-4ir08).
//   - the Layer A stall thresholds (RunSilenceStall 22 min, ReviewFinalizeStall
//     10 min, RunMaxAge 4 h): the per-run stall detector in stallfeed.go, which
//     emits stall_detected and kills a frozen agent. Per-bead override of the
//     age ceiling via the "run_max_age=<seconds>" label (beadRunMaxAge). Use it
//     on a bead whose work legitimately outlives the ceiling.
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

	// staleWatchNoProgressAfter is the default window a run may pass without a
	// single event that shows it MOVED.
	//
	// It backs a SECOND clock, deliberately separate from the quiet window
	// above. The quiet window measures silence, and the daemon breaks that
	// silence itself: RunHeartbeatLoop emits agent_heartbeat for the run every
	// 5 min for as long as the agent process exists, whether or not the agent
	// is doing anything. Measured from the last event of any kind, a run that
	// wedges with its process alive is refreshed by that beat every 5 min
	// against a 10-min window, so run_stale can never fire for it — the last
	// 490 recorded runs produced 2 run_stale events between them. This is the
	// same trap
	// launchInitiatedAt and lastLivenessAt already guard against, one layer up:
	// the beat is the daemon's own timer, so reading it as proof the run is
	// alive is a watchdog listening to its own alarm.
	//
	// THIS CLOCK REPORTS. IT DOES NOT CANCEL. Only the quiet window above arms
	// killConsumerBackstop — see the shouldKillConsumer assignment in checkRun.
	// That is what makes the number below safe to be approximately right, and
	// the first draft of this change shows why the property is worth having.
	//
	// It is not free of consequence, though, and the one it does have is worth
	// naming: an emission on either clock drives the session FSM to
	// StateFailed(silent_hang), and that state has no valid outgoing
	// transitions. So an early fire does not kill the agent, but it does close
	// the machine for a run that may still be working. Nothing reaps on FSM
	// state today, so the cost is a wrong record rather than lost work: a later
	// legitimate transition on a closed machine is silently ignored rather than
	// raised as an error, and the silent-hang aggregate degraded-daemon reason
	// has no producer. The one live cost is an operator wake — cognition-loop
	// consumers treat Ready→Failed(silent_hang) as a judgment wake — which is
	// the price the reporting-only design accepts in exchange for never
	// cancelling on this clock.
	//
	// That draft chose 90 min and justified it with a measurement: across the
	// recorded implementer phases the longest was 85 min and none passed 90, so
	// 90 left a 5-minute margin. The measurement does not survive contact with
	// the instrument. The claude path gives up on its own commit hard ceiling at
	// exactly 90 min (pasteinject.go commitHardCeiling), so no phase CAN be
	// recorded past 90 — the sample is truncated at the very value being chosen
	// and "none over 90" describes the ceiling, not the workload. Re-measured
	// over 2372 phases: max 90.6 min, p99 90.1 min, 75 phases past 85 min, and
	// every one of those 75 carries commit_landed=false with exit 0, which is
	// the ceiling giving up. Nothing at all is recorded past 91 min.
	//
	// So the real margin at 90 min was not 5 minutes, it was negative: a window
	// firing at 90.0 pre-empts the ceiling by seconds and turns an orderly
	// give-up into a cancel, on the claude path the draft said was unaffected.
	//
	// 120 min is chosen to clear the truncation rather than to sit on it. It is
	// the first round value comfortably past the 90-min ceiling that hides the
	// true tail, and past the 4-hour run-age backstop it changes nothing. The
	// population this clock uniquely covers is the SessionIDCaptured harnesses
	// (codex, pi), where pasteTarget is nil so no commit ceiling is built at all
	// and a wedged child holds its slot until that 4-hour backstop. Their
	// recorded history is far too thin to bound a tail from — 18 clean codex
	// runs and 2 pi runs — which is the second reason this clock reports rather
	// than cancels. When those harnesses have a real history, re-measure, and
	// arming the canceller becomes a decision someone can defend with data.
	//
	// A bead whose work legitimately passes the window carries a
	// "no_progress_after=<seconds>" label, the same escape hatch the other
	// watchdogs in this file use.
	staleWatchNoProgressAfter = 120 * time.Minute
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

// forceReapGraceDefault is the grace period (hk-mdus1) the force-reap watchdog
// waits AFTER a run's per-run context Cancel has been invoked before it directly
// Unregisters the leaked RunHandle from the RunRegistry.
//
// Rationale: the existing auto-cancellers (kill-consumer backstop, never-spawned
// reaper) call handle.Cancel() exactly once, then rely on the per-run goroutine
// unwinding via waitWithSocketGrace → beadRunOne return → deferred Unregister to
// free the concurrency slot. When the goroutine is parked on a wait that does
// NOT observe runCtx (a br/git subprocess on context.Background(), a mutex, a
// merge-build), Cancel never unwinds it and the RunHandle — and thus the
// concurrency slot it occupies in RunRegistry.Len()/LenForQueue — leaks forever,
// eventually closing the global gate and gridlocking the fleet until a daemon
// restart. The force-reap watchdog is the re-entrant backstop that closes that
// leak: 90 s after Cancel, if the run is STILL registered, it force-Unregisters
// the handle directly (freeing the slot) and drives the queue item terminal.
//
// 90 s is comfortably longer than the socket-grace / normal unwind window
// (seconds) so a genuinely-unwinding run is never force-reaped prematurely, yet
// short enough that a truly wedged slot is reclaimed long before the ~15-min
// gridlock cadence.
//
// Declared as a var so tests can override without waiting real wall time; the
// production default is const-like (never mutated outside tests).
var forceReapGraceDefault = 90 * time.Second

// deadProcessStaleAfterDefault is the minimum quiet window (hk-mdus1 fast dead-
// process reap) a run must have been silent before the dead-process probe is
// allowed to cancel it. It gates the fast-reap so that a run whose agent process
// exited but whose goroutine is legitimately mid-post-processing is NOT reaped:
// only a run that is BOTH process-dead AND event-silent past this window is
// fast-cancelled, starting the force-reap grace immediately instead of waiting
// out the 10–30 min stale thresholds.
//
// 5 min (widened from an initial 2 min per hk-mdus1 review B2) is deliberately
// sized to comfortably exceed a worst-case COLD-CACHE post-merge build gate.
// That gate (go build + vet ./...; workloop.go emitMergeThenBuild path) runs
// SYNCHRONOUSLY inside the per-run goroutine AFTER the agent's claude pane has
// exited but BEFORE beadRunOne returns and tears the pane down — so the pane PID
// can read dead while a legitimate cold build is still compiling, and that phase
// emits NO heartbeat to refresh lastEventAt. A window shorter than the cold
// build would let the fast reap cancel a healthy run mid-build
// (→ merge_build_failed → bead reopened), which is worse than the leak it
// guards. 5 min clears the observed cold `go build+vet ./...` worst case with
// margin. The truly-wedged case is still caught by the force-reap watchdog at
// forceReapGrace (90 s) once ANY canceller fires, so this wider window does not
// reintroduce the gridlock — it only defers the FAST path, not the backstop.
var deadProcessStaleAfterDefault = 5 * time.Minute

// runStaleState tracks per-run staleness accounting inside StaleWatcher.
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

// forceReapCB and runDeadCB wrap the two optional daemon-wired seams so they can
// be published race-free via atomic.Pointer (they are set post-construction, in
// a two-phase wiring, while the scan goroutine may already be running).
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

	// ── Layer A stall detection (hk-hsp9e) ──────────────────────────────────
	// The three thresholds below feed sentinel.DetectLayerA once per scan per
	// run. The library refuses a zero threshold on purpose, so the daemon gives
	// each one a compiled default here: a detector that goes quiet because a
	// config key is missing is the failure this whole feeder exists to remove.

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

// forceReapFn returns the currently-published force-reap seam, or nil.
func (w *StaleWatcher) forceReapFn() func(core.RunID, *RunHandle) {
	if p := w.forceReapPtr.Load(); p != nil {
		return p.fn
	}
	return nil
}

// runProcessDeadFn returns the currently-published dead-process seam, or nil.
func (w *StaleWatcher) runProcessDeadFn() func(core.RunID, *RunHandle) bool {
	if p := w.runDeadPtr.Load(); p != nil {
		return p.fn
	}
	return nil
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

// beadNoProgressAfter parses a "no_progress_after=<seconds>" or
// "no_progress_after:<seconds>" label from labels and returns the corresponding
// duration. Returns defaultAfter when no such label is present or the value is
// not a positive integer.
//
// This is the false-positive escape hatch for the no-progress clock. The window
// is a wall-clock bound on how long an agent may work without reaching one of
// the events that mark a phase, and that is a property of the WORK, not of the
// fleet. Put this label on a bead whose single phase legitimately runs longer
// than the default.
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

// beadNeverSpawnedTimeout parses a "never_spawned_timeout=<seconds>" or
// "never_spawned_timeout:<seconds>" label from labels and returns the
// corresponding duration.  Returns defaultTimeout when no such label is present
// or the value is ≤0.
//
// Long-running DOT-mode implement beads that legitimately exceed the default
// 30-min never-spawned-reaper ceiling (B9, hk-8gixi) should carry this label
// to prevent false-positive context cancellations.
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

// beadAgentReadyStallThreshold parses an "agent_ready_stall_threshold=<seconds>"
// or "agent_ready_stall_threshold:<seconds>" label from labels and returns the
// corresponding duration.  Returns defaultThreshold when no such label is
// present or the value is ≤0.
//
// The default 3-min AgentReadyStallThreshold assumes agent_ready follows
// launch_initiated within seconds. Reasoning-model harness profiles (e.g. an
// ornith/DGX pi profile) legitimately take ~20 min to reach agent_ready
// (reasoning latency ahead of the first tool_call) — well within the 30-min
// never-spawned reaper, but past the 3-min detector, so it fires a spurious
// agent_ready_stall_detected on every run. Beads dispatched against such a
// profile should carry this label so the detector's window matches the
// profile's real latency instead of alarming on healthy runs (hk-4ir08).
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
	// Seed the optional seams from cfg so tests can supply them at construction;
	// production wires them post-construction via SetForceReap/SetRunProcessDead.
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

	// hk-hsp9e: fold this event into the Layer A phase and the liveness clock
	// the stall detector reads. This is the only place the daemon sees every run
	// event, so it is the only place either can be kept.
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

// watcherOwnAlarm reports whether evType is an alarm this watcher emits ABOUT a
// run rather than a sign the run is alive.
//
// The wildcard observer sees the watcher's own emissions, so without this every
// alarm refreshes the run's liveness clock and suppresses the next detection —
// the detector silences itself the moment it works. That is worth naming rather
// than filtering by hand at each read site, because a future alarm added to
// this file inherits the same trap.
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

// runProgressEvent reports whether evType is evidence the run MOVED, rather
// than evidence that something is still there to report on it.
//
// THE RULE IS ABOUT WHO CAUSED THE EVENT, not about what it is called. An event
// the AGENT caused is progress. An event the DAEMON emits about the run on its
// own timer, or on its own decision, is not — a clock the daemon refreshes on
// its own behalf is a clock that can never run out. That is the same trap
// watcherOwnAlarm names one layer up, and the exclusions below are the same
// rule applied wider:
//
//   - the watcher's own alarms, per watcherOwnAlarm.
//   - agent_heartbeat: a fixed 5-minute timer (handler.RunHeartbeatLoop) that
//     runs for as long as the agent PROCESS exists and says nothing about
//     whether the agent is doing anything. The payload is a session id and a
//     phase string that is always "reasoning", so an agent sitting at an idle
//     prompt beats exactly like one that is working.
//   - no_progress_detected: the daemon emits this when the diff hash did not
//     change between iterations (emitDotNoProgressDetected). It is a statement
//     that the run did NOT move. Counting it as progress lets the one event
//     that means "stuck" reset the stuck clock.
//   - implementer_resumed: the daemon emits this BEFORE it dispatches a resume
//     back-edge (emitDotImplementerResumed). It is the daemon's own nudge, not
//     the agent's answer to it. A loop that keeps resuming and keeps producing
//     nothing would otherwise refresh this clock for as long as the loop runs.
//
// Everything else the daemon stamps with a run id follows the agent — a launch,
// a readiness, a phase completion, a verdict, a terminal — so the default is
// progress. That direction is deliberate: an event type added later counts as
// progress, which can only delay a detection, never invent one.
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

// advanceStallPhase folds one event into the Layer A phase the stall detector
// reads. Caller holds w.mu.
//
// The phase moves forward on every event but one. A launch is a BACKWARD edge,
// and getting that wrong reaps working agents.
//
// A graph run dispatches several nodes under ONE run id, and the
// request-changes back-edge re-launches the implementer after a verdict has
// already fired. Treated as a watermark, that run stays at verdict-fired for
// ever, so the review-stall signature — "the verdict landed and the run never
// finished" — reports every reworking run ten minutes after its FIRST
// request-changes, and kills an implementer that is doing exactly what the
// reviewer asked. A launch says the verdict it was measuring from is spent.
func advanceStallPhase(st *runStaleState, evType core.EventType, at time.Time) {
	switch evType {
	case core.EventTypeLaunchInitiated:
		// The backward edge. The run is dispatching again, so any verdict it was
		// being measured against is spent, and a new node deserves its own
		// chance to be reported and its own kill — a set carried over from an
		// earlier node would silence every later one.
		//
		// The upper bound is not decoration. The bus dispatches each observer on
		// its own goroutine, so a launch folded after a terminal event is
		// possible in principle, and without the bound it would demote a
		// finished run back into the detector's population.
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

	// The Layer A stall pass reads the same registry snapshot the loop above
	// walked, so both halves of one tick judge the same set of runs.
	w.stallPass(ctx, now, handles)

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
		// Apply per-bead label overrides if present.
		st = &runStaleState{
			nextEmitAfter:            beadStaleAfter(handle.Labels, w.cfg.StaleAfter),
			nextNoProgressAfter:      beadNoProgressAfter(handle.Labels, w.cfg.NoProgressAfter),
			neverSpawnedTimeout:      beadNeverSpawnedTimeout(handle.Labels, w.cfg.NeverSpawnedReaperTimeout),
			agentReadyStallThreshold: beadAgentReadyStallThreshold(handle.Labels, w.cfg.AgentReadyStallThreshold),
		}
		w.states[runID] = st
	} else {
		// observe() may have initialised the state before checkRun had a chance
		// to do so; it lacks access to handle.Labels and leaves these fields at
		// their zero value.  Lazily apply the per-bead label overrides here.
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
	// Hydrate BeadID from the RunHandle (available even before first event).
	if st.beadID == "" {
		st.beadID = handle.BeadID
	}

	// hk-mdus1 FORCE-REAP WATCHDOG (re-entrant backstop). The one-shot auto-
	// cancellers (kill-consumer, never-spawned, fast dead-process) call
	// handle.Cancel() once and then rely on the per-run goroutine unwinding to
	// free the concurrency slot via its deferred Unregister. When the goroutine
	// is parked on a wait that never observes runCtx (br/git subprocess on
	// context.Background(), a mutex, a merge-build), Cancel never unwinds it and
	// the RunHandle — and its slot in RunRegistry.Len()/LenForQueue — leaks
	// forever, eventually closing the global gate and gridlocking the fleet.
	// This watchdog closes that leak: forceReapGrace after Cancel was invoked,
	// if the run is STILL registered (it is — we are iterating a registry
	// snapshot), force-Unregister the handle directly and drive its queue item
	// terminal. Runs FIRST so a wedged slot is reclaimed regardless of which
	// canceller fired. Covers the Cancel==nil case: cancelledAt is recorded even
	// when Cancel was nil, so the grace still elapses and the slot is freed.
	if !st.cancelledAt.IsZero() && !st.forceReapFired && now.Sub(st.cancelledAt) >= w.cfg.ForceReapGrace {
		st.forceReapFired = true
		beadIDForReap := st.beadID
		grace := now.Sub(st.cancelledAt)
		w.mu.Unlock()
		w.forceReap(ctx, runID, beadIDForReap, handle, grace)
		return
	}

	// hk-mdus1 FAST DEAD-PROCESS REAP. When the run's agent process/pane is
	// already gone (probed via the substrate's #{pane_pid} liveness) AND the run
	// has been event-silent past DeadProcessStaleAfter, cancel it immediately
	// instead of waiting out the 10–30 min stale thresholds. The silence gate
	// protects normal post-agent-exit processing (merge/build emit their own
	// events, refreshing lastEventAt) from being reaped. Cancelling here records
	// cancelledAt, which starts the force-reap grace above so a wedged
	// goroutine is still force-Unregistered even if Cancel does not unwind it.
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
		now.Sub(st.launchInitiatedAt) > st.agentReadyStallThreshold {
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
	//
	// The effective timeout is st.neverSpawnedTimeout (initialised at first
	// event/check from the bead's "never_spawned_timeout=<seconds>" label;
	// falls back to w.cfg.NeverSpawnedReaperTimeout when no label is present).
	// Long-running DOT-mode implement beads should carry the label to avoid
	// the fleet-wide simultaneous cancellation observed in B9 (hk-8gixi).
	launchInitiatedAt := st.launchInitiatedAt
	agentReadySeen := st.agentReadySeen
	neverSpawnedFired := st.neverSpawnedFired
	if st.launchInitiatedSeen && !agentReadySeen && !neverSpawnedFired && !launchInitiatedAt.IsZero() &&
		now.Sub(launchInitiatedAt) > st.neverSpawnedTimeout {
		st.neverSpawnedFired = true
		// hk-mdus1: start the force-reap grace clock (covers Cancel==nil too).
		if st.cancelledAt.IsZero() {
			st.cancelledAt = now
		}
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
		now.Sub(lastLaunchInitiatedAt) > st.neverSpawnedTimeout {
		st.neverSpawnedFired = true
		// hk-mdus1: start the force-reap grace clock (covers Cancel==nil too).
		if st.cancelledAt.IsZero() {
			st.cancelledAt = now
		}
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

	// The NO-PROGRESS clock. The quiet window above measures silence, and the
	// daemon breaks the silence itself — agent_heartbeat every 5 min for as long
	// as the agent process exists — so a run that wedges with its process alive
	// is never quiet and run_stale never fires for it. This second clock
	// measures from the last event that shows the run MOVED (runProgressEvent),
	// so the daemon's own beat cannot refresh it. A run with no progress event
	// yet is measured from its start, which is the whole of its life so far.
	//
	// The window is far wider than the quiet one because the two are answering
	// different questions: an agent that is thinking is legitimately silent for
	// tens of minutes, and cancelling it costs the work it had done. See
	// staleWatchNoProgressAfter for how the default was sized.
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
	// noProgressSeconds is reported in its OWN payload field. age_seconds is
	// spec'd as the seconds since the run last produced any bus event
	// (core.RunStalePayload), and a wedged-but-beating run is the exact case
	// where the two differ, so overwriting it would make the payload disagree
	// with itself precisely when a reader most needs it. Both numbers are true
	// and the pair is the diagnosis: a large no_progress_seconds beside a small
	// age_seconds and last_event_type=agent_heartbeat is this failure.
	var noProgressSeconds *int64
	if wedged {
		secs := int64(noProgressAge.Seconds())
		noProgressSeconds = &secs
	}

	// Stale threshold crossed — capture snapshot fields under the lock.
	st.emitCount++
	emitCount := st.emitCount
	// core.RunStalePayload.Valid requires AgeSeconds > 0, and a no-progress
	// emission is the one case that can round to zero: the run is beating, so
	// the last event may be under a second old when the scan tick lands. An
	// invalid payload is dropped, which would lose exactly the emission this
	// clock exists to produce. Floor at one second — the truncation is under a
	// second and only ever moves the number away from zero.
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
	// hk-tn36: kill-consumer backstop fires on the FIRST run_stale emission.
	// Capture and set the flag under the same lock region as emitCount so the
	// backstop fires exactly once even under concurrent scan ticks.
	//
	// THE NO-PROGRESS CLOCK NEVER ARMS IT. Only the quiet window cancels, which
	// is exactly what it did before this clock existed. A cancel destroys the
	// work the run has already done, so arming a canceller from a new clock
	// requires the confidence that its window cannot fire early — and the
	// harnesses this clock uniquely covers (codex, pi) have far too few recorded
	// runs to bound their tail. Reporting is what the finding asked for: a run
	// that is not moving must stop reporting itself healthy. Cancelling it is a
	// separate escalation, and it is not evidenced yet. See
	// staleWatchNoProgressAfter.
	shouldKillConsumer := quiet && !st.killConsumerFired
	if shouldKillConsumer {
		st.killConsumerFired = true
		// hk-mdus1: start the force-reap grace clock. Recorded even though
		// killConsumerBackstop may find Cancel==nil — the force-reap watchdog
		// then frees the leaked slot after the grace regardless.
		if st.cancelledAt.IsZero() {
			st.cancelledAt = now
		}
	}
	// Double the window for the next emission (exponential backoff).
	// Use effectiveThreshold as the base so that the reviewer-launch gate
	// floor is accounted for in the schedule: if the gate raised the
	// threshold to 30 min, the next window is 60 min (not 20 min).
	//
	// Each clock backs off only when it was the one that fired. A shared
	// backoff would let a wedged-but-beating run widen the quiet window, which
	// is the window that catches a run that stops emitting altogether.
	if quiet {
		st.nextEmitAfter = effectiveThreshold * 2
	}
	if wedged {
		st.nextNoProgressAfter *= 2
	}
	w.mu.Unlock()

	// HC-064..HC-067 / hk-xrygh: before emitting run_stale, drive the session
	// lifecycle FSM to StateFailed(silent_hang) if the machine is in a live
	// (non-terminal) state. This ensures the Ready→Failed(silent_hang)
	// transition event fires BEFORE run_stale, satisfying the acceptance
	// criterion that "run_stale carries the lifecycle snapshot" and that the
	// silent-hang is visible as a deterministic FSM event first.
	//
	// This runs on BOTH clocks, so the reason string names the clock that
	// fired. On a no-progress emission the run is beating and ageSeconds is
	// tiny — often the floored 1 — so reporting it here would write "silent for
	// 1s" into the durable transition history for a run that has not moved in
	// two hours. That is the same self-contradicting record NoProgressSeconds
	// exists to prevent, one layer up.
	staleReason := fmt.Sprintf("session silent for %ds", ageSeconds)
	if noProgressSeconds != nil {
		staleReason = fmt.Sprintf("session made no progress for %ds (last event %s, %ds ago)",
			*noProgressSeconds, lastEventType, ageSeconds)
	}
	var lifecycleStateStr, lifecycleEnteredAtStr string
	if m := handle.GetMachine(); m != nil {
		cur := m.Current()
		if !cur.IsTerminal() {
			// Drive to StateFailed(silent_hang). The transition call is
			// idempotent: if the machine is already in a terminal state (due
			// to a concurrent path) the error is silently ignored.
			from := cur
			if tErr := m.Transition(hclifecycle.StateFailed, hclifecycle.ReasonSilentHang,
				"run_stale", staleReason); tErr == nil {
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

// forceReap is the re-entrant force-reap watchdog action (hk-mdus1). It is
// invoked by checkRun when a run remains registered in the RunRegistry
// forceReapGrace after its per-run Cancel was first invoked — i.e. Cancel did
// NOT unwind the goroutine (parked on a non-runCtx wait) or Cancel was nil.
//
// Two things happen, in order:
//
//  1. The optional daemon-wired ForceReap seam runs (when present): it emits a
//     terminal run_failed for the wedged run and drives its owning queue item
//     terminal via evaluateGroupAdvanceWithOutcome — work the wedged goroutine
//     can no longer do itself, so absent this the queue group would stall
//     forever (hk-mdus1 orphan-item path).
//  2. The RunHandle is force-Unregistered directly. This is the LOAD-BEARING
//     step: it frees the concurrency slot the leaked handle occupied in
//     RunRegistry.Len()/LenForQueue, re-opening the global gate. It happens
//     even when the seam is nil (unit-test mode), so the anti-gridlock
//     guarantee never depends on the queue-advance wiring.
//
// The parked goroutine may still be alive; if it ever unwinds its deferred
// Unregister is a harmless no-op (Unregister is idempotent). A double terminal
// event (this one plus a late one from the goroutine) is possible but benign
// and vanishingly rare (the goroutine is, by construction, wedged).
//
// Bead ref: hk-mdus1.
func (w *StaleWatcher) forceReap(_ context.Context, runID core.RunID, beadID core.BeadID, handle *RunHandle, sinceCancel time.Duration) {
	fmt.Fprintf(os.Stderr,
		"daemon: stalewatch: FORCE-REAP: bead %s run %s: still registered %s after Cancel — force-Unregistering leaked slot (hk-mdus1)\n",
		beadID, runID, sinceCancel.Round(time.Second))
	// Step 1: terminal event + queue-item advance (best-effort; nil in tests).
	if fn := w.forceReapFn(); fn != nil {
		fn(runID, handle)
	}
	// Step 2: LOAD-BEARING — free the concurrency slot unconditionally.
	w.cfg.Registry.Unregister(runID)
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
	if emitErr := w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeLaunchStallDetected, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: emit launch_stall_detected for run %s: %v\n", runID, emitErr)
	}
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
	if emitErr := w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeAgentReadyStallDetected, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stalewatch: emit agent_ready_stall_detected for run %s: %v\n", runID, emitErr)
	}
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
