package daemon

// quiesce.go — QuiesceArbiter: daemon quiesce-mode and wake-trigger (hk-jeby, M1).
//
// The QuiesceArbiter is the policy layer that sits on top of the GenuineDrain
// oracle (M0 / hk-95uf).  When the oracle returns DRAINED the arbiter:
//
//  1. Writes per-session .sleeping.<session_id> markers under .harmonik/.
//  2. Sends a comms park signal to each known LLM session (captain + crew).
//  3. Registers per-session wake triggers: when a trigger fires the arbiter
//     nudges the appropriate pane via the stored pane target.
//
// # Wake routing table (Risk 3)
//
// Events → target mapping is NEVER fleet-wide; each trigger names one session:
//
//   - QueueStore.WakeCh() + pending item for queue Q → crew bound to Q.
//   - epic_completed                                 → captain (Risk 4).
//   - agent_message{to="captain"}                   → captain (Risk 4).
//   - wake --all (explicit-only)                    → NOT implemented here;
//     that is the operator CLI surface, not an automatic trigger.
//
// # Wake reliability (Risk 2)
//
// The pane target for each session is captured once and stored in sleepRecord:
//   - Crew sessions: crew.Record.Handle + ".0"  (matches perRunSubstrate.cachedPaneTarget convention).
//   - Captain: resolved via resolveCaptainTarget() — keeper.ResolveTmuxTarget
//     (canonical EvalSymlinks hash, has-session probe, returns "<session>:agent")
//     first, then a bare-"captain" exact-match fallback, then the convention
//     "<session>:agent" as last resort (hk-fv40; replaces the old hard-coded :0.0).
//
// A max-sleep-duration FAILSAFE auto-wakes every session that has been asleep
// longer than maxSleepDuration (default 4 h).  This is the insurance mechanism:
// if a wake trigger is missed or a new class of work appears that no trigger
// covers, the session will self-recover within the ceiling.
//
// Bead ref: hk-jeby (M1 of hk-rl4b sleep-wake).
// Spec ref: codename:sleep-wake (the specs live in the kerf bench; this
// implementation provides the M1 daemon-side contract).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/schedule"
)

const (
	// quiesceArbiterPollInterval is how often the arbiter re-evaluates GenuineDrain.
	// Conservative: 30 s is fast enough to detect new work quickly without burning
	// CPU on continuous br-ready polling.
	quiesceArbiterPollInterval = 30 * time.Second

	// quiesceArbiterMaxSleepDuration is the hard auto-wake ceiling (Risk 2
	// failsafe).  Any session that has been asleep longer than this is nudged
	// unconditionally, regardless of the drain state, so the fleet never sleeps
	// past a fixed wall-clock horizon.
	quiesceArbiterMaxSleepDuration = 4 * time.Hour

	// sleepingMarkerDir is the directory under .harmonik/ where per-session
	// .sleeping.<session_id> marker files are written.  The directory is created
	// lazily; its absence simply means no sessions are sleeping.
	sleepingMarkerDir = ".harmonik"

	// fleetSleepingMarker is the file written to .harmonik/ on `harmonik sleep`
	// and removed on `harmonik wake --all`. External agents — Claude Code harness
	// crons, scripts, etc. — can check for this file via `harmonik sleep-gate`
	// (exit 0 = sleeping → suppress; exit 1 = awake → proceed). hk-xjr1n.
	fleetSleepingMarker = ".fleet-sleeping"

	// captainAgentName is the conventional captain agent name used by
	// lifecycle.TmuxSessionName and crew registries.
	captainAgentName = "captain"

	// watchAgentName is the conventional watch agent name (WE5): the always-on
	// triage-and-relay session that sits between the event bus and the captain.
	watchAgentName = "watch"
)

// SleepSource identifies who initiated a park (hk-caaf / codename:fleet-state).
// Operator intent outranks the event-reflex wake: an operator PARK must not be
// auto-woken by a stray queue submit, whereas a captain/auto park is the
// event-reflex sleep and may be woken by the normal wake triggers.
type SleepSource string

const (
	// SleepSourceOperator marks a park initiated by an explicit operator command
	// (e.g. `harmonik sleep`). Operator intent is sticky against auto-wake.
	SleepSourceOperator SleepSource = "operator"
	// SleepSourceCaptain marks a park initiated by the daemon's own drain-detect
	// event reflex (the captain-class auto-park). These are the parks the wake
	// triggers are designed to lift.
	SleepSourceCaptain SleepSource = "captain"
)

// SleepLevel is the depth of a park (hk-caaf / codename:fleet-state):
//
//	L0 — abandon      (lightest: no resumption intent recorded)
//	L1 — drain        (default: park once the current work drains)
//	L2 — handoff      (park with an intent-preserving handoff written)
//	L3 — finish-lane  (deepest: hold until the whole lane completes)
type SleepLevel string

const (
	SleepLevelAbandon    SleepLevel = "L0"
	SleepLevelDrain      SleepLevel = "L1"
	SleepLevelHandoff    SleepLevel = "L2"
	SleepLevelFinishLane SleepLevel = "L3"
)

