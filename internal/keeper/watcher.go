package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/presence"
	"github.com/gregberns/harmonik/internal/substrate"
)

// Emitter is a minimal event-emission interface used by the keeper watcher.
// The eventbus.EventBus interface satisfies this contract via its
// EmitWithRunID method — callers may pass an eventbus.EventBus directly.
type Emitter interface {
	EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error
}

// NoopEmitter is an Emitter that silently discards all events.
type NoopEmitter struct{}

func (NoopEmitter) EmitWithRunID(_ context.Context, _ core.RunID, _ core.EventType, _ []byte) error {
	return nil
}

// FileEmitter appends typed events to the harmonik events JSONL file at
// <projectDir>/.harmonik/events/events.jsonl. It is used by the standalone
// `harmonik keeper` process which runs outside the daemon and has no in-process
// event bus. On write failure the event is also logged via slog so a missing
// gauge is never fully silent.
type FileEmitter struct {
	path  string
	idGen *core.EventIDGenerator
	clock substrate.ClockPort
	mu    sync.Mutex
}

// NewFileEmitter constructs a FileEmitter that appends to the harmonik events
// log at projectDir/.harmonik/events/events.jsonl. Event TimestampWall stamps
// are read through substrate.SystemClock (the determinism port, SK-008/D9).
func NewFileEmitter(projectDir string) *FileEmitter {
	return NewFileEmitterWithClock(projectDir, substrate.SystemClock{})
}

// NewFileEmitterWithClock is NewFileEmitter with an injectable ClockPort for
// the TimestampWall stamp (D9): the T7 shell wires the SAME clock that drives
// the cycle so replay envelopes are reproducible under a substrate.FakeClock.
// Nil clock falls back to the system clock.
func NewFileEmitterWithClock(projectDir string, clock substrate.ClockPort) *FileEmitter {
	if clock == nil {
		clock = substrate.SystemClock{}
	}
	return &FileEmitter{
		path:  filepath.Join(projectDir, ".harmonik", core.EventsJSONLPath),
		idGen: core.NewEventIDGenerator(),
		clock: clock,
	}
}

// EmitWithRunID appends a typed event line to the harmonik events JSONL file.
// runID is embedded when non-zero. On write error the event is also logged via
// slog so it is never fully silent.
func (f *FileEmitter) EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	eventID, genErr := f.idGen.Next()
	if genErr != nil {
		slog.WarnContext(ctx, "keeper: FileEmitter: generate event_id", "err", genErr)
		return genErr
	}

	ev := core.Event{
		EventID:         eventID,
		SchemaVersion:   1,
		Type:            string(eventType),
		TimestampWall:   f.clock.Now().UTC(),
		SourceSubsystem: "internal/keeper",
		Payload:         json.RawMessage(payload),
	}
	var zeroRunID core.RunID
	if runID != zeroRunID {
		r := runID
		ev.RunID = &r
	}

	raw, marshalErr := json.Marshal(ev)
	if marshalErr != nil {
		slog.WarnContext(ctx, "keeper: FileEmitter: marshal event", "err", marshalErr)
		return marshalErr
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	//nolint:gosec // G304: path is daemon-startup-resolved; not user input.
	file, openErr := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if openErr != nil {
		slog.WarnContext(ctx, "keeper: FileEmitter: open events.jsonl", "err", openErr, "path", f.path)
		return openErr
	}
	defer func() { _ = file.Close() }()

	_, writeErr := file.Write(append(raw, '\n'))
	if writeErr != nil {
		slog.WarnContext(ctx, "keeper: FileEmitter: write event", "err", writeErr)
	}
	return writeErr
}

// RecordingEmitter records every EmitWithRunID call. Used in unit tests.
type RecordingEmitter struct {
	mu     sync.Mutex
	Events []EmittedEvent
}

// EmittedEvent is a single record in RecordingEmitter.Events.
type EmittedEvent struct {
	RunID   core.RunID
	Type    core.EventType
	Payload []byte
}

// EmitWithRunID records the call.
func (r *RecordingEmitter) EmitWithRunID(_ context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, EmittedEvent{RunID: runID, Type: eventType, Payload: payload})
	return nil
}

// EventsOfType returns all recorded events with the given type.
func (r *RecordingEmitter) EventsOfType(t core.EventType) []EmittedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []EmittedEvent
	for _, e := range r.Events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// HardCeilingMode selects what the SID-independent hard-ceiling backstop does
// when a watched pane crosses HardCeilingTokens. The ZERO VALUE is
// HardCeilingModeAlarm (operator decision: hard-ceiling default mode = alarm),
// so a config left untouched alarms but does not restart. Refs: hk-n6kn.
//
// NOTE: this bead (hk-n6kn) adds the TYPE only — the gate still keys restart on
// HardCeilingRestartFn != nil and is behaviour-identical to today (the mode is
// not yet consulted by the gate; that wiring is the NEXT bead).
type HardCeilingMode int

const (
	// HardCeilingModeAlarm is the zero value: emit session_keeper_hard_ceiling
	// but take no restart action. This is the operator-chosen default.
	HardCeilingModeAlarm HardCeilingMode = iota
	// HardCeilingModeOff disables the hard-ceiling backstop entirely.
	HardCeilingModeOff
	// HardCeilingModeRestart alarms AND drives a handoff+restart.
	HardCeilingModeRestart
)

// String renders the mode as its lowercase flag token.
func (m HardCeilingMode) String() string {
	switch m {
	case HardCeilingModeOff:
		return "off"
	case HardCeilingModeRestart:
		return "restart"
	case HardCeilingModeAlarm:
		return "alarm"
	default:
		return "alarm"
	}
}

// ParseHardCeilingMode parses a flag token into a HardCeilingMode. An empty or
// unrecognized token resolves to the zero value (alarm). Refs: hk-n6kn.
func ParseHardCeilingMode(s string) HardCeilingMode {
	switch s {
	case "off":
		return HardCeilingModeOff
	case "restart":
		return HardCeilingModeRestart
	case "alarm", "":
		return HardCeilingModeAlarm
	default:
		return HardCeilingModeAlarm
	}
}

