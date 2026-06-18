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
//   - Captain: lifecycle.TmuxSessionName(hash, "captain") + ":0.0" for the
//     first window/pane, falling back to just the session name when
//     tmuxresolve.ResolveTmuxTarget confirms the session is live.
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
	"path/filepath"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
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

	// captainAgentName is the conventional captain agent name used by
	// lifecycle.TmuxSessionName and crew registries.
	captainAgentName = "captain"
)

// paneNudger is the minimal interface the QuiesceArbiter needs from the tmux
// adapter.  Using a narrow interface lets tests inject a simple stub without
// implementing the full tmuxpkg.Adapter (which has ~14 methods).
type paneNudger interface {
	SendKeysEnter(ctx context.Context, paneTarget string) error
}

// QuiesceArbiterConfig bundles the dependencies of the QuiesceArbiter.
// All fields are required unless documented as optional.
type QuiesceArbiterConfig struct {
	// Drain is the GenuineDrain oracle (M0).  REQUIRED.
	Drain *DrainDetector

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
}

// QuiesceArbiter polls GenuineDrain and manages fleet sleep/wake.
type QuiesceArbiter struct {
	cfg QuiesceArbiterConfig

	mu       sync.Mutex
	sleeping map[string]sessionSleepRecord // agentName → record (non-empty means parked)

	// wakeC is the internal channel for event-triggered wakes.
	wakeC chan wakeSignal
}

// wakeSignal carries the routing key for a triggered wake event.
type wakeSignal struct {
	// queueName, when non-empty, routes the wake to the crew bound to that queue.
	queueName string
	// captainWake, when true, routes the wake to the captain regardless of queue.
	captainWake bool
	// reason is a human-readable label for logging.
	reason string
}

// NewQuiesceArbiter constructs a QuiesceArbiter from cfg.  The caller must
// invoke Subscribe before sealing the bus, then Start after sealing.
//
// Two-phase init is supported: cfg.Drain may be nil at construction time and
// set later via SetDrain before calling Start.
func NewQuiesceArbiter(cfg QuiesceArbiterConfig) *QuiesceArbiter {
	return &QuiesceArbiter{
		cfg:      cfg,
		sleeping: make(map[string]sessionSleepRecord),
		wakeC:    make(chan wakeSignal, 32),
	}
}

// SetDrain wires the GenuineDrain oracle after the arbiter is constructed.
// MUST be called before Start().  Provided for two-phase daemon wiring where
// the brAdapter (required by DrainDetector) is constructed after bus.Seal().
func (a *QuiesceArbiter) SetDrain(d *DrainDetector) {
	a.cfg.Drain = d
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
			Types: map[string]struct{}{
				string(core.EventTypeEpicCompleted): {},
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
			Types: map[string]struct{}{
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
// Pattern: same as staleWatcher.StartWatcher.
func (a *QuiesceArbiter) Start(ctx context.Context) {
	go a.run(ctx)
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

	// Drain check: evaluate GenuineDrain; on DRAINED, park all live sessions.
	// Skipped when the DrainDetector has not been wired yet (nil guard for
	// two-phase daemon init — failsafe above still runs).
	if a.cfg.Drain == nil {
		return
	}
	res, err := a.cfg.Drain.GenuineDrain(ctx)
	if err != nil || res.State != DrainStateDrained {
		// Not drained (or error → stay awake per fail-closed contract).
		return
	}

	// Drained: park all sessions not already sleeping.
	a.parkAllSessions(ctx)
}

// parkAllSessions writes sleep markers and sends park comms signals to every
// known LLM session (captain + all crews) that is not already sleeping.
func (a *QuiesceArbiter) parkAllSessions(ctx context.Context) {
	records := a.listCrewRecords()

	// Captain: resolve pane target via session name convention.
	captainTarget := lifecycle.TmuxSessionName(a.cfg.ProjectHash, captainAgentName) + ":0.0"
	a.parkSession(ctx, captainAgentName, "", "captain-session", captainTarget)

	// Each crew session.
	for _, r := range records {
		if r.Handle == "" || r.SessionID == "" {
			continue
		}
		pane := r.Handle + ".0"
		a.parkSession(ctx, r.Name, r.Queue, r.SessionID, pane)
	}
}

// parkSession parks one session: writes the sleep marker file and sends a comms
// park signal.  No-op when the session is already sleeping.
func (a *QuiesceArbiter) parkSession(ctx context.Context, agentName, queueName, sessionID, paneTarget string) {
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
	}
	a.sleeping[agentName] = rec
	a.mu.Unlock()

	// Write .sleeping.<session_id> marker.
	if sessionID != "" && a.cfg.ProjectDir != "" {
		a.writeSleepMarker(sessionID)
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
// Wakes the captain only when the message is directed at the captain.
func (a *QuiesceArbiter) handleAgentMessage(ctx context.Context, evt core.Event) error {
	var payload core.AgentMessagePayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return nil // malformed payload; skip silently
	}
	if payload.To != captainAgentName {
		return nil
	}
	select {
	case a.wakeC <- wakeSignal{captainWake: true, reason: fmt.Sprintf("agent_message from %q to captain", payload.From)}:
	default:
		// Channel full: best-effort.
	}
	return nil
}

// executeWake wakes the session identified by sig.
//
// Wake routing:
//   - sig.captainWake → wake captain (if sleeping).
//   - sig.queueName non-empty → wake crew for that queue (if sleeping).
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
// The file body is a JSON object with the session_id and parked_at time; it is
// written best-effort and used by external observers (e.g. the captain's
// crew-launch loop) to detect parked state.
func (a *QuiesceArbiter) writeSleepMarker(sessionID string) {
	dir := filepath.Join(a.cfg.ProjectDir, sleepingMarkerDir)
	//nolint:gosec // G301: .harmonik/ dir needs to be readable/writable by the project owner; 0755 is intentional
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: mkdir %q: %v\n", dir, err)
		return
	}
	path := filepath.Join(dir, ".sleeping."+sessionID)
	body, _ := json.Marshal(map[string]string{
		"session_id": sessionID,
		"parked_at":  time.Now().UTC().Format(time.RFC3339),
	})
	//nolint:gosec // G306: marker file is readable by all users of this project; 0644 is intentional
	if err := os.WriteFile(path, body, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: write sleep marker %q: %v\n", path, err)
	}
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
