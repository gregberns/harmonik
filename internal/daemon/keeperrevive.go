package daemon

// keeperrevive.go — hk-220lv: daemon-hosted periodic keeper-revive watcher.
//
// THE BUG THIS CLOSES. A crew's keeper watcher process can die silently and
// nothing notices for many hours (field case: crew `yueh`, 43 h, zero restarts
// fired). Three signals were all blind at once:
//
//  1. The liveness signal is SELF-ERASING and nobody polled it.
//     keeper.LiveKeeperPresent probes for an exclusive flock on
//     .harmonik/keeper/<agent>.lock. When the holding process dies the kernel
//     drops the flock silently — no event, no mtime change, no file removal.
//     Its only callers were human-initiated (`keeper doctor`, `set-dispatching`,
//     `hold`) plus one ONE-SHOT post-spawn probe.
//  2. Every OTHER health signal stayed green. The .ctx gauge's primary writer is
//     scripts/keeper-statusline.sh, driven by Claude Code repaint — completely
//     independent of the watcher process. A crew with a dead keeper looks
//     identical from outside to a healthy one.
//  3. Nothing owned the keeper's lifecycle. The watcher is launched
//     fire-and-forget as a tmux window (tmuxsubstrate.go spawnCrewKeeperWindow);
//     the daemon is not its parent and cannot Wait() on it, and tmux removes the
//     window when the command exits.
//
// KeeperReviveWatcher is the missing periodic poller AND the missing owner. It is
// shaped after the sibling sweep CrewIdleReaper (crewidlereap.go): a ticker
// re-checks every crew registry record, and an agent is only acted on once the
// dead condition has held CONTINUOUSLY for Grace — never on the first tick that
// sees it. The capped-revive discipline (MaxAttempts, counter reset on confirmed
// alive, one loud escalation on the cap) copies the shape of the supervisor
// watchdog (internal/supervise/supervisor_watchdog.go).
//
// DEFAULT ON. Unlike the older one-shot post-spawn probe — which is gated on
// keeper.timings.flock_acquire_grace > 0, a key that is ABSENT from production
// config and therefore silently disabled — an absent config here falls back to
// the compiled defaults and the sweep RUNS. A safety net that is
// disabled-by-default is precisely the failure class hk-220lv exists to fix. The
// single opt-out is an explicit `keeper.timings.revive_scan_interval: 0s`.
//
// GATE: only keeper.IsManaged agents are ever revived (parity with
// cmd/harmonik/keeper_cmd.go's .managed opt-in guard). An unmanaged agent is
// deliberately keeper-less; resurrecting its keeper would fight the operator.
//
// Bead ref: hk-220lv.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

const (
	// keeperReviveDefaultScanInterval is how often every managed crew's keeper
	// flock is re-probed. LiveKeeperPresent is a single open+flock+close, so a
	// sweep over a handful of crews is negligible; 60s keeps detection latency a
	// small fraction of the grace window.
	keeperReviveDefaultScanInterval = 60 * time.Second

	// keeperReviveDefaultGrace is how long the flock must read CONTINUOUSLY
	// unheld before a re-arm fires. Long enough to ride out a keeper's own
	// restart (process exit → relaunch drops and re-takes the flock) without
	// re-arming a keeper that is merely bouncing; short enough that a genuinely
	// dead watcher is replaced in minutes, not the 43 h of the field case.
	keeperReviveDefaultGrace = 90 * time.Second

	// keeperReviveDefaultMaxAttempts caps re-arms per dead episode. The counter
	// resets the moment a live flock is observed, so this bounds a REPEATEDLY
	// FAILING revive (bad binary, dead tmux session) rather than the lifetime
	// number of self-heals.
	keeperReviveDefaultMaxAttempts = 3
)

// crewKeeperReArmer is the seam KeeperReviveWatcher uses to put a keeper window
// back. Satisfied by *tmuxSubstrate.
//
// CONTRACT: a nil return means a keeper process was ACTUALLY SPAWNED. It must
// NOT mean "a window with that name already exists" — the watcher publishes a
// session_keeper_watcher_revived event on a nil return, and a re-arm that
// reports success while spawning nothing is the same report-green-do-nothing
// failure this bead exists to remove. See tmuxSubstrate.ReArmCrewKeeperWindow
// for why the stale window is killed rather than treated as sufficient.
//
// The argv itself is NOT assembled here: agentlaunch.KeeperWindowArgv is the
// single source of truth for it.
type crewKeeperReArmer interface {
	ReArmCrewKeeperWindow(ctx context.Context, crewName, sessName, projectDir string) error
}

