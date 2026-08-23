package daemon

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
	"github.com/gregberns/harmonik/internal/policy"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/schedule"
)

const (
	quiesceArbiterPollInterval = 30 * time.Second

	quiesceArbiterMaxSleepDuration = 4 * time.Hour

	sleepingMarkerDir = ".harmonik"

	fleetSleepingMarker = ".fleet-sleeping"

	captainAgentName = "captain"

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

const (
	defaultSleepSource = SleepSourceOperator
	defaultSleepLevel  = SleepLevelDrain
)

type sleepMarker struct {
	SessionID string      `json:"session_id"`
	ParkedAt  string      `json:"parked_at"`
	Source    SleepSource `json:"source"`
	Level     SleepLevel  `json:"level"`
}

func (m *sleepMarker) normalize() {
	if m.Source == "" {
		m.Source = defaultSleepSource
	}
	if m.Level == "" {
		m.Level = defaultSleepLevel
	}
}

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
	QueueStore *queuewiring.QueueStore

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
	a.reconcileOrphanedMarkers(ctx)
	go a.run(ctx)
}

func (a *QuiesceArbiter) reconcileOrphanedMarkers(ctx context.Context) {
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
			sessionID = name[len(markerPrefix):]
		}

		sleptAt := time.Now()
		if marker.ParkedAt != "" {
			if t, perr := time.Parse(time.RFC3339, marker.ParkedAt); perr == nil {
				sleptAt = t
			}
		}

		var agentName, queueName, paneTarget string
		if r, ok := crewBySID[sessionID]; ok {
			agentName = r.Name
			queueName = r.Queue
			if r.Handle != "" {
				paneTarget = r.Handle + ".0"
			}
		} else if sessionID == "captain-session" {
			agentName = captainAgentName
			paneTarget = a.resolveCaptainTarget(ctx)
		} else {
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
			a.handleQueueSubmit(ctx)

		case sig := <-a.wakeC:
			a.executeWake(ctx, sig)
		}
	}
}

func (a *QuiesceArbiter) tick(ctx context.Context, maxSleep time.Duration) {
	a.mu.Lock()
	var expired []sessionSleepRecord
	for _, rec := range a.sleeping {
		if time.Since(rec.sleptAt) >= maxSleep {
			expired = append(expired, rec)
		}
	}
	for _, rec := range expired {
		delete(a.sleeping, rec.agentName)
	}
	a.mu.Unlock()

	for _, rec := range expired {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: max-sleep failsafe: waking %q (slept %v)\n",
			rec.agentName, time.Since(rec.sleptAt).Round(time.Second))
		a.wakeSession(ctx, rec.agentName, rec.paneTarget, rec.sessionID)
	}
}

func (a *QuiesceArbiter) parkAllSessions(ctx context.Context, source SleepSource, level SleepLevel) {
	records := a.listCrewRecords()

	captainTarget := a.resolveCaptainTarget(ctx)
	a.parkSession(ctx, captainAgentName, "", "captain-session", captainTarget, source, level)

	for _, r := range records {
		if r.Handle == "" || r.SessionID == "" {
			continue
		}
		pane := r.Handle + ".0"
		a.parkSession(ctx, r.Name, r.Queue, r.SessionID, pane, source, level)
	}
}

func (a *QuiesceArbiter) resolveCaptainTarget(ctx context.Context) string {
	if a.cfg.ProjectDir != "" {
		if t := keeper.ResolveTmuxTarget(a.cfg.ProjectDir, captainAgentName, "", nil); t != "" {
			return t
		}
	}
	if tmuxHasSession(ctx, captainAgentName) {
		return captainAgentName
	}
	if a.cfg.ProjectDir != "" {
		return keeper.HarmonikSessionName(a.cfg.ProjectDir, captainAgentName) + ":agent"
	}
	return lifecycle.TmuxSessionName(a.cfg.ProjectHash, captainAgentName) + ":agent"
}

func tmuxHasSession(ctx context.Context, name string) bool {
	if name == "" {
		return false
	}
	//nolint:gosec // G204: name is a fixed constant (captainAgentName) or a derived session name.
	cmd := exec.CommandContext(ctx, "tmux", "has-session", "-t", "="+name)
	return cmd.Run() == nil
}

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

	if sessionID != "" && a.cfg.ProjectDir != "" {
		a.writeSleepMarker(sessionID, source, level)
	}

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