// WatcherConfig is the configuration for a Watcher instance.
type WatcherConfig struct {
	// Clock is the determinism port for all watcher-loop timing reads: the poll
	// ticker, staleness/cooldown math, and per-tick timestamps (SK-008/SK-R3,
	// substrate D4). Nil → substrate.SystemClock{} (production wall clock). T6
	// promotes this to a named ClockPort alongside the extracted ports.
	Clock substrate.ClockPort

	// AgentName is the keeper agent identifier (matches the --agent flag).
	AgentName string

	// ProjectDir is the harmonik project root.
	ProjectDir string

	// PollInterval is how often the watcher reads the gauge file. Default: 5s.
	PollInterval time.Duration

	// WarnPct is the upward percentage threshold that triggers a warn injection
	// when absolute token counts are unavailable. Default: 80.
	WarnPct float64

	// WarnAbsTokens is the absolute-token warn threshold. The effective threshold
	// is min(WarnAbsTokens, WarnPctCeil * WindowSize), applied by belowWarnThreshold
	// ONLY when the gauge reports BOTH Tokens>0 and WindowSize>0. When WindowSize==0
	// the abs gate is not trustworthy (the window is unknown), so the gate falls
	// back to Pct vs WarnPct — identical to the cycler, avoiding the F45
	// Tokens-vs-Pct split-brain. Default: 200000.
	// Refs: hk-cl74g, hk-kgn, hk-odhh, hk-jgzg, hk-8hr1.
	WarnAbsTokens int64

	// FallbackWindowSize is the assumed context-window size used to derive a live
	// pct from a fresh token count in the heartbeat refresh (heartbeat.go) when the
	// gauge file reports WindowSize==0 (e.g. [1m]-class models whose window size
	// cannot be inferred). It is NOT used to cap the warn threshold — belowWarnThreshold
	// requires a real WindowSize. Default: 200000. Set via --window-size.
	// Refs: hk-kgn, hk-jgzg.
	FallbackWindowSize int64

	// WarnPctCeil caps the warn threshold as a fraction of the context window,
	// preventing late warnings on large windows. Default: 0.70.
	WarnPctCeil float64

	// IdleQuiesce is the minimum duration of gauge-file quiescence before the
	// watcher considers the pane idle enough to accept an injection.
	// Default: 8s.
	IdleQuiesce time.Duration

	// Staleness is the maximum age of the gauge file's mod-time before the
	// watcher treats the gauge as absent. Default: 120s.
	Staleness time.Duration

	// TmuxTarget is the tmux pane address for injection. When empty the watcher
	// skips actual injection (warn event is still emitted). This is the normal
	// case for unit tests.
	TmuxTarget string

	// InjectFn is the function used to deliver the wrap-up-warning injection.
	// When nil, InjectWrapUpWarning is used. Set to a spy function in unit tests
	// to verify injection without spawning real tmux commands.
	InjectFn func(ctx context.Context, target string) error

	// SelfHintInjectFn delivers the one-time [KEEPER HINT] text into the
	// pane (hk-lsk5). When nil, InjectText is used (the production path, which
	// shells out to tmux). Set to a spy in unit tests so the hint path can be
	// observed without real tmux. Mirrors InjectFn for the warn injection.
	SelfHintInjectFn func(ctx context.Context, target, text string) error

	// DashboardNagInjectFn delivers the [KEEPER NAG] dashboard-staleness
	// pre-nag text into the pane (hk-xg6rw, DESIGN §4 recommendation B). When
	// nil, InjectText is used. Set to a spy in unit tests. Mirrors
	// SelfHintInjectFn / InjectFn.
	DashboardNagInjectFn func(ctx context.Context, target, text string) error

	// Cycler, if non-nil, enables Phase-2 cycle dispatch. MaybeRun is called on
	// each fresh-gauge tick; gating (act_pct, CrispIdle, HoldingDispatch,
	// anti-loop) is handled internally by the Cycler.
	Cycler *Cycler

	// SuppressNoGauge disables session_keeper_no_gauge emissions when the gauge
	// file is expected to be absent — e.g. in dogfood/test sessions that run the
	// keeper without a real gauge writer. Without this flag such sessions produce
	// x66+ no_gauge events per session (F21).
	SuppressNoGauge bool

	// ReadManagedSessionFn, when non-nil, is called on each fresh-gauge tick to
	// read the expected session_id from .managed. When nil, ReadManagedSessionID
	// is used. Set in tests to control the managed binding without filesystem I/O.
	// Refs: hk-igt (session_id clobber fix).
	ReadManagedSessionFn func(projectDir, agent string) (string, error)

	// WriteManagedSessionFn, when non-nil, is called when the watcher adopts a
	// same-agent session_id after an external /clear. When nil,
	// WriteManagedSessionID is used. Set to a no-op in unit tests that do not
	// need adoption side-effects.
	// Refs: hk-igt (session_id clobber fix).
	WriteManagedSessionFn func(projectDir, agent, sessionID string) error

	// RespawnCmd, when non-empty, enables the supervised respawn path. After the
	// gauge goes stale for at least RespawnGrace and the tmux pane is idle (agent
	// has exited), the keeper runs this command via "sh -c <RespawnCmd>" to
	// re-launch the agent. A cooldown of RespawnCooldown prevents tight loops.
	// Requires TmuxTarget to be non-empty; ignored when TmuxTarget is "".
	// Refs: hk-3w2.
	RespawnCmd string

	// RespawnGrace is the minimum duration the gauge must be stale before a
	// respawn is attempted. Prevents premature respawn when the agent briefly
	// stops writing gauge data. Default: 20s.
	// Refs: hk-3w2.
	RespawnGrace time.Duration

	// RespawnCooldown is the minimum duration between consecutive respawn
	// attempts. Prevents tight respawn loops when the agent exits immediately
	// after each launch. Default: 90s.
	// Refs: hk-3w2.
	RespawnCooldown time.Duration

	// IsPaneIdleFn reports whether the tmux pane at target is running a shell
	// (indicating the agent has exited). When nil, IsPaneIdle is used.
	// Set in tests to control the check without real tmux commands.
	// Refs: hk-3w2.
	IsPaneIdleFn func(ctx context.Context, target string) bool

	// ReapDecisions enables the hitl-decisions orphan reaper (component K5, bead
	// hk-061) on the watch tick. When true, maybeReapOrphanedDecisions runs at
	// ReapDecisionsCadence (default 90s — not every poll) and emits
	// decision_withdrawn(orphaned, by=keeper) for any open decision whose
	// blocked_agent is Offline (an explicit leave beat OR age ≥ presence.StaleCutoff,
	// never merely Stale — N9). The keeper tick is the SOLE emitter of orphaned
	// withdrawals (N9); the reaper runs independent of the gauge-fresh state machine
	// so orphan latency is bounded by Offline-cutoff + ReapDecisionsCadence.
	//
	// Default: false (the reaper is opt-in; the standalone `harmonik keeper`
	// process enables it — keeper_cmd.go). When false the watcher behaves exactly
	// as before (no decision reaping).
	// Refs: hk-061 (hitl-decisions K5); SPEC §5 / N9; hk-jrftk (cadence gate).
	ReapDecisions bool

	// EventsJSONLPath is the path to the project's events.jsonl, read by the
	// orphan reaper (ReapDecisions) for the open-decision projection + presence
	// registry. When empty and ReapDecisions is true, applyDefaults derives it as
	// <ProjectDir>/.harmonik/<core.EventsJSONLPath>.
	// Refs: hk-061.
	EventsJSONLPath string

	// DecisionEmitter is the bus used by the orphan reaper to emit
	// decision_withdrawn(orphaned). When nil and ReapDecisions is true, the
	// watcher's primary emitter is reused (the FileEmitter for the standalone
	// keeper, which appends to the same events.jsonl). Set to a spy in tests.
	// Refs: hk-061.
	DecisionEmitter presence.Emitter

	// ReapDecisionsCadence is the minimum interval between consecutive
	// maybeReapOrphanedDecisions runs when ReapDecisions is true. Zero uses
	// DefaultReapDecisionsCadence (90s). The 5s poll cadence is far too fast for
	// an O(events.jsonl) scan; 90s cuts the cost ~18× with zero correctness impact
	// (orphan latency = Offline-cutoff + one interval, within SPEC §5). Tests may
	// set a small positive value to exercise the cadence gate quickly. Configurable
	// via keeper.cadence.reap_decisions_cadence in config.yaml. Refs: hk-jrftk.
	ReapDecisionsCadence time.Duration

	// ReapDecisionsFn, when non-nil, replaces presence.ReapOrphanedDecisions in
	// maybeReapOrphanedDecisions. Set to a counting spy in unit tests to verify
	// the cadence gate bounds invocations without real events.jsonl I/O.
	// Refs: hk-jrftk.
	ReapDecisionsFn func(ctx context.Context, eventsPath string, emitter presence.Emitter) (presence.ReapResult, error)

	// OnDemandRestart, when true, marks this agent as one that owns the on-demand
	// restart-now path (the keeper drives clear→resume when the agent runs
	// `harmonik keeper restart-now`). It is auto-set for the captain in applyDefaults
	// and retained for backward compatibility, but the warn-text SELECTION is now
	// governed by selectWarnText (hk-vs4u): the actionable self-service form fires
	// when SelfServiceEnabled AND (captain OR crew-with-crews-enabled) AND a primary
	// SID AND CrispIdle. The keeper band is UNCHANGED — neither this flag nor the
	// selection widens the warn or act thresholds; only the injected text differs.
	//
	// Auto-set to true when AgentName=="captain" (see applyDefaults). Refs: hk-xjlq,
	// ON-059, hk-vs4u.
	OnDemandRestart bool

	// DefaultWarnText, when non-empty, overrides the compiled-in wrapUpWarningText
	// constant injected at warn crossings for non-captain agents (OnDemandRestart=false).
	// Empty (or InjectFn non-nil) → compiled default is used.
	// Sourced from .harmonik/config.yaml keeper.warn_messages.default_warn_text.
	// Refs: hk-lhu2.
	DefaultWarnText string

	// ActionableWarnText, when non-empty, overrides the compiled-in ActionableWarnText
	// output injected at the warn crossing when the actionable self-service form is
	// selected (see selectWarnText). Empty (or InjectFn non-nil) → the compiled
	// ActionableWarnText is used. A custom override is REQUIRED to still carry the
	// verbatim "harmonik keeper restart-now" command token; selectWarnText falls back
	// to the compiled text when the override drops it, so the required handshake
	// command can never be silently lost.
	//
	// This is the SINGLE config key for the actionable advisory: the deprecated
	// keeper.warn_messages.on_demand_warn_text aliases onto it (mapped in
	// projectconfig.go with a log warning), kept as a RECOGNIZED key so old strict
	// configs (hk-9f3f) do not hard-error.
	// Sourced from .harmonik/config.yaml keeper.warn_messages.actionable_warn_text.
	// Refs: hk-vs4u, hk-lhu2.
	ActionableWarnText string

	// SelfServiceEnabled gates the actionable self-service restart handshake form of
	// the warn text. When false, every warn injects the lighter finish-the-turn
	// advisory regardless of agent/idle/SID. Threaded from
	// keeper.self_service.enabled. Refs: hk-vs4u, hk-4gtu.
	SelfServiceEnabled bool

	// SelfServiceCrewsEnabled, when true, extends the actionable form to CREW agents
	// (the captain always gets it when SelfServiceEnabled). DEFAULT is TRUE — crews
	// self-restart; it is not the captain's job to babysit them (operator decision,
	// hk-vs4u). The unset→true resolution happens in ResolveKeeperConfig (a *bool in
	// the raw config), so an absent crews_enabled reaches here as true and an explicit
	// `crews_enabled: false` reaches here as false. Threaded from
	// keeper.self_service.crews_enabled. Refs: hk-vs4u.
	SelfServiceCrewsEnabled bool

	// SelfServiceGraceSeconds and SelfServiceInstructOnlyWhenIdle are threaded for
	// completeness (hk-vs4u defines the self_service block end-to-end). The watcher's
	// idle gate already uses CrispIdle; these carry the configured intent to the
	// resolved struct so future tuning has a consumer. Refs: hk-vs4u, hk-4gtu.
	SelfServiceGraceSeconds         int
	SelfServiceInstructOnlyWhenIdle bool

	// HeartbeatEnabled turns on the keeper-side gauge heartbeat (hk-81wk). When
	// enabled, every tick on which the gauge has aged past HeartbeatThreshold while
	// the tmux pane is still alive re-writes .ctx with a fresh timestamp (and a
	// transcript-derived token count when available), so the gauge NEVER goes stale
	// on a live agent — the dominant no_gauge:stale failure that killed BOTH keeper
	// triggers. Requires TmuxTarget to be non-empty (the pane-alive gate); a no-op
	// otherwise, which keeps the no-tmux unit-test path unchanged. The standalone
	// `harmonik keeper` process enables it (keeper_cmd.go). Default: false.
	// Refs: hk-81wk; codename:keeper-redesign.
	HeartbeatEnabled bool

	// HeartbeatThreshold is the gauge age at which the heartbeat refreshes .ctx.
	// MUST be < Staleness so the refresh lands before the stale branch is reached.
	// Default: Staleness/2.
	// Refs: hk-81wk.
	HeartbeatThreshold time.Duration

	// TranscriptDir overrides the Claude Code transcript projects directory the
	// heartbeat reads to derive the live token count. When empty it is derived as
	// ~/.claude/projects/<munged ProjectDir>. Set in tests. A wrong/empty value
	// only loses token freshness — the heartbeat still carries the last-good
	// reading forward, so gauge liveness is unaffected.
	// Refs: hk-81wk.
	TranscriptDir string

	// HeartbeatMaxMisses is the number of consecutive derive-miss ticks before
	// the heartbeat stops writing the gauge file. Zero (default) uses
	// MaxHeartbeatMisses (12). Set to a small value in tests to exercise the
	// miss-budget path without waiting 12 real ticks. Refs: hk-lal8.
	HeartbeatMaxMisses int

	// DeriveCacheTTL is how long a successful deriveContextTokens result is
	// reused before the transcript is re-scanned. Zero uses DefaultDeriveCacheTTL
	// (30 s). Tests may set a small positive value to exercise cache expiry
	// without waiting 30 real seconds. Refs: hk-div6c.
	DeriveCacheTTL time.Duration

	// ── Gauge-independent live-pane recovery (hk-75mr) ───────────────────────
	// The respawn path (RespawnCmd) only fires when the pane has gone IDLE (the
	// agent exited). It cannot recover an agent that is hung MID-TURN: the pane
	// stays alive, the gauge goes stale, and a /clear inject cannot reach a hung
	// turn. LiveRecover is the gauge-INDEPENDENT last resort for exactly that
	// case — a gated ForceRestart. Every gate is fail-closed; the action runs
	// ONLY when ALL hold (see maybeLivePaneRecover):
	//   - LiveRecoverFn is wired AND TmuxTarget is non-empty;
	//   - the gauge has been stale for at least LiveRecoverGrace (>> RespawnGrace,
	//     so the much-shorter idle-respawn path always wins for an exited agent);
	//   - the pane is ALIVE (IsPaneAliveFn — a non-shell command);
	//   - NO human operator is actively attached (OperatorAttachedFn, the hk-0t5s
	//     keystroke-recency discriminator) — never force-restart under an operator;
	//   - the agent is NOT blocked on an open decision (hitl-decisions K6);
	//   - the LiveRecoverCooldown since the last attempt has elapsed;
	//   - the bound .sid identity is a valid UUIDv4 (ReadSidFn + isPrimarySID);
	//     an absent/invalid .sid fails CLOSED (no recovery).

	// LiveRecoverFn, when non-empty, is the gated ForceRestart last-resort action
	// (NOT a /clear inject). When nil the live-pane recovery path is disabled
	// (fail-closed: no action wired → no recovery). The closure itself MUST also
	// refuse on a non-UUIDv4 bound identity (defense-in-depth); the watcher gate
	// already enforces this via ReadSidFn + isPrimarySID. Refs: hk-75mr.
	LiveRecoverFn func(ctx context.Context, agentName string) error

	// LiveRecoverGrace is the minimum gauge-staleness before a live-pane recovery
	// is attempted. MUST be >> RespawnGrace (default 20s) so a briefly-stale gauge
	// on a live agent is never force-restarted prematurely, and so an EXITED agent
	// is always handled by the much-shorter idle-respawn path first.
	// Default: 5m. Refs: hk-75mr.
	LiveRecoverGrace time.Duration

	// LiveRecoverCooldown is the minimum duration between consecutive live-pane
	// recovery attempts. Prevents a tight force-restart loop. Default: 5m.
	// Refs: hk-75mr.
	LiveRecoverCooldown time.Duration

	// IsPaneAliveFn reports whether the tmux pane at target is running a NON-shell
	// command (the managed agent is still present — hung, not exited). When nil,
	// IsPaneAlive is used. Set in tests to control the check without real tmux.
	// Refs: hk-75mr.
	IsPaneAliveFn func(ctx context.Context, target string) bool

	// OperatorAttachedFn reports whether a human operator is ACTIVELY attached to
	// the target tmux session (keystroke recency, hk-0t5s). When true, live-pane
	// recovery is suppressed — never force-restart a pane a human is driving. When
	// nil, OperatorAttached is used. Refs: hk-75mr, hk-0t5s.
	OperatorAttachedFn func(target string) bool

	// ReadSidFn reads the bound identity from the single-writer .sid channel
	// (hk-8prq). Live-pane recovery is gated on the value being a valid UUIDv4
	// (isPrimarySID); an absent/invalid .sid fails CLOSED. When nil,
	// ReadSessionIDFile is used. Set in tests. Refs: hk-75mr, hk-8prq.
	ReadSidFn func(projectDir, agent string) (string, time.Time, error)

	// ResolveTmuxTargetFn derives a canonical tmux target from projectDir+agentName
	// when the configured TmuxTarget fails the IsPaneAliveFn probe — the stored
	// target may be stale or mangled (the B3 watch-restall class, hk-9cqtm). When
	// nil, ResolveTmuxTarget(projectDir, agentName, "", nil) is used. Set in tests
	// to control re-resolution without real tmux. Refs: hk-9cqtm.
	ResolveTmuxTargetFn func(projectDir, agentName string) string

	// SleepingCheckFn reports whether the session identified by sessionID is
	// currently parked by the QuiesceArbiter (.harmonik/.sleeping.<sessionID>
	// marker, M1 / hk-jeby). When it returns true, the keeper suppresses BOTH
	// warn pane-injection AND cycle dispatch so the sleeping session is not woken
	// by its own keeper (M3 / hk-l3gs, Risk 1 option a). The keeper state machine
	// (warnArmed/warnFired) stays intact; only tmux delivery is gated, so the
	// keeper cooperates rather than fights. When nil, IsSleeping is used.
	// Refs: hk-l3gs, hk-jeby.
	SleepingCheckFn func(projectDir, sessionID string) bool

	// HoldTTL is the keeper HOLD timer backstop; zero → DefaultHoldTTL.
	HoldTTL time.Duration

	// HeldCheckFn reports whether a fresh, session-scoped operator HOLD is active
	// (D5). maybeRespawn and maybeLivePaneRecover return early (respawn/restart
	// suspended) when true, while WARN still fires. Auto-reverts structurally
	// (keyed by the re-minted session-id) plus a timer backstop. The hard-ceiling
	// restart deliberately does NOT consult this — true overflow protection beats a
	// hold. When nil, a closure over IsHeld(.,.,HoldTTL) is used. Refs: hk-9waz.
	HeldCheckFn func(projectDir, agent string) bool

	// WarnCooldown is the minimum duration the warn state machine waits after a
	// warn fires before re-arming for a second crossing (dip-rise cooldown).
	// A transient dip below the threshold followed immediately by a rise back above
	// is treated as one event during this window. Default: warnCooldown (30s).
	// Set to 0 in tests that deliberately exercise multi-crossing behaviour.
	// Refs: hk-sol6.
	WarnCooldown time.Duration

	// NoGaugeBackoff is retained for config backward compatibility
	// (.harmonik/config.yaml keeper.cadence.no_gauge_backoff) but is no longer
	// consumed by the watcher loop. hk-1q7bt replaced the per-tick backoff+re-emit
	// interval with transition-only suppression (maybeEmitNoGauge): session_keeper_no_gauge
	// now fires exactly once per reason-transition, making the backoff window moot.
	// applyDefaults still fills this from DefaultNoGaugeBackoff so config entries
	// do not hard-error; the resolved value is a no-op. Refs: hk-4gtu, hk-sol6, hk-1q7bt.
	NoGaugeBackoff time.Duration

	// ── Backstop 2: SID-independent hard-ceiling failsafe (hk-34ac) ──────────

	// HardCeilingRestartFn, when non-nil, enables the SID-independent hard-ceiling
	// backstop. When any watched pane's token count meets or exceeds
	// HardCeilingAbsTokens (280 000), this function is called to force a
	// handoff+restart regardless of whether the SID binding is correct. The
	// cooldown (HardCeilingCooldown) prevents tight restart loops.
	//
	// When nil the hard-ceiling backstop is disabled (fail-closed: no action
	// wired → no restart). Set in production to a Cycler.MaybeRun wrapper or an
	// on-demand restart function; set to a spy in tests.
	// Refs: hk-34ac.
	HardCeilingRestartFn func(ctx context.Context, agentName string) error

	// HardCeilingTokens is the SID-independent absolute-token hard ceiling: when
	// a watched pane's token count meets or exceeds this value the hard-ceiling
	// backstop trips. applyDefaults fills it with DefaultHardCeilingTokens
	// (280 000) when zero, so the live gate reads w.cfg.HardCeilingTokens rather
	// than the bare const and the emitted session_keeper_hard_ceiling event
	// reports the EFFECTIVE configured value. Refs: hk-n6kn (const→field).
	HardCeilingTokens int64

	// HardCeilingMode selects the backstop's action at the ceiling. Zero value =
	// HardCeilingModeAlarm (operator-chosen default). The gate CONSULTS this field
	// (hk-z8d0): off → no-op; alarm → emit only (even when HardCeilingRestartFn is
	// nil); restart → emit + call HardCeilingRestartFn (degrades to alarm-only when
	// the fn is nil). The auto-restart is EXCLUSIVE to the SID-independent
	// blind/foreign path — the normal SID-matched path restarts via force_act at
	// the act/force cycle, never via this ceiling. Refs: hk-n6kn (type), hk-z8d0
	// (gate wiring; resolves hk-746u dormant-failsafe).
	HardCeilingMode HardCeilingMode

	// HardCeilingCooldown is the minimum duration between consecutive
	// hard-ceiling restart attempts. Prevents tight restart loops when the token
	// count remains above the ceiling across multiple ticks.
	// Default: 5 minutes. Refs: hk-34ac.
	HardCeilingCooldown time.Duration

	// BlindKeeperThreshold is the minimum duration of continuous foreign_session
	// rejection before the blind-keeper alarm (session_keeper_blind) fires. In
	// production this is 5 minutes; it may be set to a much shorter value in tests
	// so the alarm can be exercised without sleeping 5 real minutes.
	// Default: 5 minutes. Refs: hk-34ac, hk-nlio.
	BlindKeeperThreshold time.Duration

	// WarnOnly, when true, restricts the keeper to warn-only mode: it emits
	// session_keeper_warn events and injects the wrap-up advisory into the pane
	// when the context threshold is crossed, but NEVER triggers a restart cycle,
	// respawn, or live-pane recovery. RespawnCmd and LiveRecoverFn are both
	// ignored when WarnOnly is true. This is the correct mode for crew-session
	// keepers: the captain, not the keeper, decides when to restart a crew.
	// Set via --warn-only on `harmonik keeper`. Refs: hk-yfcc.
	WarnOnly bool

	// ── Backstop B: operator-visible WARN channel (hk-ehm8s) ─────────────────
	//
	// OperatorWarnFn, when non-nil, is called at the WARN upward crossing to
	// deliver an out-of-band operator notification — so operators watching via
	// remote-control / iOS / a pane they are not actively watching see the
	// 200K warning before any ACT cycle fires.
	//
	// Called in the same warnArmed && !warnFired block as emitWarn, so it
	// inherits the existing warn_cooldown throttle: exactly once per upward
	// crossing, never on every poll tick while the context remains above warn.
	// Pane injection is unchanged (additive, not replacing).
	//
	// Nil → no operator-channel notification (fail-closed). In production, set
	// to keeperOperatorWarnFn(projectDir, agentName) in keeper_cmd.go. In
	// tests, wire a recording spy. Refs: hk-ehm8s.
	OperatorWarnFn func(ctx context.Context, sessionID string, tokens, warnTokens, actTokens int64)

	// OnWarnRearmFn is a TEST-ONLY observability hook. When non-nil, it is
	// invoked in the below-threshold re-arm branch immediately after the warn
	// state machine re-arms (warnArmed=true). It is nil in production (zero
	// behaviour change) and exists solely to let tests deterministically
	// synchronize on the transient dip observation instead of racing a fixed
	// sleep against the poll interval. Refs: hk-me8ru (de-flake).
	OnWarnRearmFn func()

	// OnPollTickFn is a TEST-ONLY observability hook. When non-nil, it is
	// invoked at the top of every poll-tick iteration — immediately after a tick
	// is received and BEFORE any processing — so a test driving the loop with a
	// substrate.FakeClock can advance virtual time one poll interval at a time in
	// deterministic lockstep with the loop, instead of racing a fixed real-time
	// window whose iteration count is nondeterministic under -race starvation. It
	// is nil in production (zero behaviour change). Refs: hk-3dn16 (de-flake).
	OnPollTickFn func()
}