// KeeperReviveDisabledByConfig reports whether the operator has explicitly
// turned the keeper-revive sweep OFF.
//
// The ONLY opt-out is an explicit `keeper.timings.revive_scan_interval: 0s`.
// The Present bit is what distinguishes that from an ABSENT key, which resolves
// to the compiled defaults and leaves the sweep RUNNING — a safety net that is
// disabled-by-default is the failure class hk-220lv closes.
//
// Exported (not inlined at the construction site) so the kill-switch is pinned
// by a test rather than living only as an expression inside bootsocket wiring.
func KeeperReviveDisabledByConfig(cfg KeeperConfig) bool {
	return cfg.Present.ReviveScanInterval && cfg.ReviveScanInterval <= 0
}

// KeeperReviveWatcherConfig holds the construction-time parameters for
// KeeperReviveWatcher. Every external dependency is a seam so scan() can be
// driven deterministically from tests with no tmux, no filesystem and no clock.
type KeeperReviveWatcherConfig struct {
	// ProjectDir is the project root passed to ListCrews / IsManagedFn /
	// LiveKeeperFn. Empty makes scan a no-op.
	ProjectDir string

	// Disabled turns the whole sweep off. Set ONLY when the operator explicitly
	// wrote `keeper.timings.revive_scan_interval: 0s`; an ABSENT key must leave
	// this false so the compiled defaults apply and the sweep runs.
	Disabled bool

	// ScanInterval is how often the background goroutine re-probes every crew.
	// Zero → keeperReviveDefaultScanInterval.
	ScanInterval time.Duration

	// Grace is how long a crew's keeper flock must read continuously unheld
	// before a re-arm fires. Zero → keeperReviveDefaultGrace.
	Grace time.Duration

	// MaxAttempts caps re-arms per dead episode; reset on confirmed-alive.
	// Zero → keeperReviveDefaultMaxAttempts.
	MaxAttempts int

	// ListCrews enumerates the crew registry. Nil → crew.List.
	ListCrews crewListFunc

	// IsManagedFn reports the .managed opt-in. Nil → keeper.IsManaged.
	IsManagedFn func(projectDir, agent string) bool

	// LiveKeeperFn is the flock liveness probe. Nil → keeper.LiveKeeperPresent.
	LiveKeeperFn func(projectDir, agent string) bool

	// ReArmFn puts the keeper window back for (crewName, tmux session). Nil
	// makes scan a no-op — with no way to remediate, emitting a "revived" event
	// would be a lie. A nil return MUST mean a keeper was actually spawned; see
	// the crewKeeperReArmer contract.
	ReArmFn func(ctx context.Context, crewName, session string) error

	// Emit is the durable event seam (session_keeper_watcher_dead /
	// session_keeper_watcher_revived). Nil disables event emission.
	Emit crewKeeperEventBus

	// Comms is the operator-alert seam (keeper-alert). Nil disables alerts.
	Comms crewKeeperCommsBus

	// Now is the wall-clock source. Nil → time.Now.
	Now func() time.Time
}

// KeeperReviveWatcher periodically re-probes every managed crew's keeper flock
// and re-arms the keeper window for any crew whose watcher has been dead for at
// least Grace, up to MaxAttempts times per dead episode.
type KeeperReviveWatcher struct {
	cfg KeeperReviveWatcherConfig

	mu sync.Mutex
	// deadSince maps agent → the first scan its keeper flock was observed
	// unheld. Cleared on confirmed-alive, on a revive (so the next grace window
	// applies afresh), and when the crew record disappears.
	deadSince map[string]time.Time
	// attempts maps agent → revives fired during the current dead episode.
	// Reset to zero on confirmed-alive.
	attempts map[string]int
	// alerted maps agent → whether an operator keeper-alert has already fired
	// for the current dead episode. Reset on confirmed-alive. Prevents a
	// permanently-dead crew from mailing the operator on every scan.
	alerted map[string]bool
}