// defaultSleepSource / defaultSleepLevel are the backward-compatible defaults
// applied when an on-disk marker predates the source/level fields (hk-caaf).
// A marker with no source is treated as an operator park (the safe, sticky
// interpretation — never auto-wake something we cannot prove was an auto-park);
// a marker with no level is treated as an L1 drain park (the common case).
const (
	defaultSleepSource = SleepSourceOperator
	defaultSleepLevel  = SleepLevelDrain
)

// sleepMarker is the on-disk shape of .harmonik/.sleeping.<session_id>.
// JSON tags are stable; new fields MUST default cleanly so a marker written by
// an older daemon (session_id + parked_at only) still round-trips.
type sleepMarker struct {
	SessionID string      `json:"session_id"`
	ParkedAt  string      `json:"parked_at"`
	Source    SleepSource `json:"source"`
	Level     SleepLevel  `json:"level"`
}

// normalize applies the backward-compatible defaults for any field a legacy
// marker omitted, so callers always see a fully-populated record (hk-caaf).
func (m *sleepMarker) normalize() {
	if m.Source == "" {
		m.Source = defaultSleepSource
	}
	if m.Level == "" {
		m.Level = defaultSleepLevel
	}
}

// paneNudger is the minimal interface the QuiesceArbiter needs from the tmux
// adapter.  Using a narrow interface lets tests inject a simple stub without
// implementing the full tmuxpkg.Adapter (which has ~14 methods).
type paneNudger interface {
	SendKeysEnter(ctx context.Context, paneTarget string) error
}

// QuiesceArbiterConfig bundles the dependencies of the QuiesceArbiter.
// All fields are required unless documented as optional.
type QuiesceArbiterConfig struct {
	// ProjectDir is the harmonik project root.  REQUIRED.
	ProjectDir string

	// ProjectHash is the pre-computed project hash used to derive tmux session
	// names via lifecycle.TmuxSessionName.  REQUIRED.
	ProjectHash core.ProjectHash

	// Adapter delivers Enter-key nudges to tmux session panes.  When nil the
	// arbiter skips pane nudging (tests that do not need real tmux can leave
	// this nil; the sleep/wake state machine still runs).
	//
	// In production, pass the *tmuxpkg.OSAdapter (or any tmuxpkg.Adapter)
	// obtained from cfg.Substrate via the substrateWithAdapter interface.
	Adapter paneNudger

	// QueueStore is the queue store used to determine which named queue has new
	// pending items (wake routing for crew sessions).  REQUIRED.
	QueueStore *QueueStore

	// CommsBus, when non-nil, is used to emit park/wake comms messages.
	// Optional: when nil the comms-send step is skipped (pane nudge is still
	// issued on wake).
	CommsBus eventbus.CommsMessageEmitter

	// ScheduleStore, when non-nil, is used to suspend all enabled schedule jobs
	// on sleep and restore them symmetrically on wake. Optional: when nil the
	// schedule-suspend step is skipped. Set via SetScheduleStore after construction.
	ScheduleStore *schedule.Store

	// PollInterval overrides quiesceArbiterPollInterval for tests.  Zero → use default.
	PollInterval time.Duration

	// MaxSleepDuration overrides quiesceArbiterMaxSleepDuration for tests.  Zero → use default.
	MaxSleepDuration time.Duration
}

// sessionSleepRecord tracks the sleep state for one LLM session.
type sessionSleepRecord struct {
	agentName  string
	queueName  string // queue this session services (empty = captain)
	paneTarget string // tmux pane target for Enter-key nudge
	sessionID  string // for .sleeping.<session_id> marker file
	sleptAt    time.Time
	source     SleepSource // who initiated the park (hk-caaf)
	level      SleepLevel  // depth of the park (hk-caaf)
}

// QuiesceArbiter polls GenuineDrain and manages fleet sleep/wake.
type QuiesceArbiter struct {
	cfg QuiesceArbiterConfig

	mu       sync.Mutex
	sleeping map[string]sessionSleepRecord // agentName → record (non-empty means parked)
	drain    *DrainDetector                // SS-INV-005 veto gate (P1-c, hk-zqb3); nil = gate skipped

	// wakeC is the internal channel for event-triggered wakes.
	wakeC chan wakeSignal
}

// wakeSignal carries the routing key for a triggered wake event.
type wakeSignal struct {
	// queueName, when non-empty, routes the wake to the crew bound to that queue.
	queueName string
	// captainWake, when true, routes the wake to the captain regardless of queue.
	captainWake bool
	// agentName, when non-empty, routes the wake to the named sleeping agent
	// directly (e.g. "watch" — WE5).  Checked only when captainWake is false
	// and queueName is empty.
	agentName string
	// reason is a human-readable label for logging.
	reason string
}