// applyDefaults fills in zero-valued duration / pct fields.
func (c *WatcherConfig) applyDefaults() {
	if c.Clock == nil {
		c.Clock = substrate.SystemClock{}
	}
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultPollInterval
	}
	// Warn-band defaults are sourced from thresholds.go (the single source of
	// truth shared with CyclerConfig.applyDefaults). Refs: hk-bpkv.
	if c.WarnPct <= 0 {
		c.WarnPct = defaultWarnPct
	}
	if c.WarnAbsTokens <= 0 {
		c.WarnAbsTokens = defaultWarnAbsTokens
	}
	if c.FallbackWindowSize <= 0 {
		c.FallbackWindowSize = defaultFallbackWindowSize
	}
	if c.WarnPctCeil <= 0 {
		c.WarnPctCeil = defaultWarnPctCeil
	}
	if c.IdleQuiesce <= 0 {
		c.IdleQuiesce = DefaultIdleQuiesce
	}
	if c.Staleness <= 0 {
		c.Staleness = DefaultStaleness
	}
	// HeartbeatThreshold defaults to half of Staleness: refresh well before the
	// stale branch (≈60 s at the 120 s default) so a live agent's gauge never
	// reaches Staleness. Refs: hk-81wk.
	if c.HeartbeatThreshold <= 0 {
		c.HeartbeatThreshold = c.Staleness / 2
	}
	// HeartbeatMaxMisses defaults to the package constant. Tests may set a
	// smaller value to exercise the miss-budget path quickly. Refs: hk-lal8.
	if c.HeartbeatMaxMisses <= 0 {
		c.HeartbeatMaxMisses = DefaultMaxHeartbeatMisses
	}
	// DeriveCacheTTL defaults to DefaultDeriveCacheTTL (30 s). Tests set a
	// smaller value to exercise cache expiry without long wall-clock delays.
	// Refs: hk-div6c.
	if c.DeriveCacheTTL <= 0 {
		c.DeriveCacheTTL = DefaultDeriveCacheTTL
	}
	// ReapDecisionsCadence defaults to DefaultReapDecisionsCadence (90s). Tests
	// set a small positive value to exercise the cadence gate quickly. Refs: hk-jrftk.
	if c.ReapDecisionsCadence <= 0 {
		c.ReapDecisionsCadence = DefaultReapDecisionsCadence
	}
	if c.ReadManagedSessionFn == nil {
		c.ReadManagedSessionFn = ReadManagedSessionID
	}
	if c.WriteManagedSessionFn == nil {
		c.WriteManagedSessionFn = WriteManagedSessionID
	}
	if c.RespawnGrace <= 0 {
		c.RespawnGrace = DefaultRespawnGrace
	}
	if c.RespawnCooldown <= 0 {
		c.RespawnCooldown = DefaultRespawnCooldown
	}
	if c.IsPaneIdleFn == nil {
		c.IsPaneIdleFn = IsPaneIdle
	}
	if c.SelfHintInjectFn == nil {
		c.SelfHintInjectFn = InjectText
	}
	// Live-pane recovery defaults (hk-75mr). LiveRecoverGrace (5m) is MUCH larger
	// than RespawnGrace (20s) so an EXITED agent is always handled by the
	// idle-respawn path long before live recovery considers force-restarting a
	// (possibly briefly) stale-but-alive pane — the anti-premature-reap invariant.
	if c.LiveRecoverGrace <= 0 {
		c.LiveRecoverGrace = DefaultLiveRecoverGrace
	}
	if c.LiveRecoverCooldown <= 0 {
		c.LiveRecoverCooldown = DefaultLiveRecoverCooldown
	}
	if c.IsPaneAliveFn == nil {
		c.IsPaneAliveFn = IsPaneAlive
	}
	if c.OperatorAttachedFn == nil {
		c.OperatorAttachedFn = OperatorAttached
	}
	if c.ReadSidFn == nil {
		c.ReadSidFn = ReadSessionIDFile
	}
	if c.ResolveTmuxTargetFn == nil {
		c.ResolveTmuxTargetFn = func(projectDir, agentName string) string {
			return ResolveTmuxTarget(projectDir, agentName, "", nil)
		}
	}
	if c.SleepingCheckFn == nil {
		c.SleepingCheckFn = IsSleeping
	}
	if c.HoldTTL <= 0 {
		c.HoldTTL = DefaultHoldTTL
	}
	if c.HeldCheckFn == nil {
		ttl := c.HoldTTL
		c.HeldCheckFn = func(projectDir, agent string) bool { return IsHeld(projectDir, agent, ttl) }
	}
	// WarnCooldown: default to warnCooldown (30s) when not explicitly set.
	// Tests that need multi-crossing behaviour must set WarnCooldown to a small
	// positive value (e.g. 1ms) rather than 0, since 0 is the zero-value sentinel
	// that triggers this default. The state machine treats 0 as "no cooldown" after
	// applyDefaults runs — but applyDefaults replaces 0 with 30s, so 0 is never
	// seen at runtime unless explicitly forced by passing a *negative* duration
	// (which applyDefaults clamps to 0 to disable the gate). Refs: hk-sol6.
	if c.WarnCooldown < 0 {
		c.WarnCooldown = 0 // negative sentinel → disable cooldown
	} else if c.WarnCooldown == 0 {
		c.WarnCooldown = DefaultWarnCooldown // zero sentinel → use production default
	}
	// NoGaugeBackoff: filled for backward config compat; no longer consumed by the
	// watcher loop (hk-1q7bt replaced the backoff+re-emit with transition-only).
	if c.NoGaugeBackoff <= 0 {
		c.NoGaugeBackoff = DefaultNoGaugeBackoff
	}
	// Hard-ceiling threshold (hk-n6kn): fill from DefaultHardCeilingTokens
	// (280 000, byte-identical to the prior bare const) when zero so the live
	// gate and the emitted event report the EFFECTIVE configured value.
	if c.HardCeilingTokens <= 0 {
		c.HardCeilingTokens = DefaultHardCeilingTokens
	}
	// HardCeilingMode zero value IS HardCeilingModeAlarm (operator-chosen
	// default), so no normalization is required here — the zero value is
	// already alarm-safe. Refs: hk-n6kn.
	// Hard-ceiling backstop cooldown (hk-34ac). Default: 5 minutes.
	if c.HardCeilingCooldown <= 0 {
		c.HardCeilingCooldown = DefaultHardCeilingCooldown
	}
	// Blind-keeper alarm threshold (hk-34ac). Default: 5 minutes.
	// Tests set this to a small value to exercise the alarm without sleeping.
	if c.BlindKeeperThreshold <= 0 {
		c.BlindKeeperThreshold = DefaultBlindKeeperThreshold
	}
	// Auto-enable on-demand restart UX for the captain agent. The captain uses
	// 'harmonik keeper restart-now' (ON-059) rather than the keeper's auto-cycle,
	// so the warn injection must instruct it accordingly. The band is unchanged.
	// Refs: hk-xjlq, ON-059.
	if !c.OnDemandRestart && c.AgentName == "captain" {
		c.OnDemandRestart = true
	}
	// EventsJSONLPath is read by BOTH the K5 orphan reaper (ReapDecisions) and the
	// K6 respawn-exemption (blockedOnOpenDecision). Derive it whenever it is unset
	// and a project dir is known — K6's exemption must consult the open-decision
	// set even when the K5 reaper is disabled.
	// Refs: hk-061 (K5), hk-50f (K6).
	if c.EventsJSONLPath == "" && c.ProjectDir != "" {
		c.EventsJSONLPath = filepath.Join(c.ProjectDir, ".harmonik", core.EventsJSONLPath)
	}
}