// NewKeeperReviveWatcher constructs a KeeperReviveWatcher from cfg, applying the
// compiled defaults for zero-valued fields and the production functions for nil
// seams.
func NewKeeperReviveWatcher(cfg KeeperReviveWatcherConfig) *KeeperReviveWatcher {
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = keeperReviveDefaultScanInterval
	}
	if cfg.Grace <= 0 {
		cfg.Grace = keeperReviveDefaultGrace
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = keeperReviveDefaultMaxAttempts
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ListCrews == nil {
		cfg.ListCrews = crew.List
	}
	if cfg.IsManagedFn == nil {
		cfg.IsManagedFn = keeper.IsManaged
	}
	if cfg.LiveKeeperFn == nil {
		cfg.LiveKeeperFn = keeper.LiveKeeperPresent
	}
	return &KeeperReviveWatcher{
		cfg:       cfg,
		deadSince: make(map[string]time.Time),
		attempts:  make(map[string]int),
		alerted:   make(map[string]bool),
	}
}

// inactiveReason returns a plain-English reason the sweep will do nothing, or
// "" when it is live. Every non-empty reason is announced at boot: an invisibly
// inactive safety net is the failure this bead closes, and the pre-existing
// post-spawn probe sat dead in production for exactly that reason.
func (w *KeeperReviveWatcher) inactiveReason() string {
	switch {
	case w.cfg.Disabled:
		return "explicitly disabled by operator config (keeper.timings.revive_scan_interval: 0s)"
	case w.cfg.ProjectDir == "":
		return "no project dir is configured on this daemon"
	case w.cfg.ReArmFn == nil:
		return "the active substrate cannot re-arm a crew keeper window — only the tmux substrate can " +
			"(e.g. HARMONIK_SUBSTRATE=codexdriver leaves no seam to spawn a keeper window into)"
	}
	return ""
}

// StartWatcher launches the background scan goroutine. Returns immediately; the
// goroutine runs until ctx is cancelled. Safe on a nil receiver (a daemon boot
// path that never wired the watcher).
//
// Whether the sweep comes up ACTIVE or INACTIVE, it says so at boot on stderr
// alongside the daemon's other startup diagnostics. This is deliberately NOT a
// debug-gated line: the operator must be able to tell from a normal daemon log
// whether crew keepers are being watched.
func (w *KeeperReviveWatcher) StartWatcher(ctx context.Context) {
	if w == nil {
		fmt.Fprintf(os.Stderr,
			"daemon: keeper-revive: INACTIVE — the watcher was never constructed on this boot path. "+
				"%s\n", keeperReviveInactiveConsequence)
		return
	}
	if reason := w.inactiveReason(); reason != "" {
		fmt.Fprintf(os.Stderr,
			"daemon: keeper-revive: INACTIVE — %s. %s\n", reason, keeperReviveInactiveConsequence)
		return
	}
	fmt.Fprintf(os.Stderr,
		"daemon: keeper-revive: ACTIVE — probing every managed crew's keeper flock every %s; "+
			"re-arming a keeper window after %s continuously dead, up to %d attempts per episode\n",
		w.cfg.ScanInterval, w.cfg.Grace, w.cfg.MaxAttempts)
	go w.loop(ctx)
}

// keeperReviveInactiveConsequence spells out what an operator loses whenever the
// sweep is inactive, so the log line is actionable rather than a status word.
const keeperReviveInactiveConsequence = "Crew keeper watchers will NOT be auto-revived: if a crew's " +
	"keeper process dies, the kernel drops its flock silently, every other health signal stays green, " +
	"and the crew runs unmonitored until a human notices (hk-220lv)."