// NewQuiesceArbiter constructs a QuiesceArbiter from cfg.  The caller must
// invoke Subscribe before sealing the bus, then Start after sealing.
func NewQuiesceArbiter(cfg QuiesceArbiterConfig) *QuiesceArbiter {
	return &QuiesceArbiter{
		cfg:      cfg,
		sleeping: make(map[string]sessionSleepRecord),
		wakeC:    make(chan wakeSignal, 32),
	}
}

// Subscribe registers the arbiter's event consumers on bus.  MUST be called
// before bus.Seal() — exactly like StaleWatcher, HandlerPausePolicyGoroutine, etc.
//
// Registered subscriptions:
//
//  1. epic_completed (Risk 4) → wake captain.
//  2. agent_message (Risk 4)  → wake captain when To == "captain".
func (a *QuiesceArbiter) Subscribe(bus eventbus.EventBus) error {
	epicSub := core.Subscription{
		ConsumerID:    "quiesce-arbiter-epic-completed",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeEpicCompleted: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: a.handleEpicCompleted,
	}
	if _, err := bus.Subscribe(epicSub); err != nil {
		return fmt.Errorf("QuiesceArbiter.Subscribe: epic_completed: %w", err)
	}

	msgSub := core.Subscription{
		ConsumerID:    "quiesce-arbiter-agent-message",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				"agent_message": {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: a.handleAgentMessage,
	}
	if _, err := bus.Subscribe(msgSub); err != nil {
		return fmt.Errorf("QuiesceArbiter.Subscribe: agent_message: %w", err)
	}

	return nil
}

// Start launches the arbiter's background goroutine.  MUST be called after
// bus.Seal() — the goroutine runs until ctx is cancelled.
//
// Before the loop starts, Start reconciles any orphaned on-disk sleep markers
// left behind by a daemon that died mid-sleep (hk-x03v): without this, the
// in-memory sleeping map and the max-sleep failsafe are lost on restart while
// the .sleeping.<sid> markers persist, so the keeper gates stay suppressed
// indefinitely. The reconcile re-loads each orphaned marker into the in-memory
// map (so the wake triggers and the failsafe cover it again).
//
// Pattern: same as staleWatcher.StartWatcher.
func (a *QuiesceArbiter) Start(ctx context.Context) {
	a.reconcileOrphanedMarkers()
	go a.run(ctx)
}

// reconcileOrphanedMarkers re-loads on-disk .sleeping.<sid> markers into the
// in-memory sleeping map at daemon boot (hk-x03v / codename:fleet-state).
//
// If the daemon dies while sessions are parked, the in-memory map and the
// max-sleep failsafe timer are gone, but the marker files persist — so the
// keeper's IsSleeping gate keeps suppressing those sessions with nothing left to
// ever wake them. This pass restores the map (keeping the ORIGINAL parked_at as
// sleptAt, so the 4h max-sleep failsafe measures from the real park time and a
// marker already past the ceiling is woken on the first tick) and resolves a
// fresh pane target so the nudge can land.
//
// A marker whose session cannot be mapped to a known session (no matching crew
// record and not the captain sentinel) is still re-loaded under its session_id
// so the failsafe can eventually clear its file — but without a pane target the
// nudge is skipped (the marker removal alone lifts the keeper suppression).
//
// Best-effort: any per-marker error is logged and skipped; the daemon never
// fails to start over a bad marker.
func (a *QuiesceArbiter) reconcileOrphanedMarkers() {
	if a.cfg.ProjectDir == "" {
		return
	}
	dir := filepath.Join(a.cfg.ProjectDir, sleepingMarkerDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "daemon: quiesce: reconcile: read %q: %v\n", dir, err)
		}
		return // no .harmonik dir → nothing parked
	}

	// Build sessionID → crew record index for pane/queue/agent resolution.
	crewBySID := make(map[string]crew.Record)
	for _, r := range a.listCrewRecords() {
		if r.SessionID != "" {
			crewBySID[r.SessionID] = r
		}
	}

	const markerPrefix = ".sleeping."
	var restored int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) <= len(markerPrefix) || name[:len(markerPrefix)] != markerPrefix {
			continue
		}
		path := filepath.Join(dir, name)
		marker, readErr := a.readSleepMarker(path)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: quiesce: reconcile: parse %q: %v\n", path, readErr)
			continue
		}
		sessionID := marker.SessionID
		if sessionID == "" {
			// Recover the session id from the filename when the body omitted it.
			sessionID = name[len(markerPrefix):]
		}

		// sleptAt = the ORIGINAL park time so the failsafe clock continues.
		sleptAt := time.Now()
		if marker.ParkedAt != "" {
			if t, perr := time.Parse(time.RFC3339, marker.ParkedAt); perr == nil {
				sleptAt = t
			}
		}

		// Resolve agentName / queue / pane for this session.
		var agentName, queueName, paneTarget string
		if r, ok := crewBySID[sessionID]; ok {
			agentName = r.Name
			queueName = r.Queue
			if r.Handle != "" {
				paneTarget = r.Handle + ".0"
			}
		} else if sessionID == "captain-session" {
			agentName = captainAgentName
			paneTarget = a.resolveCaptainTarget()
		} else {
			// Unknown session: key the map by session id so the failsafe can still
			// clear the marker; no pane target → nudge is skipped.
			agentName = sessionID
		}

		a.mu.Lock()
		if _, already := a.sleeping[agentName]; already {
			a.mu.Unlock()
			continue
		}
		a.sleeping[agentName] = sessionSleepRecord{
			agentName:  agentName,
			queueName:  queueName,
			paneTarget: paneTarget,
			sessionID:  sessionID,
			sleptAt:    sleptAt,
			source:     marker.Source,
			level:      marker.Level,
		}
		a.mu.Unlock()
		restored++
	}

	if restored > 0 {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: reconcile: re-loaded %d orphaned sleep marker(s) into the failsafe\n", restored)
	}
}