// belowWarnThreshold reports whether the gauge reading is below the warn
// threshold. Uses the absolute-token gate ONLY when both Tokens and WindowSize
// are present; otherwise falls back to Pct vs WarnPct. This is byte-for-byte the
// cycler's CyclerConfig.belowWarnThreshold gate (cycle.go) — the two MUST agree,
// or warn and cycle decide on different bases (the F45 Tokens-vs-Pct split-brain:
// the watcher previously fabricated a FallbackWindowSize for the pct-ceil cap when
// WindowSize==0, so on a large-window session reporting tokens-but-no-window it
// fired warn at ~140k tokens — far below the configured warn_pct, recording
// pct < warn_pct in the event log). With the window unknown the pct comparison is
// the only trustworthy signal, matching the cycler. Refs: hk-jgzg (F45), hk-bpkv.
func (c *WatcherConfig) belowWarnThreshold(cf *CtxFile) bool {
	if cf.Tokens > 0 && cf.WindowSize > 0 {
		// pct<WarnPct is a NECESSARY condition: even if tokens exceed the abs gate,
		// we must not warn below the configured warn_pct. On a 1M-context (Opus)
		// session the abs gate resolves to min(200k,700k)=200k = ~20% of the
		// window, so without the pct guard, warn fires at pct=20 instead of 80.
		// Refs: hk-lbo9w. Byte-identical logic with CyclerConfig.belowWarnThreshold.
		return cf.Pct < c.WarnPct || cf.Tokens < minAbsOrPctCeil(c.WarnAbsTokens, c.WarnPctCeil, cf.WindowSize)
	}
	return cf.Pct < c.WarnPct
}

// actionableWarnEligible reports whether the ACTIONABLE self-service restart
// handshake warn form should be selected for this agent at this tick, per the
// hk-vs4u gate. ALL of the following must hold:
//   - SelfServiceEnabled (self_service.enabled);
//   - the agent is the captain, OR it is a crew AND SelfServiceCrewsEnabled
//     (crews_enabled, which DEFAULTS TRUE — operator decision: crews self-restart);
//   - the bound SID is a primary lowercase UUIDv4 (IsPrimarySID) — we only instruct
//     a self-restart when the keeper can trust the identity it will act on;
//   - the pane is CrispIdle (the Stop hook fired after the last gauge update) — we
//     instruct a clean-stop procedure only at a clean stop.
//
// The ReadCtxFile-succeeds clause from the spec is implicit: this is called from
// the fresh-gauge path where ctxFile is already a successfully-read gauge.
//
// When this returns false the lighter finish-the-turn advisory is selected, which
// the watcher ALWAYS injects once gaugeQuiesced even when NOT CrispIdle — so a busy
// (non-CrispIdle) session still gets exactly one warn. Refs: hk-vs4u.
func (c *WatcherConfig) actionableWarnEligible(sessionID string, crispIdle bool) bool {
	if !c.SelfServiceEnabled {
		return false
	}
	isCaptain := c.AgentName == "captain"
	if !isCaptain && !c.SelfServiceCrewsEnabled {
		return false
	}
	if !isPrimarySID(sessionID) {
		return false
	}
	return crispIdle
}

// selectWarnText returns the warn text to inject for this tick. When the agent is
// actionable-eligible (actionableWarnEligible) it returns the ACTIONABLE
// self-service restart handshake text (live tokens + band interpolated). A custom
// ActionableWarnText config override is honored ONLY when it still carries the
// verbatim restart-now command token; otherwise the compiled ActionableWarnText is
// used so the required handshake command can never be silently dropped (the bead's
// "custom override CANNOT drop the required command token" invariant).
//
// When NOT eligible it returns the lighter finish-the-turn advisory: DefaultWarnText
// when configured, else the compiled wrapUpWarningText. Refs: hk-vs4u.
//
// operatorAttached is the operator-attached guard (hk-1ryc): when a human operator
// is actively attached to the pane the ACTIONABLE restart instruction is NOT
// injected — issuing the self-restart handshake command mid-keystroke would race
// the operator's own input. The lighter finish-the-turn advisory is selected
// instead so the warn is still delivered (no warn is ever lost) without ever
// instructing a self-restart over an operator's in-flight turn. This mirrors the
// Cycler's act-path guard (cycle.go Gate-7) for the warn-path actionable text.
func (c *WatcherConfig) selectWarnText(cf *CtxFile, crispIdle, operatorAttached bool) string {
	if !operatorAttached && c.actionableWarnEligible(cf.SessionID, crispIdle) {
		compiled := ActionableWarnText(c.AgentName, cf.Tokens, c.WarnAbsTokens, c.actEffectiveTokens())
		if c.ActionableWarnText != "" && containsRestartNowCmd(c.ActionableWarnText) {
			return c.ActionableWarnText
		}
		return compiled
	}
	if c.DefaultWarnText != "" {
		return c.DefaultWarnText
	}
	return wrapUpWarningText
}

// actEffectiveTokens returns the act-band token figure used in the actionable warn
// text. The watcher does not carry the act threshold directly (it lives on the
// Cycler), so it is derived from the warn threshold plus the compiled warn→act gap
// when unavailable — a display-only figure; the real act gate is the Cycler's.
func (c *WatcherConfig) actEffectiveTokens() int64 {
	return c.WarnAbsTokens + (defaultActAbsTokens - defaultWarnAbsTokens)
}

// containsRestartNowCmd reports whether s carries the verbatim restart-now command
// stem ("harmonik keeper restart-now"). Used to validate a custom ActionableWarnText
// override before honoring it — an override that drops the command falls back to the
// compiled text so the required self-restart handshake is never lost. Refs: hk-vs4u.
func containsRestartNowCmd(s string) bool {
	return strings.Contains(s, "harmonik keeper restart-now")
}

// Watcher polls the gauge file and manages the warn-injection state machine.
// It is safe to construct a Watcher and call Run once.
//
// Spec ref: codename:session-keeper §4.2 Phase-1 warn-mode (hk-8vzek).
type Watcher struct {
	cfg     WatcherConfig
	emitter Emitter

	// heartbeatMissCount counts consecutive ticks on which deriveContextTokens
	// returned false. When it exceeds MaxHeartbeatMisses the heartbeat stops
	// writing the gauge file, allowing it to age to genuine staleness so the
	// no_gauge:stale path fires loudly. Reset to 0 on any successful derive.
	// Refs: hk-lal8.
	heartbeatMissCount int

	// heartbeatLastSID is the session_id the heartbeat last targeted for derive.
	// When the effective SID changes (new session detected via .managed or .sid),
	// heartbeatMissCount is reset so the new session gets a fresh derive budget.
	// Refs: hk-4xni9 K1.
	heartbeatLastSID string

	// Heartbeat derive cache — avoids O(filesize) JSONL re-scans on consecutive
	// heartbeat ticks for the same session. Only hits are cached; misses always
	// hit the disk so the miss-budget counter works correctly. Single-threaded:
	// only the Run goroutine calls deriveCachedTokens via maybeHeartbeat.
	// Refs: hk-div6c.
	deriveCacheSID    string
	deriveCacheTokens int64
	deriveCacheExpiry time.Time

	// ── Backstop 1: blind-keeper alarm (hk-34ac) ─────────────────────────────
	// blindSince is the time of the FIRST foreign_session tick in the current
	// continuous blind episode. Zero value means the keeper is not currently
	// blind. Set on the first foreign_session tick; reset on any successful
	// (non-foreign, non-stale) tick.
	blindSince time.Time

	// blindAlarmFired is true when session_keeper_blind has already been emitted
	// for the current blind episode. Prevents re-emission on every tick. Cleared
	// (along with blindSince) when the gauge becomes readable again.
	blindAlarmFired bool

	// lastDashboardNagAt is the wall-clock time of the most recent dashboard
	// staleness pre-nag injection (hk-xg6rw). Zero means no nag has fired yet
	// this session. Gates re-injection to dashboardNagCooldown.
	lastDashboardNagAt time.Time

	// lastNoGaugeReason is the reason string of the most recent
	// session_keeper_no_gauge emission. Empty when the gauge is fresh (initial
	// state, or after a fresh-gauge recovery resets it). Used by
	// maybeEmitNoGauge to implement log-once-per-(agent,reason)-transition:
	// the event fires only when the reason changes, not on every poll tick.
	// Refs: hk-1q7bt.
	lastNoGaugeReason string
}

// NewWatcher constructs a Watcher with the given config and emitter.
// Defaults are applied to zero-valued config fields.
func NewWatcher(cfg WatcherConfig, emitter Emitter) *Watcher {
	cfg.applyDefaults()
	if emitter == nil {
		emitter = NoopEmitter{}
	}
	return &Watcher{cfg: cfg, emitter: emitter}
}