func (a *QuiesceArbiter) handleQueueSubmit(ctx context.Context) {
	if a.cfg.QueueStore == nil {
		return
	}
	queues := a.cfg.QueueStore.AllQueues()
	records := a.listCrewRecords()

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

func (a *QuiesceArbiter) handleEpicCompleted(ctx context.Context, evt core.Event) error {
	select {
	case a.wakeC <- wakeSignal{captainWake: true, reason: "epic_completed"}:
	default:
	}
	return nil
}

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
		}
	case watchAgentName:
		select {
		case a.wakeC <- wakeSignal{agentName: watchAgentName, reason: fmt.Sprintf("agent_message from %q to watch", payload.From)}:
		default:
		}
	}
	return nil
}

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
	for _, rec := range targets {
		delete(a.sleeping, rec.agentName)
	}
	a.mu.Unlock()

	for _, rec := range targets {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: waking %q (%s)\n", rec.agentName, sig.reason)
		a.wakeSession(ctx, rec.agentName, rec.paneTarget, rec.sessionID)
	}
}

func (a *QuiesceArbiter) wakeSession(ctx context.Context, agentName, paneTarget, sessionID string) {
	a.clearSleepMarker(sessionID)
	a.nudgePane(ctx, agentName, paneTarget)
}

func (a *QuiesceArbiter) nudgePane(ctx context.Context, agentName, paneTarget string) {
	if a.cfg.Adapter == nil || paneTarget == "" {
		return
	}
	if err := a.cfg.Adapter.SendKeysEnter(ctx, paneTarget); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: nudgePane %q pane %q: %v\n", agentName, paneTarget, err)
	}
}

func (a *QuiesceArbiter) writeSleepMarker(sessionID string, source SleepSource, level SleepLevel) {
	dir := filepath.Join(a.cfg.ProjectDir, sleepingMarkerDir)
	if err := os.MkdirAll(dir, core.HarmonikDirMode); err != nil {
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
	body, marshalErr := json.Marshal(marker)
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: marshal sleep marker for %q: %v\n", sessionID, marshalErr)
		return
	}
	//nolint:gosec // G306: marker file is readable by all users of this project; 0644 is intentional
	if err := os.WriteFile(path, body, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: write sleep marker %q: %v\n", path, err)
	}
}

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
	a.parkAllSessions(ctx, SleepSourceOperator, SleepLevelDrain)
	a.suspendScheduleJobs()
	a.writeFleetSleepingMarker()
	return nil
}

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

	res := policy.SleepVeto(drainSnapshot(facts))
	if res.Unsure {
		return fmt.Errorf("sleep vetoed: fleet state uncertain (%s); use --force to override",
			strings.Join(res.UnsureReasons, "; "))
	}
	if len(res.Strands) > 0 {
		return fmt.Errorf("sleep vetoed: fleet has active work (%s); use --force to override",
			strings.Join(res.Strands, ", "))
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
	a.mu.Lock()
	rec, ok := a.sleeping[agentName]
	if ok {
		delete(a.sleeping, agentName)
	}
	a.mu.Unlock()
	if !ok {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: wake: %q is not currently sleeping\n", agentName)
		return nil
	}
	fmt.Fprintf(os.Stderr, "daemon: quiesce: waking %q (operator wake --agent)\n", agentName)
	a.wakeSession(ctx, agentName, rec.paneTarget, rec.sessionID)
	return nil
}

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
		a.wakeSession(ctx, rec.agentName, rec.paneTarget, rec.sessionID)
	}
	a.restoreScheduleJobs()
	a.clearFleetSleepingMarker()
}

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

func (a *QuiesceArbiter) clearFleetSleepingMarker() {
	if a.cfg.ProjectDir == "" {
		return
	}
	path := filepath.Join(a.cfg.ProjectDir, sleepingMarkerDir, fleetSleepingMarker)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "daemon: quiesce: clear fleet-sleeping marker: %v\n", err)
	}
}