// run is the main loop of the QuiesceArbiter.
func (a *QuiesceArbiter) run(ctx context.Context) {
	poll := a.cfg.PollInterval
	if poll <= 0 {
		poll = quiesceArbiterPollInterval
	}
	maxSleep := a.cfg.MaxSleepDuration
	if maxSleep <= 0 {
		maxSleep = quiesceArbiterMaxSleepDuration
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	var submitWakeC <-chan struct{}
	if a.cfg.QueueStore != nil {
		submitWakeC = a.cfg.QueueStore.WakeCh()
	}

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			a.tick(ctx, maxSleep)

		case <-submitWakeC:
			// Queue submission: check all queues for pending items and wake
			// the crew bound to each queue that has pending work.
			a.handleQueueSubmit(ctx)

		case sig := <-a.wakeC:
			a.executeWake(ctx, sig)
		}
	}
}

// tick runs one drain-check + failsafe-wake cycle.
func (a *QuiesceArbiter) tick(ctx context.Context, maxSleep time.Duration) {
	// Max-sleep failsafe (Risk 2): unconditionally wake sessions that have slept
	// past the ceiling, regardless of drain state.  Runs even when Drain is nil.
	a.mu.Lock()
	var expired []sessionSleepRecord
	for _, rec := range a.sleeping {
		if time.Since(rec.sleptAt) >= maxSleep {
			expired = append(expired, rec)
		}
	}
	a.mu.Unlock()

	for _, rec := range expired {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: max-sleep failsafe: waking %q (slept %v)\n",
			rec.agentName, time.Since(rec.sleptAt).Round(time.Second))
		a.nudgePane(ctx, rec.agentName, rec.paneTarget)
		a.clearSleepMarker(rec.sessionID)
		a.mu.Lock()
		delete(a.sleeping, rec.agentName)
		a.mu.Unlock()
	}
}

// parkAllSessions writes sleep markers and sends park comms signals to every
// known LLM session (captain + all crews) that is not already sleeping.
// source/level record the park provenance/depth on each marker (hk-caaf).
func (a *QuiesceArbiter) parkAllSessions(ctx context.Context, source SleepSource, level SleepLevel) {
	records := a.listCrewRecords()

	// Captain: resolve the live pane target (hk-fv40 / codename:fleet-state).
	// The old code hard-coded "<convention-session>:0.0", which missed whenever
	// the live captain session is the BARE name "captain" — the wake nudge then
	// landed on a dead pane and the session stayed asleep until the 4h failsafe
	// (which re-nudged the SAME wrong pane). resolveCaptainTarget now probes
	// liveness and returns the target of whichever session is actually live.
	captainTarget := a.resolveCaptainTarget()
	a.parkSession(ctx, captainAgentName, "", "captain-session", captainTarget, source, level)

	// Each crew session.
	for _, r := range records {
		if r.Handle == "" || r.SessionID == "" {
			continue
		}
		pane := r.Handle + ".0"
		a.parkSession(ctx, r.Name, r.Queue, r.SessionID, pane, source, level)
	}
}

// resolveCaptainTarget determines the tmux target for the captain's wake nudge
// (hk-fv40 / codename:fleet-state).
//
// Resolution order:
//  1. keeper.ResolveTmuxTarget(projectDir, "captain", ...) — derives the
//     conventional "harmonik-<hash>-captain" session, probes it with
//     `tmux has-session`, and returns "<session>:agent" (the AGENT window's
//     active pane) when live. This is the same idiom the keeper itself uses.
//  2. Bare "captain" session fallback — the live captain is sometimes the bare
//     session name "captain" (not the hashed name); probe it directly and use
//     it when live.
//  3. Convention-derived "<session>:agent" — last resort so the failsafe still
//     has a plausible target even when no probe confirmed a live session
//     (e.g. tmux unavailable in this environment).
func (a *QuiesceArbiter) resolveCaptainTarget() string {
	if a.cfg.ProjectDir != "" {
		if t := keeper.ResolveTmuxTarget(a.cfg.ProjectDir, captainAgentName, "", nil); t != "" {
			return t
		}
	}
	// Fallback: the bare "captain" session (the comms-wake pane-mismatch case).
	if tmuxHasSession(captainAgentName) {
		return captainAgentName
	}
	// Last resort: convention-derived name with the AGENT window's active pane.
	if a.cfg.ProjectDir != "" {
		return keeper.HarmonikSessionName(a.cfg.ProjectDir, captainAgentName) + ":agent"
	}
	return lifecycle.TmuxSessionName(a.cfg.ProjectHash, captainAgentName) + ":agent"
}