// Run starts the watcher loop. It returns when ctx is cancelled (returning
// ctx.Err()) or on a fatal internal error. Run is intended to be called once.
func (w *Watcher) Run(ctx context.Context) error {
	var (
		// warnArmed is true when pct was below warnPct on the previous tick;
		// when armed, an upward crossing arms the injection.
		warnArmed = true

		// warnFired tracks whether we've emitted the warn event for the current
		// crossing. Reset to false when pct drops below warnPct again.
		warnFired = false

		// pendingInject is true when a warn was emitted but the inject has not
		// yet been delivered (pane was not quiesced on the crossing tick).
		// Cleared when the inject succeeds or when pct resets below warnPct.
		pendingInject = false

		// lastModTime is the mod-time of the gauge file on the previous tick.
		// Used for idle-gate (quiescence detection).
		lastModTime time.Time

		// gaugeStaleSince is the time when the gauge first became stale/absent
		// in the current stale streak. Zero when the gauge is fresh. Used by the
		// respawn path to enforce RespawnGrace. (Refs: hk-3w2)
		gaugeStaleSince time.Time

		// lastRespawnAt is the time of the most recent respawn attempt. Used to
		// enforce RespawnCooldown. Zero when no respawn has occurred. (Refs: hk-3w2)
		lastRespawnAt time.Time

		// lastLiveRecoverAt is the time of the most recent live-pane recovery
		// attempt. Used to enforce LiveRecoverCooldown. Zero when no recovery has
		// occurred this session. (Refs: hk-75mr)
		lastLiveRecoverAt time.Time

		// lastWarnFiredAt is the wall time of the most recent warn-threshold
		// firing. Used by the dip-rise cooldown to suppress a re-fire within
		// warnCooldown of the previous one. Refs: hk-sol6.
		lastWarnFiredAt time.Time

		// hintSentThisSession is latched true after the one-time [KEEPER HINT]
		// injection fires on the first warn crossing of a session. Reset to false
		// when a cycle completes (warnArmed reset path). Refs: hk-lsk5.
		hintSentThisSession = false

		// pendingHint is true when the one-time self-hint crossing has occurred
		// but the [KEEPER HINT] inject has not yet landed. Delivery is deferred
		// to the sleep-gated block below (hk-bzol4) so a parked session is not
		// woken by its own keeper — mirrors pendingInject's deferral. It follows
		// hintSentThisSession's lifecycle (armed on the crossing while the latch
		// is unset; cleared on delivery or on the dip-reset that clears the latch),
		// NOT pendingInject's — so a pending hint survives a transient gauge blip.
		pendingHint = false

		// hardCeilingLastAt is the time of the most recent hard-ceiling restart
		// attempt. Used to enforce HardCeilingCooldown. Zero when no hard-ceiling
		// restart has occurred this session. (Refs: hk-34ac)
		hardCeilingLastAt time.Time

		// lastReapAt is the time of the most recent maybeReapOrphanedDecisions run.
		// The cadence gate skips the O(events.jsonl) scan until ReapDecisionsCadence
		// has elapsed. Zero value means the reaper has not run yet this session
		// (zero → fires on the first tick). Refs: hk-jrftk.
		lastReapAt time.Time
	)

	// Boot-time check: emit no_gauge immediately if gauge is absent or stale.
	// Uses the transition gate so a subsequent ticker tick with the same reason
	// is a no-op. Refs: hk-1q7bt.
	if absent, reason := w.gaugeUnavailable(ctx); absent {
		w.maybeEmitNoGauge(ctx, reason)
	}

	ticker := w.cfg.Clock.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C():
			// TEST-ONLY lockstep hook (hk-3dn16): signal that a poll tick was
			// received, BEFORE any processing, so a FakeClock-driving test can
			// advance virtual time deterministically. Nil in production.
			if w.cfg.OnPollTickFn != nil {
				w.cfg.OnPollTickFn()
			}

			// ── InCycle suppression (SK-017 / D11) ───────────────────────────
			// While the restart cycle is in flight (the reactor is off-Idle),
			// ALL non-cycle tick processing is parked: no warn state machine,
			// no precompact detection, no heartbeat, reaper, or hard-ceiling —
			// only the cycle-detection poll + timer-fire drive the reactor
			// forward. Today the cycle shell drives that loop SYNCHRONOUSLY
			// inside MaybeRun/RunForPrecompact/RunForIdle below (this goroutine
			// blocks there, reproducing the pre-rebuild freeze exactly), so
			// this guard cannot observe an off-Idle reactor; it makes the
			// parked-processing contract explicit and keeps it holding if the
			// reactor is ever driven asynchronously. Relaxing InCycle is a
			// later, separately-measured change (deferred; SK §11).
			if w.cfg.Cycler != nil && w.cfg.Cycler.InCycle() {
				continue
			}

			// ── hitl-decisions orphan reaper (K5, hk-061) ────────────────────
			// Runs BEFORE the gauge-read branches below (which may `continue` past
			// the rest of the loop body when the gauge is absent/stale/foreign),
			// but gated to ReapDecisionsCadence (default 90s) rather than every
			// tick: the O(events.jsonl) scan does not need 5s latency (orphan
			// latency bound = Offline-cutoff + one reap interval, well within
			// SPEC §5 / N9). The keeper tick is the SOLE emitter of
			// decision_withdrawn(orphaned). Refs: hk-jrftk.
			w.maybeReapOrphanedDecisions(ctx, &lastReapAt)

			// ── dashboard staleness pre-nag (hk-xg6rw, DESIGN §4 rec. B) ─────
			// Runs unconditionally each tick (like the reaper above), independent
			// of gauge state — a wedged/foreign-session captain still needs the
			// nudge before the daemon-side forcing gate trips.
			w.maybeNagDashboardStale(ctx, w.cfg.Clock.Now())

			ctxFile, modTime, err := ReadCtxFile(w.cfg.ProjectDir, w.cfg.AgentName)

			// ── gauge absent ────────────────────────────────────────────────
			if errors.Is(err, os.ErrNotExist) {
				w.maybeEmitNoGauge(ctx, "absent")
				warnArmed = true
				warnFired = false
				pendingInject = false
				// An absent gauge is a no_gauge condition, not a foreign one:
				// break any continuous foreign_session blind episode so the
				// 5-min blind clock restarts on the next foreign streak (hk-34ac).
				w.blindSince = time.Time{}
				w.blindAlarmFired = false
				if gaugeStaleSince.IsZero() {
					gaugeStaleSince = w.cfg.Clock.Now()
				}
				w.maybeRespawn(ctx, gaugeStaleSince, &lastRespawnAt)
				w.maybeLivePaneRecover(ctx, gaugeStaleSince, &lastLiveRecoverAt)
				continue
			}
			if err != nil {
				// parse / stat error: treat as absent, log and continue
				slog.WarnContext(ctx, "keeper: read ctx file", "err", err)
				w.maybeEmitNoGauge(ctx, "absent")
				// Mirror the absent branch: re-arm the warn state machine so the
				// next upward crossing fires a fresh warn (was omitted here).
				warnArmed = true
				warnFired = false
				pendingInject = false
				// Break any continuous foreign_session blind episode (see above).
				w.blindSince = time.Time{}
				w.blindAlarmFired = false
				if gaugeStaleSince.IsZero() {
					gaugeStaleSince = w.cfg.Clock.Now()
				}
				w.maybeRespawn(ctx, gaugeStaleSince, &lastRespawnAt)
				w.maybeLivePaneRecover(ctx, gaugeStaleSince, &lastLiveRecoverAt)
				continue
			}

			// ── keeper-side heartbeat (hk-81wk) ──────────────────────────────
			// The gauge's only other writer is keeper-statusline.sh, which fires
			// on UI repaint and SKIPS the write on NA/absent pct (after /clear, or
			// when a session stops repainting). On a LIVE agent the gauge can then
			// age into the stale branch below and `continue` past BOTH triggers.
			// Once the gauge is aging while the pane is still alive, re-write .ctx
			// with a fresh ts (transcript-derived tokens when available) so a live
			// agent's gauge NEVER goes stale. No-op when the pane is idle so the
			// respawn path stays intact.
			w.maybeHeartbeat(ctx, ctxFile, w.cfg.Clock.Since(modTime))

			// ── gauge stale ──────────────────────────────────────────────────
			if w.cfg.Clock.Since(modTime) >= w.cfg.Staleness {
				w.maybeEmitNoGauge(ctx, "stale")
				warnArmed = true
				warnFired = false
				pendingInject = false
				// A stale gauge is a no_gauge condition, not a foreign one:
				// break any continuous foreign_session blind episode (hk-34ac).
				w.blindSince = time.Time{}
				w.blindAlarmFired = false
				if gaugeStaleSince.IsZero() {
					gaugeStaleSince = w.cfg.Clock.Now()
				}
				w.maybeRespawn(ctx, gaugeStaleSince, &lastRespawnAt)
				w.maybeLivePaneRecover(ctx, gaugeStaleSince, &lastLiveRecoverAt)
				continue
			}

			// ── session_id binding ────────────────────────────────────────────
			// Identity is sourced from the single-writer <agent>.sid channel
			// (hk-8prq), which ReadCtxFile folds into ctxFile.SessionID whenever it
			// is present and well-formed. The daemon never writes .sid, so a real
			// session's SessionID here is already the authoritative interactive id.
			// The old keeper rebind surface was removed with hk-3391; identity logic
			// now uses a single cheap guard:
			//   - foreign session: .managed is bound and the live id differs (two
			//     concurrent same-agent sessions, last-writer on the shared .sid) —
			//     treat as absent so warn/cycle logic stays consistent.
			// Stale-binding recovery is handled by the cycler's re-arm on
			// clear→resume (it clears .managed), not by an in-watcher auto-clear.
			// (Refs: hk-3391, hk-8prq; supersedes hk-igt/hk-mejt/hk-mzdm/hk-lap/hk-0tvm heuristics.)
			if managedSID, managedErr := w.cfg.ReadManagedSessionFn(w.cfg.ProjectDir, w.cfg.AgentName); managedErr != nil {
				slog.WarnContext(ctx, "keeper: read managed session_id", "err", managedErr)
				// Fall through on read error to avoid silent monitoring gaps.
			} else if managedSID != "" && ctxFile.SessionID != "" && ctxFile.SessionID != managedSID {
				// Potential foreign session or same-agent post-external-/clear.
				// Re-read the authoritative .sid channel before rejecting: if the live
				// .sid is a valid UUIDv4 and matches the gauge's session_id, this is
				// the SAME agent with a new session after an external /clear — adopt it
				// by rewriting .managed. Reject as truly foreign only when the gauge's
				// session_id does NOT match the authoritative .sid (two concurrent
				// sessions writing to shared .ctx). Refs: hk-1tn2.
				liveSID, _, sidErr := w.cfg.ReadSidFn(w.cfg.ProjectDir, w.cfg.AgentName)
				if sidErr == nil && isPrimarySID(liveSID) && liveSID == ctxFile.SessionID {
					// Same agent, new session after external /clear — adopt.
					slog.InfoContext(ctx, "keeper: re-resolving .managed after external /clear; adopting new session_id",
						"agent", w.cfg.AgentName, "old_sid", managedSID, "new_sid", liveSID)
					if adoptErr := w.cfg.WriteManagedSessionFn(w.cfg.ProjectDir, w.cfg.AgentName, liveSID); adoptErr != nil {
						slog.WarnContext(ctx, "keeper: adopt managed session_id", "agent", w.cfg.AgentName, "err", adoptErr)
						// Non-fatal: fall through and keep monitoring with the new id.
					}
					// Fall through to fresh-gauge handling below.
				} else {
					// True foreign session — treat as absent.
					// Log and emit only on transition to foreign_session; suppress
					// the per-tick slog.Warn flood for continuously-foreign gauges.
					// Refs: hk-1q7bt.
					if w.maybeEmitNoGauge(ctx, "foreign_session") {
						slog.WarnContext(ctx, "keeper: gauge session_id mismatch; ignoring foreign session",
							"agent", w.cfg.AgentName, "expected_sid", managedSID, "got_sid", ctxFile.SessionID)
					}
					warnArmed = true
					warnFired = false
					pendingInject = false

					// ── Backstop 1: blind-keeper alarm (hk-34ac) ─────────────────
					// Track continuous foreign_session episodes. Arm blindSince on
					// the first foreign tick of each episode; fire session_keeper_blind
					// once after 5 minutes of continuous blindness. Latch so we emit
					// only once per episode (blindAlarmFired). The latch and timer are
					// reset on the next successful (non-foreign) tick (below).
					if w.blindSince.IsZero() {
						w.blindSince = w.cfg.Clock.Now()
					}
					if !w.blindAlarmFired && w.cfg.Clock.Since(w.blindSince) > w.cfg.BlindKeeperThreshold {
						blindSeconds := int64(w.cfg.Clock.Since(w.blindSince).Seconds())
						slog.WarnContext(ctx, "keeper: blind-keeper alarm: continuous foreign_session for >5 min; keeper cannot monitor this pane",
							"agent", w.cfg.AgentName, "managed_sid", managedSID, "live_sid", ctxFile.SessionID,
							"blind_seconds", blindSeconds)
						w.emitBlind(ctx, managedSID, ctxFile.SessionID, blindSeconds)
						w.blindAlarmFired = true
					}

					// ── Backstop 2: SID-independent hard-ceiling failsafe (hk-z8d0) ──
					// Even when the SID is foreign, the ctxFile IS readable (we just
					// parsed it above). This is the ONE place the keeper can act on
					// context overflow while blind: the normal act/force cycle cannot
					// run (the cycler keys on a fresh, SID-matched gauge), so a
					// mis-bound keeper would otherwise silently allow overflow.
					//
					// Mode gating (HardCeilingMode, default alarm) — fixes hk-746u, the
					// dormant-failsafe bug where the emit lived INSIDE the
					// `HardCeilingRestartFn != nil` guard, so alarm mode (and a nil fn)
					// emitted nothing:
					//   - off     → no-op (no emit, no restart).
					//   - alarm   → emit session_keeper_hard_ceiling ONLY. The emit MUST
					//               fire even when HardCeilingRestartFn is nil.
					//   - restart → emit + call HardCeilingRestartFn, cooldown-gated. If
					//               the fn is nil (not wired) it degrades to alarm
					//               (emit only) — never panics.
					// Skip entirely if token count is not available (zero — older .ctx
					// or unreadable field) or below the ceiling.
					//
					// NOTE (hk-9waz): the hard-ceiling restart deliberately OVERRIDES an
					// operator HOLD — true overflow protection beats a hold. No HeldCheckFn
					// gate here, by design.
					if w.cfg.HardCeilingMode != HardCeilingModeOff &&
						ctxFile.Tokens > 0 && ctxFile.Tokens >= w.cfg.HardCeilingTokens {
						wantRestart := w.cfg.HardCeilingMode == HardCeilingModeRestart &&
							w.cfg.HardCeilingRestartFn != nil
						if wantRestart {
							// Restart mode with a wired fn: cooldown-gate the whole
							// emit+restart so we do not thrash. Outside the cooldown the
							// emit is suppressed too (the prior tick already alarmed).
							if hardCeilingLastAt.IsZero() || w.cfg.Clock.Since(hardCeilingLastAt) >= w.cfg.HardCeilingCooldown {
								slog.WarnContext(ctx, "keeper: hard ceiling hit (SID-independent): forcing restart",
									"agent", w.cfg.AgentName, "tokens", ctxFile.Tokens, "hard_ceiling", w.cfg.HardCeilingTokens)
								w.emitHardCeiling(ctx, ctxFile.Tokens)
								_ = w.cfg.HardCeilingRestartFn(ctx, w.cfg.AgentName) //nolint:errcheck // best-effort restart
								hardCeilingLastAt = w.cfg.Clock.Now()
							} else {
								slog.DebugContext(ctx, "keeper: hard ceiling hit but cooldown active; skipping restart",
									"agent", w.cfg.AgentName, "tokens", ctxFile.Tokens)
							}
						} else {
							// Alarm mode, OR restart mode degraded to alarm (fn nil): emit
							// only, cooldown-gated so we alarm at most once per cooldown
							// window rather than every tick the pane sits above ceiling.
							if hardCeilingLastAt.IsZero() || w.cfg.Clock.Since(hardCeilingLastAt) >= w.cfg.HardCeilingCooldown {
								slog.WarnContext(ctx, "keeper: hard ceiling hit (SID-independent): alarm only",
									"agent", w.cfg.AgentName, "tokens", ctxFile.Tokens, "hard_ceiling", w.cfg.HardCeilingTokens,
									"mode", w.cfg.HardCeilingMode.String())
								w.emitHardCeiling(ctx, ctxFile.Tokens)
								hardCeilingLastAt = w.cfg.Clock.Now()
							}
						}
					}

					continue
				}
			}

			// Gauge is fresh (and belongs to the managed session): reset no_gauge
			// tracking so the next absence/stale/foreign fires a fresh transition
			// event. Also clear respawn staleness tracking. Refs: hk-1q7bt.
			w.lastNoGaugeReason = ""
			gaugeStaleSince = time.Time{}

			// ── Backstop 1 reset (hk-34ac) ───────────────────────────────────────
			// Gauge is readable and SID-matched: clear the blind episode so the
			// next foreign_session streak starts a fresh 5-minute clock.
			w.blindSince = time.Time{}
			w.blindAlarmFired = false

			// ── idle-gate ────────────────────────────────────────────────────
			// The pane is considered idle when the gauge file's mod-time has not
			// changed since the previous tick for at least IdleQuiesce.
			gaugeQuiesced := !modTime.IsZero() && !lastModTime.IsZero() &&
				modTime.Equal(lastModTime) &&
				w.cfg.Clock.Since(modTime) >= w.cfg.IdleQuiesce
			lastModTime = modTime

			// ── Phase-2 gate predicates ──────────────────────────────────────
			// CrispIdle: Stop hook fired after the last gauge update.
			// HoldingDispatch: orchestrator has in-flight queue work.
			crispIdle := CrispIdle(w.cfg.ProjectDir, w.cfg.AgentName)
			holdingDispatch := HoldingDispatch(w.cfg.ProjectDir, w.cfg.AgentName)
			slog.DebugContext(ctx, "keeper: gate predicates",
				"agent", w.cfg.AgentName,
				"crisp_idle", crispIdle,
				"holding_dispatch", holdingDispatch,
			)

			// ── Phase-2 cycle dispatch ────────────────────────────────────────
			// Cycler.MaybeRun handles all internal gating (act_pct, CrispIdle,
			// HoldingDispatch, anti-loop). We pass the full ctxFile so the cycler
			// can read pct and session_id directly.
			if w.cfg.Cycler != nil && !w.cfg.WarnOnly {
				if cycleErr := w.cfg.Cycler.MaybeRun(ctx, ctxFile); cycleErr != nil {
					slog.WarnContext(ctx, "keeper: cycle error", "agent", w.cfg.AgentName, "err", cycleErr)
				}
			}

			// ── PreCompact backstop ───────────────────────────────────────────
			// If keeper-precompact-hook.sh blocked a native compaction it writes a
			// .precompact marker. Detect it and run the cycle immediately, skipping
			// the CrispIdle and act_pct gates (the agent is mid-turn when PreCompact
			// fires). RunForPrecompact always clears the marker so the next PreCompact
			// fire gets a clean slate (bounded-fallback contract).
			if w.cfg.Cycler != nil && !w.cfg.WarnOnly && HasPrecompactTrigger(w.cfg.ProjectDir, w.cfg.AgentName) {
				if pcErr := w.cfg.Cycler.RunForPrecompact(ctx, ctxFile); pcErr != nil {
					slog.WarnContext(ctx, "keeper: precompact cycle error", "agent", w.cfg.AgentName, "err", pcErr)
				}
			}

			// ── idle-large-context restart ────────────────────────────────────
			// Restart idle crews with large (≥150K) context below the act
			// threshold to compact context to a small baseline. Gating is
			// handled internally. (Refs: hk-ee81)
			if w.cfg.Cycler != nil && !w.cfg.WarnOnly {
				if idleErr := w.cfg.Cycler.RunForIdle(ctx, ctxFile); idleErr != nil {
					slog.WarnContext(ctx, "keeper: RunForIdle error", "agent", w.cfg.AgentName, "err", idleErr)
				}
			}

			// NOTE: restart-now (captain-initiated) is NO LONGER a watcher-detected
			// marker path. `harmonik keeper restart-now` now drives the
			// ack→/clear→agent-brief SYNCHRONOUSLY in its own process
			// (internal/keeper/restartnow.go) — there is no .restart-now marker for
			// the watcher to poll, which removes the silent-no-op project-dir
			// divergence. Refs: hk-5da7 (was hk-wjzf/ON-059 marker path).

			// ── warn state machine ───────────────────────────────────────────
			if w.cfg.belowWarnThreshold(ctxFile) {
				// Below threshold: reset so the next upward crossing will warn.
				// Dip-rise cooldown (hk-sol6): only re-arm warnArmed if the
				// cooldown period (cfg.WarnCooldown, default 30s) has elapsed
				// since the last warn fire, preventing a transient dip-then-rise
				// from counting as a second event. A zero WarnCooldown disables
				// the gate entirely (used by tests that exercise multi-crossing).
				if lastWarnFiredAt.IsZero() || w.cfg.WarnCooldown == 0 || w.cfg.Clock.Since(lastWarnFiredAt) >= w.cfg.WarnCooldown {
					warnArmed = true
					warnFired = false
					pendingInject = false
					// Reset hint latch on genuine session reset (gauge dropped below
					// warn and cooldown elapsed — new effective session start). Also
					// cancel any undelivered pending hint from the prior crossing.
					hintSentThisSession = false
					pendingHint = false
					// TEST-ONLY observability: signal the re-arm so tests can
					// deterministically wait for the dip to be observed instead
					// of racing a fixed sleep. Nil in production. Refs: hk-me8ru.
					if w.cfg.OnWarnRearmFn != nil {
						w.cfg.OnWarnRearmFn()
					}
				}
				continue
			}

			// At or above the warn threshold.
			if warnArmed && !warnFired {
				// Upward crossing detected — emit the warn event immediately.
				// Inject delivery is deferred until the pane is quiesced; see
				// the pendingInject block below. We must NOT latch warnFired
				// before the inject lands or the retry path is permanently
				// cut off (BUG-1: hk-g4ei7).
				w.emitWarn(ctx, ctxFile)
				// Backstop B (hk-ehm8s): operator-visible out-of-pane notification
				// at the crossing. Subject to the same warnArmed/warnFired gate as
				// emitWarn — fires exactly once per upward crossing, never every tick.
				if w.cfg.OperatorWarnFn != nil {
					w.cfg.OperatorWarnFn(ctx, ctxFile.SessionID, ctxFile.Tokens, w.cfg.WarnAbsTokens, w.cfg.actEffectiveTokens())
				}
				warnFired = true
				warnArmed = false
				lastWarnFiredAt = w.cfg.Clock.Now()
				if w.cfg.TmuxTarget != "" {
					pendingInject = true
				}

				// ── one-time self-hint injection (hk-lsk5) ───────────────────
				// On the FIRST warn crossing of the session, ARM the hint so the
				// agent is nudged to wrap up. Only once per session —
				// hintSentThisSession latches after delivery. Actual tmux delivery
				// is DEFERRED to the sleep-gated block below (hk-bzol4): injecting
				// here fired with no SleepingCheckFn guard and woke a parked
				// session. pendingHint carries the intent to the gated delivery.
				if !hintSentThisSession && w.cfg.TmuxTarget != "" {
					pendingHint = true
				}
			}

			// ── one-time self-hint delivery (hk-lsk5; sleep-gated hk-bzol4) ──
			// Delivers on the crossing tick itself when the session is awake
			// (pendingHint was just armed above), or retries on a later tick once
			// a parked session wakes. Sleep-gated so the keeper never wakes a
			// parked session with its own hint — closing the M3 gap where the hint
			// bypassed the SleepingCheckFn guard that the warn advisory below
			// already honors (watcher.go SleepingCheckFn contract). The hint is a
			// lightweight nudge, so — unlike the warn advisory — it is NOT
			// gauge-quiesce-gated, preserving its prior immediate-delivery timing;
			// only the sleep suppression is added.
			if pendingHint && w.cfg.TmuxTarget != "" {
				if w.cfg.SleepingCheckFn(w.cfg.ProjectDir, ctxFile.SessionID) {
					slog.DebugContext(ctx, "keeper: self-hint suppressed — session is sleeping",
						"agent", w.cfg.AgentName, "session_id", ctxFile.SessionID)
				} else if hintErr := w.cfg.SelfHintInjectFn(ctx, w.cfg.TmuxTarget, keeperHintText(ctxFile.Tokens)); hintErr != nil {
					slog.WarnContext(ctx, "keeper: inject self-hint", "err", hintErr)
				} else {
					hintSentThisSession = true
					pendingHint = false
				}
			}

			// Attempt inject delivery — on the crossing tick or any subsequent tick
			// once the pane has quiesced. Retries on each tick until success so a
			// non-quiesced crossing tick never permanently suppresses injection.
			if pendingInject && gaugeQuiesced {
				// Sleep gate (M3 / hk-l3gs): do not inject into a parked session.
				// warnFired and pendingInject remain true so delivery retries on the
				// next tick once the session wakes (M1 failsafe clears the marker).
				if w.cfg.SleepingCheckFn(w.cfg.ProjectDir, ctxFile.SessionID) {
					slog.DebugContext(ctx, "keeper: inject suppressed — session is sleeping",
						"agent", w.cfg.AgentName, "session_id", ctxFile.SessionID)
					continue
				}
				inject := w.cfg.InjectFn
				if inject == nil {
					// hk-vs4u: select the ACTIONABLE self-service restart handshake
					// text vs the lighter finish-the-turn advisory. The actionable
					// form fires ONLY when ALL of: self_service.enabled AND
					// (captain OR crew-with-crews-enabled) AND a primary (UUIDv4) SID
					// AND CrispIdle. Otherwise the lighter advisory is used — and the
					// lighter advisory ALWAYS injects once gaugeQuiesced (this block),
					// even when NOT CrispIdle, so no session ever loses its warn.
					//
					// hk-1ryc operator-attached guard: when a human operator is
					// actively attached to the pane, suppress the ACTIONABLE restart
					// instruction (it would race the operator's keystrokes mid-turn)
					// and fall back to the lighter advisory. The warn is still
					// delivered; only the self-restart command is withheld until the
					// operator detaches. Checked here (not at the crossing tick) so the
					// live attach state is sampled at delivery time.
					operatorAttached := w.cfg.TmuxTarget != "" && w.cfg.OperatorAttachedFn(w.cfg.TmuxTarget)
					text := w.cfg.selectWarnText(ctxFile, crispIdle, operatorAttached)
					inject = func(ctx context.Context, target string) error {
						return InjectText(ctx, target, text)
					}
				}
				if injectErr := inject(ctx, w.cfg.TmuxTarget); injectErr != nil {
					slog.WarnContext(ctx, "keeper: inject wrap-up warning", "err", injectErr)
				} else {
					pendingInject = false
				}
			}
		}
	}
}