// loop is the background goroutine body.
func (w *KeeperReviveWatcher) loop(ctx context.Context) {
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

// scan re-evaluates every crew record once. Exposed (not just loop-private) so
// tests can drive a single deterministic tick instead of waiting on the ticker —
// same seam CrewIdleReaper.scan provides.
func (w *KeeperReviveWatcher) scan(ctx context.Context) {
	if w.inactiveReason() != "" {
		return
	}
	records, err := w.cfg.ListCrews(w.cfg.ProjectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: keeper-revive: list crews: %v\n", err)
		return
	}

	now := w.cfg.Now()
	live := make(map[string]struct{}, len(records))
	for _, rec := range records {
		if rec.Name == "" {
			continue
		}
		live[rec.Name] = struct{}{}
		w.checkCrew(ctx, rec, now)
	}

	// Prune tracking for crews no longer in the registry (stopped, reaped).
	w.mu.Lock()
	for name := range w.deadSince {
		if _, ok := live[name]; !ok {
			delete(w.deadSince, name)
			delete(w.attempts, name)
			delete(w.alerted, name)
		}
	}
	w.mu.Unlock()
}

// checkCrew evaluates a single crew record.
//
// Order of gates, each of which is load-bearing:
//  1. NOT .managed → forget everything about the agent and return. An unmanaged
//     agent is intentionally keeper-less.
//  2. Flock HELD → confirmed alive: clear the dead clock, reset the attempt
//     counter and the alert latch. This is what makes MaxAttempts a per-episode
//     budget rather than a lifetime one.
//  3. Flock UNHELD, first observation → only ARM the grace clock. Never revive
//     on the first tick that sees a dead watcher.
//  4. Flock UNHELD past Grace → revive (or, at the cap, escalate once).
func (w *KeeperReviveWatcher) checkCrew(ctx context.Context, rec crew.Record, now time.Time) {
	if !w.cfg.IsManagedFn(w.cfg.ProjectDir, rec.Name) {
		w.forget(rec.Name)
		return
	}
	if w.cfg.LiveKeeperFn(w.cfg.ProjectDir, rec.Name) {
		w.confirmAlive(rec.Name)
		return
	}

	session := crewSessionFromHandle(rec.Handle)

	w.mu.Lock()
	since, tracked := w.deadSince[rec.Name]
	if !tracked {
		w.deadSince[rec.Name] = now
		w.mu.Unlock()
		return
	}
	deadFor := now.Sub(since)
	if deadFor < w.cfg.Grace {
		w.mu.Unlock()
		return
	}

	if session == "" {
		// No tmux session recorded in the registry Handle — there is nowhere to
		// put a keeper window back. Escalate once and stop; a silent skip here
		// would recreate the very blind spot this watcher closes.
		alreadyAlerted := w.alerted[rec.Name]
		w.alerted[rec.Name] = true
		w.mu.Unlock()
		if !alreadyAlerted {
			w.emitDead(ctx, rec.Name, deadFor)
			w.alertOperator(ctx, fmt.Sprintf(
				"crew %q keeper watcher is DEAD (flock unheld for %s) and its registry record has no tmux handle — "+
					"the daemon cannot re-arm a keeper window; run 'harmonik crew stop %s && harmonik crew start %s' "+
					"or attach one manually with 'harmonik keeper --agent %s'",
				rec.Name, deadFor.Round(time.Second), rec.Name, rec.Name, rec.Name))
		}
		return
	}

	attempts := w.attempts[rec.Name]
	if attempts >= w.cfg.MaxAttempts {
		alreadyAlerted := w.alerted[rec.Name]
		w.alerted[rec.Name] = true
		w.mu.Unlock()
		if !alreadyAlerted {
			w.alertOperator(ctx, fmt.Sprintf(
				"crew %q keeper watcher is DEAD and %d automatic re-arms all failed to take (flock still unheld "+
					"after %s) — GIVING UP; the crew is running unmonitored. Investigate the crew's tmux session %q "+
					"and run 'harmonik keeper --agent %s' by hand",
				rec.Name, w.cfg.MaxAttempts, deadFor.Round(time.Second), session, rec.Name))
		}
		return
	}
	attempt := attempts + 1
	w.attempts[rec.Name] = attempt
	// Re-arm the grace clock so the NEXT attempt waits a full Grace for the
	// freshly-spawned keeper to take its flock.
	w.deadSince[rec.Name] = now
	w.mu.Unlock()

	w.revive(ctx, rec.Name, session, deadFor, attempt)
}

// revive emits the diagnosis event, re-arms the keeper window, and — only if the
// re-arm call itself succeeded — emits the remediation event.
func (w *KeeperReviveWatcher) revive(ctx context.Context, agent, session string, deadFor time.Duration, attempt int) {
	fmt.Fprintf(os.Stderr,
		"daemon: keeper-revive: crew %q keeper watcher flock unheld for %s — re-arming keeper window in session %q (attempt %d/%d)\n",
		agent, deadFor.Round(time.Second), session, attempt, w.cfg.MaxAttempts)

	w.emitDead(ctx, agent, deadFor)

	if err := w.cfg.ReArmFn(ctx, agent, session); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: keeper-revive: re-arm keeper window for crew %q: %v\n", agent, err)
		return
	}

	w.emitEvent(ctx, core.EventTypeSessionKeeperWatcherRevived, core.SessionKeeperWatcherRevivedPayload{
		AgentName:      agent,
		Session:        session,
		DeadForSeconds: deadFor.Seconds(),
		Attempt:        attempt,
		MaxAttempts:    w.cfg.MaxAttempts,
	})
}