// tmuxHasSession reports whether a tmux session whose name EXACTLY equals name
// is live, via `tmux has-session -t "=<name>"`. The "=" anchor forces an exact
// match (mirrors keeper.tmuxSessionLive, which is unexported). A non-zero exit
// (absent session / no tmux server) reports false.
func tmuxHasSession(name string) bool {
	if name == "" {
		return false
	}
	//nolint:gosec // G204: name is a fixed constant (captainAgentName) or a derived session name.
	cmd := exec.CommandContext(context.Background(), "tmux", "has-session", "-t", "="+name)
	return cmd.Run() == nil
}

// parkSession parks one session: writes the sleep marker file and sends a comms
// park signal.  No-op when the session is already sleeping.
func (a *QuiesceArbiter) parkSession(ctx context.Context, agentName, queueName, sessionID, paneTarget string, source SleepSource, level SleepLevel) {
	if source == "" {
		source = defaultSleepSource
	}
	if level == "" {
		level = defaultSleepLevel
	}
	a.mu.Lock()
	if _, already := a.sleeping[agentName]; already {
		a.mu.Unlock()
		return
	}
	rec := sessionSleepRecord{
		agentName:  agentName,
		queueName:  queueName,
		paneTarget: paneTarget,
		sessionID:  sessionID,
		sleptAt:    time.Now(),
		source:     source,
		level:      level,
	}
	a.sleeping[agentName] = rec
	a.mu.Unlock()

	// Write .sleeping.<session_id> marker.
	if sessionID != "" && a.cfg.ProjectDir != "" {
		a.writeSleepMarker(sessionID, source, level)
	}

	// Emit comms park signal (best-effort; log on failure; never fatal).
	if a.cfg.CommsBus != nil {
		parkBody := fmt.Sprintf(`{"type":"park","reason":"drain_detected","drained_at":%q}`, time.Now().UTC().Format(time.RFC3339))
		_, emitErr := a.cfg.CommsBus.EmitAgentMessage(ctx, core.AgentMessagePayload{
			From:  "daemon",
			To:    agentName,
			Topic: "park",
			Body:  parkBody,
		})
		if emitErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: quiesce: park comms send to %q: %v\n", agentName, emitErr)
		}
	}
}

// handleQueueSubmit is called when a queue submission arrives (via WakeCh).
// It checks each queue for pending items and wakes the crew assigned to that queue.
//
// Two routing paths:
//  1. Crew registry: if a crew.Record exists for the queue, use its name for
//     the log message.
//  2. Sleeping-map fallback: executeWake routes by queueName regardless of
//     whether the crew registry is populated, so pending items always wake
//     sleeping sessions bound to that queue.
func (a *QuiesceArbiter) handleQueueSubmit(ctx context.Context) {
	if a.cfg.QueueStore == nil {
		return
	}
	queues := a.cfg.QueueStore.AllQueues()
	records := a.listCrewRecords()

	// Build queueName → crew name index (for log messages only).
	queueToCrewName := make(map[string]string, len(records))
	for _, r := range records {
		if r.Queue != "" {
			queueToCrewName[r.Queue] = r.Name
		}
	}

	for qName, q := range queues {
		if q == nil || qName == "" {
			continue
		}
		// Check for pending items in this queue.
		hasPending := false
		for gi := range q.Groups {
			for _, item := range q.Groups[gi].Items {
				if item.Status == queue.ItemStatusPending {
					hasPending = true
					break
				}
			}
			if hasPending {
				break
			}
		}
		if !hasPending {
			continue
		}

		// Wake any session sleeping for this queue.
		// executeWake matches sleeping records by queueName; crew registry is
		// optional — used only to enrich the log message.
		crewName, ok := queueToCrewName[qName]
		var reason string
		if ok {
			reason = fmt.Sprintf("queue %q has pending items (crew %q)", qName, crewName)
		} else {
			reason = fmt.Sprintf("queue %q has pending items", qName)
		}
		a.executeWake(ctx, wakeSignal{queueName: qName, reason: reason})
	}
}

// handleEpicCompleted is the event handler for epic_completed (Risk 4 / captain interlock).
func (a *QuiesceArbiter) handleEpicCompleted(ctx context.Context, evt core.Event) error {
	// Route to captain — epic completion always wakes the captain.
	select {
	case a.wakeC <- wakeSignal{captainWake: true, reason: "epic_completed"}:
	default:
		// Channel full: best-effort; the tick's failsafe catches any missed wakes.
	}
	return nil
}