// maybeReapOrphanedDecisions runs one hitl-decisions orphan-reap pass (K5,
// hk-061) when the reaper is enabled (cfg.ReapDecisions) AND the reap cadence
// (cfg.ReapDecisionsCadence, default 90s) has elapsed since the last run.
//
// It emits decision_withdrawn(orphaned, by=keeper) for every open decision whose
// blocked_agent is Offline (an explicit leave beat OR age ≥ presence.StaleCutoff,
// never merely Stale — N9), via cfg.ReapDecisionsFn or the canonical
// presence.ReapOrphanedDecisions.
//
// It is a no-op when ReapDecisions is false or the cadence has not elapsed.
// The emitter is cfg.DecisionEmitter when set, else the watcher's primary
// emitter (the standalone keeper's FileEmitter). A reap error or a per-decision
// emit failure is logged and swallowed — the next cadence window retries (the
// pass is idempotent: the open set is re-read fresh each call, N3).
//
// lastReapAt is updated on every attempt (success or error) so the cadence gate
// bounds the O(events.jsonl) scan frequency regardless of error. Refs: hk-jrftk.
func (w *Watcher) maybeReapOrphanedDecisions(ctx context.Context, lastReapAt *time.Time) {
	if !w.cfg.ReapDecisions {
		return
	}
	if !lastReapAt.IsZero() && w.cfg.Clock.Since(*lastReapAt) < w.cfg.ReapDecisionsCadence {
		return
	}
	*lastReapAt = w.cfg.Clock.Now()
	emitter := w.cfg.DecisionEmitter
	if emitter == nil {
		emitter = w.emitter
	}
	reapFn := w.cfg.ReapDecisionsFn
	if reapFn == nil {
		reapFn = presence.ReapOrphanedDecisions
	}
	res, err := reapFn(ctx, w.cfg.EventsJSONLPath, emitter)
	if err != nil {
		slog.WarnContext(ctx, "keeper: orphan-decision reap", "err", err, "events_path", w.cfg.EventsJSONLPath)
		return
	}
	if res.Reaped > 0 {
		slog.InfoContext(ctx, "keeper: reaped orphaned decisions",
			"agent", w.cfg.AgentName, "reaped", res.Reaped, "open", res.Open, "decision_ids", res.DecisionIDs)
	}
}