// emitDead emits session_keeper_watcher_dead — the same event type and payload
// the one-shot post-spawn probe uses (crewstart.go reportKeeperWatcherDead), so
// existing consumers need no change to see a mid-life keeper death.
func (w *KeeperReviveWatcher) emitDead(ctx context.Context, agent string, deadFor time.Duration) {
	w.emitEvent(ctx, core.EventTypeSessionKeeperWatcherDead, core.SessionKeeperWatcherDeadPayload{
		AgentName:          agent,
		GracePeriodSeconds: w.cfg.Grace.Seconds(),
		Reason: fmt.Sprintf("keeper flock unheld for %s (>= revive_grace) — watcher process died mid-life",
			deadFor.Round(time.Second)),
	})
}

// emitEvent marshals and emits payload as eventType. Best-effort: a nil bus or a
// marshal/emit failure never blocks the sweep.
func (w *KeeperReviveWatcher) emitEvent(ctx context.Context, eventType core.EventType, payload any) {
	if w.cfg.Emit == nil {
		return
	}
	b, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: keeper-revive: marshal %s payload: %v\n", eventType, err)
		return
	}
	if emitErr := w.cfg.Emit.Emit(ctx, eventType, b); emitErr != nil {
		// Non-fatal: a lost diagnostic must not stop the self-heal. But it is
		// logged — a silently-dropped watcher_dead/watcher_revived would put the
		// audit trail right back in the blind spot this bead closes.
		fmt.Fprintf(os.Stderr, "daemon: keeper-revive: emit %s: %v\n", eventType, emitErr)
	}
}

// alertOperator sends a keeper-alert comms message, matching the call shape of
// crewstart.go reportKeeperWatcherDead. Best-effort.
func (w *KeeperReviveWatcher) alertOperator(ctx context.Context, body string) {
	fmt.Fprintf(os.Stderr, "daemon: keeper-revive: %s\n", body)
	if w.cfg.Comms == nil {
		return
	}
	//nolint:errcheck // best-effort alert; non-fatal
	_, _ = w.cfg.Comms.EmitAgentMessage(ctx, core.AgentMessagePayload{
		From:  "daemon",
		To:    "operator",
		Topic: "keeper-alert",
		Body:  body,
	})
}

// confirmAlive clears all per-episode state for agent. A live flock is proof the
// crew is monitored again, which resets the revive budget and re-arms the
// operator alert for any FUTURE death.
func (w *KeeperReviveWatcher) confirmAlive(agent string) { w.forget(agent) }

// forget drops every tracking entry for agent.
func (w *KeeperReviveWatcher) forget(agent string) {
	w.mu.Lock()
	delete(w.deadSince, agent)
	delete(w.attempts, agent)
	delete(w.alerted, agent)
	w.mu.Unlock()
}

// crewSessionFromHandle derives the tmux SESSION name from a crew registry
// Handle. Handles are window handles of the form "<session>:agent" (crew.Record
// Handle, written by HandleCrewStart from the substrate's WindowHandle), so the
// ":agent" suffix — tmux.WindowAgent, never a hardcoded literal — is trimmed.
// A handle with some other window suffix falls back to everything before the
// last ':'. An empty handle yields an empty session, which the caller treats as
// "not re-armable".
func crewSessionFromHandle(handle string) string {
	if handle == "" {
		return ""
	}
	if sess, ok := strings.CutSuffix(handle, ":"+tmux.WindowAgent); ok {
		return sess
	}
	if i := strings.LastIndex(handle, ":"); i > 0 {
		return handle[:i]
	}
	return handle
}