// handleAgentMessage is the event handler for agent_message (Risk 4 / captain interlock).
// Wakes the captain when the message is directed at the captain; wakes the watch
// session when the message is directed at "watch" (WE5 — parked-watch wake path).
// All other destinations are silently ignored (no fleet-wide wake).
func (a *QuiesceArbiter) handleAgentMessage(ctx context.Context, evt core.Event) error {
	var payload core.AgentMessagePayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return nil // malformed payload; skip silently
	}
	switch payload.To {
	case captainAgentName:
		select {
		case a.wakeC <- wakeSignal{captainWake: true, reason: fmt.Sprintf("agent_message from %q to captain", payload.From)}:
		default:
			// Channel full: best-effort.
		}
	case watchAgentName:
		// WE5: a message directed at the watch wakes the parked watch session.
		select {
		case a.wakeC <- wakeSignal{agentName: watchAgentName, reason: fmt.Sprintf("agent_message from %q to watch", payload.From)}:
		default:
			// Channel full: best-effort.
		}
	}
	return nil
}

// executeWake wakes the session identified by sig.
//
// Wake routing:
//   - sig.captainWake → wake captain (if sleeping).
//   - sig.queueName non-empty → wake crew for that queue (if sleeping).
//   - sig.agentName non-empty → wake the named sleeping agent directly (WE5).
func (a *QuiesceArbiter) executeWake(ctx context.Context, sig wakeSignal) {
	a.mu.Lock()
	var targets []sessionSleepRecord
	if sig.captainWake {
		if rec, ok := a.sleeping[captainAgentName]; ok {
			targets = append(targets, rec)
		}
	} else if sig.queueName != "" {
		for _, rec := range a.sleeping {
			if rec.queueName == sig.queueName {
				targets = append(targets, rec)
				break
			}
		}
	} else if sig.agentName != "" {
		if rec, ok := a.sleeping[sig.agentName]; ok {
			targets = append(targets, rec)
		}
	}
	// Remove from sleeping map before releasing lock so concurrent wakes don't double-nudge.
	for _, rec := range targets {
		delete(a.sleeping, rec.agentName)
	}
	a.mu.Unlock()

	for _, rec := range targets {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: waking %q (%s)\n", rec.agentName, sig.reason)
		a.nudgePane(ctx, rec.agentName, rec.paneTarget)
		a.clearSleepMarker(rec.sessionID)
	}
}

// nudgePane sends an Enter key to paneTarget to wake a parked session.
// Best-effort: errors are logged but never fatal (the max-sleep failsafe
// provides an upper bound on how long a wake failure can persist).
func (a *QuiesceArbiter) nudgePane(ctx context.Context, agentName, paneTarget string) {
	if a.cfg.Adapter == nil || paneTarget == "" {
		return
	}
	if err := a.cfg.Adapter.SendKeysEnter(ctx, paneTarget); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: nudgePane %q pane %q: %v\n", agentName, paneTarget, err)
	}
}

// writeSleepMarker creates .harmonik/.sleeping.<sessionID>.
// The file body is a JSON object with the session_id, parked_at time, and the
// park source/level (hk-caaf); it is written best-effort and used by external
// observers (e.g. the captain's crew-launch loop) to detect parked state.
func (a *QuiesceArbiter) writeSleepMarker(sessionID string, source SleepSource, level SleepLevel) {
	dir := filepath.Join(a.cfg.ProjectDir, sleepingMarkerDir)
	//nolint:gosec // G301: .harmonik/ dir needs to be readable/writable by the project owner; 0755 is intentional
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: mkdir %q: %v\n", dir, err)
		return
	}
	path := filepath.Join(dir, ".sleeping."+sessionID)
	marker := sleepMarker{
		SessionID: sessionID,
		ParkedAt:  time.Now().UTC().Format(time.RFC3339),
		Source:    source,
		Level:     level,
	}
	marker.normalize()
	body, _ := json.Marshal(marker)
	//nolint:gosec // G306: marker file is readable by all users of this project; 0644 is intentional
	if err := os.WriteFile(path, body, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: write sleep marker %q: %v\n", path, err)
	}
}

// readSleepMarker reads and parses .harmonik/.sleeping.<sessionID>, applying the
// backward-compatible defaults (hk-caaf) so a marker written by an older daemon
// (session_id + parked_at only) still yields a fully-populated record.
func (a *QuiesceArbiter) readSleepMarker(path string) (sleepMarker, error) {
	var m sleepMarker
	//nolint:gosec // G304: path is composed from the trusted ProjectDir + a fixed marker prefix.
	body, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, err
	}
	m.normalize()
	return m, nil
}

// clearSleepMarker removes .harmonik/.sleeping.<sessionID>.
// Best-effort: errors are logged but never fatal.
func (a *QuiesceArbiter) clearSleepMarker(sessionID string) {
	if sessionID == "" || a.cfg.ProjectDir == "" {
		return
	}
	path := filepath.Join(a.cfg.ProjectDir, sleepingMarkerDir, ".sleeping."+sessionID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: clear sleep marker %q: %v\n", path, err)
	}
}