// gaugeUnavailable returns (true, reason) when the gauge file is absent or
// stale. Used at boot for the initial no_gauge check.
func (w *Watcher) gaugeUnavailable(ctx context.Context) (bool, string) {
	_, modTime, err := ReadCtxFile(w.cfg.ProjectDir, w.cfg.AgentName)
	if errors.Is(err, os.ErrNotExist) {
		return true, "absent"
	}
	if err != nil {
		slog.WarnContext(ctx, "keeper: read ctx file at boot", "err", err)
		return true, "absent"
	}
	if w.cfg.Clock.Since(modTime) >= w.cfg.Staleness {
		return true, "stale"
	}
	return false, ""
}

// warnCooldown is the minimum duration between warn-threshold firings in the
// same direction (dip-rise cooldown). Prevents a transient dip below the
// threshold immediately followed by a rise from counting as a second event.
// Refs: hk-sol6.
// Alias of the exported DefaultWarnCooldown (thresholds.go single source). hk-gwz6.
const warnCooldown = DefaultWarnCooldown

// keeperHintText renders the one-time self-hint injected on the first
// warn-threshold crossing per session. The live token count from the gauge is
// interpolated so the message reflects the actual context size. When tokens is
// zero (pct-only gauge), falls back to the static ~190K approximation.
// Refs: hk-lsk5.
func keeperHintText(tokens int64) string {
	approxK := int64(190)
	if tokens > 0 {
		approxK = (tokens + 500) / 1000
	}
	return fmt.Sprintf("[KEEPER HINT] Context is at ~%dK tokens. Consider wrapping up the current task and preparing a handoff soon.", approxK)
}

// maybeEmitNoGauge emits session_keeper_no_gauge and logs only when the
// no-gauge reason transitions from the last emitted reason
// (log-once-per-(agent,reason)-transition). The event fires on:
//   - first call from the initial "" state to any non-empty reason (gauge went absent);
//   - any subsequent call where reason differs from w.lastNoGaugeReason (e.g.
//     "absent" → "stale", or any reason → "foreign_session").
//
// No event fires when reason equals w.lastNoGaugeReason: continuous absence does
// not re-emit on every poll tick — the event is a TRANSITION signal, not a
// persistent-state alarm. The caller resets w.lastNoGaugeReason to "" on
// fresh-gauge recovery so the next absence starts a fresh transition.
//
// Returns true when an event was emitted. Refs: hk-1q7bt.
func (w *Watcher) maybeEmitNoGauge(ctx context.Context, reason string) bool {
	if w.lastNoGaugeReason == reason {
		return false
	}
	w.emitNoGauge(ctx, reason)
	w.lastNoGaugeReason = reason
	return true
}

// emitNoGauge emits the session_keeper_no_gauge event.
// When SuppressNoGauge is set the call is a no-op (F21: dogfood/test sessions
// without a real gauge writer otherwise produce x66+ events per session).
func (w *Watcher) emitNoGauge(ctx context.Context, reason string) {
	if w.cfg.SuppressNoGauge {
		return
	}
	payload := core.SessionKeeperNoGaugePayload{
		AgentName: w.cfg.AgentName,
		Reason:    reason,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.WarnContext(ctx, "keeper: marshal no_gauge payload", "err", err)
		return
	}
	if emitErr := w.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperNoGauge, raw); emitErr != nil {
		slog.WarnContext(ctx, "keeper: emit session_keeper_no_gauge", "err", emitErr)
	}
}

// emitWarn emits the session_keeper_warn event.
func (w *Watcher) emitWarn(ctx context.Context, cf *CtxFile) {
	payload := core.SessionKeeperWarnPayload{
		AgentName: w.cfg.AgentName,
		Pct:       cf.Pct,
		WarnPct:   w.cfg.WarnPct,
		SessionID: cf.SessionID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.WarnContext(ctx, "keeper: marshal warn payload", "err", err)
		return
	}
	if emitErr := w.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperWarn, raw); emitErr != nil {
		slog.WarnContext(ctx, "keeper: emit session_keeper_warn", "err", emitErr)
	}
	slog.WarnContext(ctx, "keeper: context window warn threshold crossed",
		"agent", w.cfg.AgentName, "pct", cf.Pct, "warn_pct", w.cfg.WarnPct)
	fmt.Printf("keeper: warn — agent %q context window at %.1f%% (threshold %.1f%%)\n",
		w.cfg.AgentName, cf.Pct, w.cfg.WarnPct)
}

// maybeRespawn fires the respawn command if all gates pass:
//   - RespawnCmd is non-empty
//   - TmuxTarget is non-empty
//   - staleSince has been set for at least RespawnGrace
//   - cooldown since last attempt has elapsed
//   - the tmux pane is idle (agent has exited)
//   - the agent is NOT blocked on an open decision (hitl-decisions K6 exemption)
//
// On success it updates *lastRespawnAt and emits session_keeper_respawn_attempted.
// Refs: hk-3w2; hk-50f (K6 exemption).
func (w *Watcher) maybeRespawn(ctx context.Context, staleSince time.Time, lastRespawnAt *time.Time) {
	// WarnOnly mode: never respawn — warn events only. Refs: hk-yfcc.
	if w.cfg.WarnOnly {
		return
	}
	if w.cfg.RespawnCmd == "" || w.cfg.TmuxTarget == "" {
		return
	}
	// Operator HOLD (D5/hk-9waz): suspend respawn while a fresh session-scoped hold
	// is active. Auto-reverts (session-id key + timer backstop).
	if w.cfg.HeldCheckFn(w.cfg.ProjectDir, w.cfg.AgentName) {
		return
	}
	if staleSince.IsZero() || w.cfg.Clock.Since(staleSince) < w.cfg.RespawnGrace {
		return
	}
	if !lastRespawnAt.IsZero() && w.cfg.Clock.Since(*lastRespawnAt) < w.cfg.RespawnCooldown {
		return
	}
	if !w.cfg.IsPaneIdleFn(ctx, w.cfg.TmuxTarget) {
		return
	}

	// ── hitl-decisions K6 exemption (SPEC §4/§5) ─────────────────────────────
	// The 120s-silent-hang reaper (this respawn path) is about to kill/respawn
	// the watched agent as "hung". But an agent that is the blocked_agent of an
	// OPEN decision — and is legitimately WAITING (a fresh §4 heartbeat keeps it
	// Online) — is BLOCKED, not hung. Skip the reap: it is the complement of K5
	// (K6 protects the LIVE blocked agent; K5 reaps the DECISION once the agent
	// is genuinely gone). The fresh-heartbeat qualifier (presence Online) is what
	// prevents over-shielding a truly-dead agent — a Stale/Offline blocked agent
	// is NOT exempted here (its decision is K5's to reap). Read-only: consults the
	// projection, emits nothing.
	if w.blockedOnOpenDecision(ctx) {
		slog.InfoContext(ctx, "keeper: agent blocked on an open decision — exempt from 120s reaper",
			"agent", w.cfg.AgentName)
		return
	}

	slog.InfoContext(ctx, "keeper: respawning agent via --respawn-cmd",
		"agent", w.cfg.AgentName, "cmd", w.cfg.RespawnCmd)
	fmt.Printf("keeper: respawn — agent %q exited; re-launching via respawn-cmd\n", w.cfg.AgentName)

	//nolint:gosec // G204: RespawnCmd is operator-supplied via --respawn-cmd flag, not user input.
	cmd := exec.CommandContext(ctx, "sh", "-c", w.cfg.RespawnCmd)
	runErr := cmd.Run()

	*lastRespawnAt = w.cfg.Clock.Now()
	outcome := "ok"
	errMsg := ""
	if runErr != nil {
		outcome = "error"
		errMsg = runErr.Error()
		slog.WarnContext(ctx, "keeper: respawn command failed",
			"agent", w.cfg.AgentName, "err", runErr)
	}
	w.emitRespawnAttempted(ctx, outcome, errMsg)
}

// maybeLivePaneRecover is the gauge-INDEPENDENT last-resort recovery (hk-75mr).
// It fires a GATED ForceRestart (LiveRecoverFn) for an agent that is hung
// MID-TURN: the gauge has gone stale but the tmux pane is still ALIVE, so the
// idle-respawn path (maybeRespawn) never engages and a /clear inject cannot
// reach a hung turn. It is called from the same stale/absent branches as
// maybeRespawn; the two are mutually exclusive via the pane alive-vs-idle check.
//
// EVERY gate is fail-closed — recovery fires ONLY when ALL hold:
//   - LiveRecoverFn is wired AND TmuxTarget is non-empty (else no-op);
//   - staleSince ≥ LiveRecoverGrace (>> RespawnGrace — anti-premature-reap);
//   - LiveRecoverCooldown since the last attempt has elapsed;
//   - the pane is ALIVE (IsPaneAliveFn) — a hung agent, not an exited one;
//   - NO human operator is actively attached (OperatorAttachedFn, hk-0t5s) —
//     never force-restart a pane a human is driving;
//   - the agent is NOT blocked on an open decision (hitl-decisions K6) — a
//     blocked agent is waiting, not hung;
//   - the bound .sid identity is a valid UUIDv4 (ReadSidFn + isPrimarySID) — an
//     absent or malformed channel fails CLOSED (we will not force-restart an
//     agent whose identity we cannot trust).
//
// Interaction with the heartbeat (hk-81wk): the keeper-side heartbeat is the
// FIRST line of defense — it re-writes .ctx on a live pane so a transient
// repaint gap never trips the stale branch. Live-pane recovery therefore fires
// ONLY when the gauge has gone stale DESPITE the heartbeat (heartbeat disabled,
// or its WriteCtxFile failing) while the pane is still alive — a true last
// resort. LiveRecoverGrace (5m) is set far past the heartbeat threshold (~60s)
// so the heartbeat gets many chances to recover the gauge before recovery acts.
//
// On a successful gate pass it sets *lastRecoverAt and emits
// session_keeper_live_pane_recover. Refs: hk-75mr; hk-8prq (identity); hk-0t5s
// (operator discriminator); hk-50f (K6 exemption); hk-81wk (heartbeat).
func (w *Watcher) maybeLivePaneRecover(ctx context.Context, staleSince time.Time, lastRecoverAt *time.Time) {
	// WarnOnly mode: never force-restart — warn events only. Refs: hk-yfcc.
	if w.cfg.WarnOnly {
		return
	}
	if w.cfg.LiveRecoverFn == nil || w.cfg.TmuxTarget == "" {
		return
	}
	// Operator HOLD (D5/hk-9waz): suspend live-pane recovery while a fresh
	// session-scoped hold is active. Auto-reverts (session-id key + timer backstop).
	if w.cfg.HeldCheckFn(w.cfg.ProjectDir, w.cfg.AgentName) {
		return
	}
	if staleSince.IsZero() || w.cfg.Clock.Since(staleSince) < w.cfg.LiveRecoverGrace {
		return
	}
	if !lastRecoverAt.IsZero() && w.cfg.Clock.Since(*lastRecoverAt) < w.cfg.LiveRecoverCooldown {
		return
	}
	// Pane must be ALIVE (non-shell). An idle pane is the maybeRespawn path's job.
	// If the configured TmuxTarget is mangled (stale session name, wrong format —
	// the B3 watch-restall class), re-resolve from projectDir+agentName before
	// giving up. Refs: hk-9cqtm.
	effectiveTarget := w.cfg.TmuxTarget
	if !w.cfg.IsPaneAliveFn(ctx, effectiveTarget) {
		resolved := w.cfg.ResolveTmuxTargetFn(w.cfg.ProjectDir, w.cfg.AgentName)
		if resolved == "" || resolved == effectiveTarget || !w.cfg.IsPaneAliveFn(ctx, resolved) {
			return
		}
		slog.InfoContext(ctx, "keeper: live-pane recovery: re-resolved mangled target",
			"agent", w.cfg.AgentName, "old_target", w.cfg.TmuxTarget, "new_target", resolved)
		effectiveTarget = resolved
	}
	// Never force-restart a pane a human operator is actively driving (hk-0t5s).
	if w.cfg.OperatorAttachedFn(effectiveTarget) {
		slog.InfoContext(ctx, "keeper: live-pane recovery suppressed — operator actively attached",
			"agent", w.cfg.AgentName)
		return
	}
	// A blocked-on-decision agent is waiting, not hung (hitl-decisions K6).
	if w.blockedOnOpenDecision(ctx) {
		slog.InfoContext(ctx, "keeper: live-pane recovery suppressed — agent blocked on an open decision",
			"agent", w.cfg.AgentName)
		return
	}
	// Bound identity MUST be a valid UUIDv4 from the single-writer .sid channel.
	// Fail CLOSED on an absent/malformed channel — we will not force-restart an
	// agent whose identity we cannot trust (hk-8prq).
	boundSID, _, sidErr := w.cfg.ReadSidFn(w.cfg.ProjectDir, w.cfg.AgentName)
	if sidErr != nil || !isPrimarySID(boundSID) {
		slog.WarnContext(ctx, "keeper: live-pane recovery suppressed — bound .sid identity absent or not a valid UUIDv4 (fail-closed)",
			"agent", w.cfg.AgentName, "sid_err", sidErr, "bound_sid", boundSID)
		return
	}

	staleSeconds := int64(w.cfg.Clock.Since(staleSince).Seconds())
	slog.WarnContext(ctx, "keeper: live-pane recovery — gauge stale over a live pane; firing gated ForceRestart last-resort",
		"agent", w.cfg.AgentName, "stale_seconds", staleSeconds, "bound_sid", boundSID)
	fmt.Printf("keeper: live-pane recovery — agent %q hung mid-turn (gauge stale %ds, pane alive); force-restarting\n",
		w.cfg.AgentName, staleSeconds)

	runErr := w.cfg.LiveRecoverFn(ctx, w.cfg.AgentName)

	*lastRecoverAt = w.cfg.Clock.Now()
	outcome := "ok"
	errMsg := ""
	if runErr != nil {
		outcome = "error"
		errMsg = runErr.Error()
		slog.WarnContext(ctx, "keeper: live-pane recovery action failed",
			"agent", w.cfg.AgentName, "err", runErr)
	}
	w.emitLivePaneRecover(ctx, boundSID, staleSeconds, outcome, errMsg)
}

// emitLivePaneRecover emits the session_keeper_live_pane_recover event.
func (w *Watcher) emitLivePaneRecover(ctx context.Context, sessionID string, staleSeconds int64, outcome, errMsg string) {
	payload := core.SessionKeeperLivePaneRecoverPayload{
		AgentName:    w.cfg.AgentName,
		SessionID:    sessionID,
		StaleSeconds: staleSeconds,
		Outcome:      outcome,
		Error:        errMsg,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.WarnContext(ctx, "keeper: marshal live_pane_recover payload", "err", err)
		return
	}
	if emitErr := w.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperLivePaneRecover, raw); emitErr != nil {
		slog.WarnContext(ctx, "keeper: emit session_keeper_live_pane_recover", "err", emitErr)
	}
}

// blockedOnOpenDecision reports whether the watched agent (w.cfg.AgentName) is
// the blocked_agent of an OPEN hitl-decisions decision AND is legitimately
// waiting per SPEC §4 (a fresh heartbeat — presence Online). This is the K6
// exemption predicate: when true, the 120s-silent-hang reaper (maybeRespawn)
// treats the agent as BLOCKED, not HUNG, and skips the kill/respawn.
//
// It is READ-ONLY — it consults the K3 open-decision projection
// (presence.OpenDecisions) and the presence registry (presence.ComputeRegistry)
// over the durable events.jsonl and emits NOTHING. K6 protects the live agent;
// K5 (the reaper) is the SOLE emitter of decision_withdrawn(orphaned) once the
// agent is genuinely gone.
//
// The fresh-heartbeat qualifier is the exact complement of K5's "truly gone"
// predicate (presence StateOffline): K6 exempts ONLY when the blocked agent is
// presence-Online (a fresh §4 subscribe-stream heartbeat). A Stale or Offline
// blocked agent is NOT exempted — it is not indefinitely shielded; its open
// decision is K5's to reap. This is the no-over-exemption guarantee:
//   - an agent absent from every open decision → not exempt (reaped normally);
//   - an agent named in an open decision but presence-Stale/Offline → not exempt
//     (K5 reaps the decision; the agent is not shielded);
//   - an agent named in an open decision with a fresh (Online) heartbeat → EXEMPT.
//
// Returns false (fail-open — i.e. NOT exempt, the agent is reaped normally) when
// EventsJSONLPath is unset, so a misconfigured keeper never silently shields a
// hung agent.
//
// Refs: hk-50f (component K6); SPEC §4 (keeper-alive via heartbeat), §5 (keeper
// seam K6).
func (w *Watcher) blockedOnOpenDecision(_ context.Context) bool {
	if w.cfg.EventsJSONLPath == "" {
		return false
	}
	open := presence.OpenDecisions(w.cfg.EventsJSONLPath)
	if len(open) == 0 {
		return false
	}
	// Is this agent the blocked_agent of any open decision?
	blocked := false
	for _, dec := range open {
		if dec.BlockedAgent == w.cfg.AgentName {
			blocked = true
			break
		}
	}
	if !blocked {
		return false
	}
	// Fresh-heartbeat qualifier (SPEC §4): exempt ONLY when the agent's presence
	// is Online. A merely-Stale or Offline blocked agent is NOT exempted — it is
	// K5's job to reap the decision, not K6's to shield a dead agent. An agent
	// with no presence record at all is likewise not exempt (no evidence it is
	// alive and waiting).
	rec, known := presence.ComputeRegistry(w.cfg.EventsJSONLPath)[w.cfg.AgentName]
	if !known {
		return false
	}
	return presence.GetState(rec) == presence.StateOnline
}

// emitBlind emits the session_keeper_blind event (hk-34ac, Backstop 1).
func (w *Watcher) emitBlind(ctx context.Context, managedSID, liveSID string, blindSeconds int64) {
	payload := core.SessionKeeperBlindPayload{
		AgentName:    w.cfg.AgentName,
		ManagedSID:   managedSID,
		LiveSID:      liveSID,
		BlindSeconds: blindSeconds,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.WarnContext(ctx, "keeper: marshal blind payload", "err", err)
		return
	}
	if emitErr := w.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperBlind, raw); emitErr != nil {
		slog.WarnContext(ctx, "keeper: emit session_keeper_blind", "err", emitErr)
	}
}

// emitHardCeiling emits the session_keeper_hard_ceiling event (hk-34ac, Backstop 2).
func (w *Watcher) emitHardCeiling(ctx context.Context, tokens int64) {
	payload := core.SessionKeeperHardCeilingPayload{
		AgentName:   w.cfg.AgentName,
		ContextLen:  tokens,
		HardCeiling: w.cfg.HardCeilingTokens,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.WarnContext(ctx, "keeper: marshal hard_ceiling payload", "err", err)
		return
	}
	if emitErr := w.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperHardCeiling, raw); emitErr != nil {
		slog.WarnContext(ctx, "keeper: emit session_keeper_hard_ceiling", "err", emitErr)
	}
}

// emitRespawnAttempted emits the session_keeper_respawn_attempted event.
func (w *Watcher) emitRespawnAttempted(ctx context.Context, outcome, errMsg string) {
	payload := core.SessionKeeperRespawnAttemptedPayload{
		AgentName: w.cfg.AgentName,
		Outcome:   outcome,
		Error:     errMsg,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.WarnContext(ctx, "keeper: marshal respawn_attempted payload", "err", err)
		return
	}
	if emitErr := w.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperRespawnAttempted, raw); emitErr != nil {
		slog.WarnContext(ctx, "keeper: emit session_keeper_respawn_attempted", "err", emitErr)
	}
}