// SetDrain wires the drain detector used by the SS-INV-005 veto gate in
// HandleDaemonSleep (P1-c, hk-zqb3).  Called once from the daemon
// composition root after the brcli adapter is available, before the socket
// listener starts accepting connections.  Thread-safe (guarded by mu).
func (a *QuiesceArbiter) SetDrain(d *DrainDetector) {
	a.mu.Lock()
	a.drain = d
	a.mu.Unlock()
}

// SetScheduleStore wires the schedule store used to suspend all enabled schedule
// jobs on `harmonik sleep` and restore them symmetrically on `harmonik wake --all`
// (hk-xjr1n). Called once from the daemon composition root after the schedule
// store is loaded, before Start. Thread-safe (guarded by mu).
func (a *QuiesceArbiter) SetScheduleStore(s *schedule.Store) {
	a.mu.Lock()
	a.cfg.ScheduleStore = s
	a.mu.Unlock()
}

// HandleDaemonSleep implements QuiesceOverrideHandler.
//
// When force is false the SS-INV-005 veto gate runs: GatherDrainFacts is
// called and the request is refused when the fleet has dispatchable or
// in-flight work that would be stranded (one-directional veto, P1-c hk-zqb3).
// force=true bypasses the gate entirely (operator escape hatch).
//
// CLI surface: harmonik sleep [--force]
// Bead ref: hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake).
func (a *QuiesceArbiter) HandleDaemonSleep(ctx context.Context, force bool) error {
	if !force {
		if err := a.vetoCheck(ctx); err != nil {
			return err
		}
	}
	// CLI `harmonik sleep` is an explicit operator command: source=operator so
	// the resulting park is sticky against event-reflex auto-wake (hk-caaf).
	a.parkAllSessions(ctx, SleepSourceOperator, SleepLevelDrain)
	// Suspend all enabled schedule jobs so no timer-driven work fires while the
	// fleet is parked. Write the fleet-sleeping marker for external gate checks
	// (harness crons via `harmonik sleep-gate`). hk-xjr1n.
	a.suspendScheduleJobs()
	a.writeFleetSleepingMarker()
	return nil
}

// vetoCheck runs GatherDrainFacts (SS-INV-005 veto gate) and returns a
// non-nil error when the sleep request should be refused.  Returns nil when
// the drain detector is nil (gate skipped; test mode / no brcli adapter
// wired) or when the fleet is confirmed empty on all active-work axes.
func (a *QuiesceArbiter) vetoCheck(ctx context.Context) error {
	a.mu.Lock()
	drain := a.drain
	a.mu.Unlock()
	if drain == nil {
		return nil
	}

	facts, err := drain.GatherDrainFacts(ctx)
	if err != nil {
		return fmt.Errorf("sleep vetoed: cannot determine fleet state: %w", err)
	}
	if facts.Unsure {
		return fmt.Errorf("sleep vetoed: fleet state uncertain (%s); use --force to override",
			strings.Join(facts.UnsureReasons, "; "))
	}

	// Refuse if any dispatchable or in-flight work would be stranded.
	var strands []string
	if facts.Ready.Count > 0 {
		strands = append(strands, fmt.Sprintf("%d ready bead(s)", facts.Ready.Count))
	}
	if facts.InProgress.Count > 0 {
		strands = append(strands, fmt.Sprintf("%d in-progress bead(s)", facts.InProgress.Count))
	}
	if facts.Runs.RegistryCount > 0 {
		strands = append(strands, fmt.Sprintf("%d in-flight run(s)", facts.Runs.RegistryCount))
	}
	if facts.Runs.LiveWorktrees > 0 {
		strands = append(strands, fmt.Sprintf("%d live worktree(s)", facts.Runs.LiveWorktrees))
	}
	if facts.Queued.Count > 0 {
		strands = append(strands, fmt.Sprintf("%d queued item(s)", facts.Queued.Count))
	}
	if len(facts.Queued.PausedQueues) > 0 {
		strands = append(strands, fmt.Sprintf("%d paused queue(s)", len(facts.Queued.PausedQueues)))
	}
	if len(facts.Queued.FailedArchives) > 0 {
		strands = append(strands, fmt.Sprintf("%d failed archive(s)", len(facts.Queued.FailedArchives)))
	}
	if len(facts.BlockedByOpenEpic) > 0 {
		strands = append(strands, fmt.Sprintf("%d bead(s) blocked by open epic(s)", len(facts.BlockedByOpenEpic)))
	}
	if len(strands) > 0 {
		return fmt.Errorf("sleep vetoed: fleet has active work (%s); use --force to override",
			strings.Join(strands, ", "))
	}
	return nil
}

// HandleDaemonWake implements QuiesceOverrideHandler.
//
// wakeAll=true wakes every sleeping session regardless of agentName.
// agentName, when non-empty, wakes only that named sleeping session.
// Returns an error if neither agentName nor wakeAll is specified.
//
// CLI surface: harmonik wake [--agent <name>|--all]
// Bead ref: hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake).
func (a *QuiesceArbiter) HandleDaemonWake(ctx context.Context, agentName string, wakeAll bool) error {
	if !wakeAll && agentName == "" {
		return fmt.Errorf("daemon: wake: provide --agent <name> or --all")
	}
	if wakeAll {
		a.wakeAllSessions(ctx)
		return nil
	}
	// Wake a specific named agent.
	a.mu.Lock()
	rec, ok := a.sleeping[agentName]
	if ok {
		delete(a.sleeping, agentName)
	}
	a.mu.Unlock()
	if !ok {
		// Not currently sleeping — informational, not fatal.
		fmt.Fprintf(os.Stderr, "daemon: quiesce: wake: %q is not currently sleeping\n", agentName)
		return nil
	}
	fmt.Fprintf(os.Stderr, "daemon: quiesce: waking %q (operator wake --agent)\n", agentName)
	a.nudgePane(ctx, agentName, rec.paneTarget)
	a.clearSleepMarker(rec.sessionID)
	return nil
}

// wakeAllSessions wakes every sleeping session unconditionally.
// Used by HandleDaemonWake(--all) and the operator CLI surface.
func (a *QuiesceArbiter) wakeAllSessions(ctx context.Context) {
	a.mu.Lock()
	targets := make([]sessionSleepRecord, 0, len(a.sleeping))
	for _, rec := range a.sleeping {
		targets = append(targets, rec)
	}
	for _, rec := range targets {
		delete(a.sleeping, rec.agentName)
	}
	a.mu.Unlock()
	for _, rec := range targets {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: waking %q (operator wake --all)\n", rec.agentName)
		a.nudgePane(ctx, rec.agentName, rec.paneTarget)
		a.clearSleepMarker(rec.sessionID)
	}
	// Restore schedule jobs that were suspended by sleep and remove the fleet
	// marker so external gate checks (harness crons) resume normally. hk-xjr1n.
	a.restoreScheduleJobs()
	a.clearFleetSleepingMarker()
}

// listCrewRecords loads the current crew registry.  Returns nil on error
// (logged; non-fatal — the arbiter simply skips crews it cannot enumerate).
func (a *QuiesceArbiter) listCrewRecords() []crew.Record {
	if a.cfg.ProjectDir == "" {
		return nil
	}
	records, err := crew.List(a.cfg.ProjectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: list crew records: %v\n", err)
		return nil
	}
	return records
}

// suspendScheduleJobs disables all currently-enabled schedule jobs and records
// which ones were disabled in .harmonik/sleep-suspended-jobs.json. Best-effort:
// errors are logged but never fatal (the session park already happened). hk-xjr1n.
func (a *QuiesceArbiter) suspendScheduleJobs() {
	a.mu.Lock()
	store := a.cfg.ScheduleStore
	a.mu.Unlock()
	if store == nil {
		return
	}
	suspended, err := store.SuspendAllForSleep()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: sleep: suspend schedule jobs: %v\n", err)
		return
	}
	if len(suspended) > 0 {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: sleep: suspended %d schedule job(s): %v\n", len(suspended), suspended)
	}
}

// restoreScheduleJobs re-enables the schedule jobs that were suspended by sleep,
// reading the suspended set from .harmonik/sleep-suspended-jobs.json. Best-effort:
// errors are logged but never fatal. hk-xjr1n.
func (a *QuiesceArbiter) restoreScheduleJobs() {
	a.mu.Lock()
	store := a.cfg.ScheduleStore
	a.mu.Unlock()
	if store == nil {
		return
	}
	restored, err := store.RestoreFromSleep()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: wake: restore schedule jobs: %v\n", err)
		return
	}
	if len(restored) > 0 {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: wake: restored %d schedule job(s): %v\n", len(restored), restored)
	}
}

// writeFleetSleepingMarker creates .harmonik/.fleet-sleeping so external agents
// (harness crons, scripts) can detect the sleeping state via `harmonik sleep-gate`
// without connecting to the daemon socket. Best-effort. hk-xjr1n.
func (a *QuiesceArbiter) writeFleetSleepingMarker() {
	if a.cfg.ProjectDir == "" {
		return
	}
	path := filepath.Join(a.cfg.ProjectDir, sleepingMarkerDir, fleetSleepingMarker)
	//nolint:gosec // G306: 0644 intentional — marker is world-readable within the project
	if err := os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: write fleet-sleeping marker: %v\n", err)
	}
}

// clearFleetSleepingMarker removes .harmonik/.fleet-sleeping. Best-effort. hk-xjr1n.
func (a *QuiesceArbiter) clearFleetSleepingMarker() {
	if a.cfg.ProjectDir == "" {
		return
	}
	path := filepath.Join(a.cfg.ProjectDir, sleepingMarkerDir, fleetSleepingMarker)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: clear fleet-sleeping marker: %v\n", err)
	}
}
