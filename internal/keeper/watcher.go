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
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/presence"
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
	mu    sync.Mutex
}

// NewFileEmitter constructs a FileEmitter that appends to the harmonik events
// log at projectDir/.harmonik/events/events.jsonl.
func NewFileEmitter(projectDir string) *FileEmitter {
	return &FileEmitter{
		path:  filepath.Join(projectDir, ".harmonik", core.EventsJSONLPath),
		idGen: core.NewEventIDGenerator(),
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
		TimestampWall:   time.Now().UTC(),
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

// WatcherConfig is the configuration for a Watcher instance.
type WatcherConfig struct {
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
	// is min(WarnAbsTokens, WarnPctCeil * WindowSize). Used when the gauge file
	// contains Tokens (i.e. keeper-statusline.sh is current). When WindowSize is
	// zero, FallbackWindowSize is used to cap the threshold. Falls back to WarnPct
	// only when Tokens is also zero. Default: 240000.
	// Refs: hk-cl74g, hk-kgn.
	WarnAbsTokens int64

	// FallbackWindowSize is the assumed context-window size used for the
	// WarnPctCeil cap when the gauge file reports WindowSize==0 (e.g. [1m]-class
	// models whose window size cannot be inferred). Default: 200000. Set via
	// --window-size.
	// Refs: hk-kgn.
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

	// WriteManagedSessionFn, when non-nil, is called when the watcher latches the
	// first observed session_id into .managed. When nil, WriteManagedSessionID is
	// used. Set to a no-op in unit tests that do not need latch side-effects.
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
	// hk-061) on the watch tick. When true, every ticker fire runs
	// presence.ReapOrphanedDecisions over EventsJSONLPath, emitting
	// decision_withdrawn(orphaned, by=keeper) for any open decision whose
	// blocked_agent is Offline (an explicit leave beat OR age ≥ presence.StaleCutoff,
	// never merely Stale — N9). The keeper tick is the SOLE emitter of orphaned
	// withdrawals (N9); the reap runs UNCONDITIONALLY on each tick (independent of
	// the gauge-fresh state machine below) so orphan latency is bounded by the tick
	// cadence regardless of this agent's own gauge state.
	//
	// Default: false (the reaper is opt-in; the standalone `harmonik keeper`
	// process enables it — keeper_cmd.go). When false the watcher behaves exactly
	// as before (no decision reaping).
	// Refs: hk-061 (hitl-decisions K5); SPEC §5 / N9.
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
}

// applyDefaults fills in zero-valued duration / pct fields.
func (c *WatcherConfig) applyDefaults() {
	if c.PollInterval <= 0 {
		c.PollInterval = 5 * time.Second
	}
	if c.WarnPct <= 0 {
		c.WarnPct = 80.0
	}
	if c.WarnAbsTokens <= 0 {
		c.WarnAbsTokens = 240_000
	}
	if c.FallbackWindowSize <= 0 {
		c.FallbackWindowSize = 200_000
	}
	if c.WarnPctCeil <= 0 {
		c.WarnPctCeil = 0.70
	}
	if c.IdleQuiesce <= 0 {
		c.IdleQuiesce = 8 * time.Second
	}
	if c.Staleness <= 0 {
		c.Staleness = 120 * time.Second
	}
	if c.ReadManagedSessionFn == nil {
		c.ReadManagedSessionFn = ReadManagedSessionID
	}
	if c.WriteManagedSessionFn == nil {
		c.WriteManagedSessionFn = WriteManagedSessionID
	}
	if c.RespawnGrace <= 0 {
		c.RespawnGrace = 20 * time.Second
	}
	if c.RespawnCooldown <= 0 {
		c.RespawnCooldown = 90 * time.Second
	}
	if c.IsPaneIdleFn == nil {
		c.IsPaneIdleFn = IsPaneIdle
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
// threshold. Uses absolute tokens when Tokens>0, even if WindowSize==0 (using
// FallbackWindowSize for the pct-ceil cap). Falls back to Pct vs WarnPct only
// when Tokens is also zero.
func (c *WatcherConfig) belowWarnThreshold(cf *CtxFile) bool {
	if cf.Tokens > 0 {
		windowSize := cf.WindowSize
		if windowSize == 0 {
			windowSize = c.FallbackWindowSize
		}
		threshold := c.WarnAbsTokens
		if c.WarnPctCeil > 0 && windowSize > 0 {
			pctBased := int64(c.WarnPctCeil * float64(windowSize))
			if pctBased < threshold {
				threshold = pctBased
			}
		}
		return cf.Tokens < threshold
	}
	return cf.Pct < c.WarnPct
}

// Watcher polls the gauge file and manages the warn-injection state machine.
// It is safe to construct a Watcher and call Run once.
//
// Spec ref: codename:session-keeper §4.2 Phase-1 warn-mode (hk-8vzek).
type Watcher struct {
	cfg     WatcherConfig
	emitter Emitter
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

		// lastNoGaugeEmit is when we last emitted session_keeper_no_gauge.
		// Tracks staleness re-emit interval.
		lastNoGaugeEmit time.Time

		// noGaugeEmittedAtBoot tracks the boot-time no_gauge emission.
		noGaugeEmittedAtBoot = false

		// gaugeStaleSince is the time when the gauge first became stale/absent
		// in the current stale streak. Zero when the gauge is fresh. Used by the
		// respawn path to enforce RespawnGrace. (Refs: hk-3w2)
		gaugeStaleSince time.Time

		// lastRespawnAt is the time of the most recent respawn attempt. Used to
		// enforce RespawnCooldown. Zero when no respawn has occurred. (Refs: hk-3w2)
		lastRespawnAt time.Time
	)

	// Boot-time check: emit no_gauge immediately if gauge is absent or stale.
	if absent, reason := w.gaugeUnavailable(ctx); absent {
		if !noGaugeEmittedAtBoot {
			w.emitNoGauge(ctx, reason)
			lastNoGaugeEmit = time.Now()
			noGaugeEmittedAtBoot = true
		}
	}

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// ── hitl-decisions orphan reaper (K5, hk-061) ────────────────────
			// Runs UNCONDITIONALLY on every tick, BEFORE the gauge-read branches
			// below (which may `continue` past the rest of the loop body when the
			// gauge is absent/stale/foreign). This keeps orphan-withdraw latency
			// bounded by the tick cadence (≤ Offline-cutoff + one tick, SPEC §5 /
			// N9) regardless of THIS agent's own gauge state — the reaper acts on
			// the global open-decision set, not on this watcher's managed pane.
			// The keeper tick is the SOLE emitter of decision_withdrawn(orphaned).
			w.maybeReapOrphanedDecisions(ctx)

			ctxFile, modTime, err := ReadCtxFile(w.cfg.ProjectDir, w.cfg.AgentName)

			// ── gauge absent ────────────────────────────────────────────────
			if errors.Is(err, os.ErrNotExist) {
				w.maybeReemitNoGauge(ctx, "absent", lastNoGaugeEmit, &lastNoGaugeEmit)
				noGaugeEmittedAtBoot = true
				warnArmed = true
				warnFired = false
				pendingInject = false
				if gaugeStaleSince.IsZero() {
					gaugeStaleSince = time.Now()
				}
				w.maybeRespawn(ctx, gaugeStaleSince, &lastRespawnAt)
				continue
			}
			if err != nil {
				// parse / stat error: treat as absent, log and continue
				slog.WarnContext(ctx, "keeper: read ctx file", "err", err)
				w.maybeReemitNoGauge(ctx, "absent", lastNoGaugeEmit, &lastNoGaugeEmit)
				noGaugeEmittedAtBoot = true
				pendingInject = false
				if gaugeStaleSince.IsZero() {
					gaugeStaleSince = time.Now()
				}
				w.maybeRespawn(ctx, gaugeStaleSince, &lastRespawnAt)
				continue
			}

			// ── gauge stale ──────────────────────────────────────────────────
			if time.Since(modTime) >= w.cfg.Staleness {
				w.maybeReemitNoGauge(ctx, "stale", lastNoGaugeEmit, &lastNoGaugeEmit)
				noGaugeEmittedAtBoot = true
				warnArmed = true
				warnFired = false
				pendingInject = false
				if gaugeStaleSince.IsZero() {
					gaugeStaleSince = time.Now()
				}
				w.maybeRespawn(ctx, gaugeStaleSince, &lastRespawnAt)
				continue
			}

			// ── session_id binding ────────────────────────────────────────────
			// Validate that the gauge belongs to the session this keeper is bound to.
			// If .managed contains an expected session_id and the gauge carries a
			// DIFFERENT non-empty session_id, this is a foreign-session write — two
			// concurrent same-agent Claude Code processes both with HARMONIK_AGENT=<X>
			// would otherwise clobber each other's session_id in this state machine.
			// Treat a foreign gauge as absent so the warn/cycle logic stays consistent.
			// On first valid gauge (no binding yet), latch the session_id into .managed
			// so subsequent foreign-session writes are filtered. (Refs: hk-igt)
			if managedSID, managedErr := w.cfg.ReadManagedSessionFn(w.cfg.ProjectDir, w.cfg.AgentName); managedErr != nil {
				slog.WarnContext(ctx, "keeper: read managed session_id", "err", managedErr)
				// Fall through on read error to avoid silent monitoring gaps.
			} else {
				// Self-heal: .managed holds a stale UUIDv7 from pre-hk-lap state
				// (e.g. keeper restarted after a clear->resume cycle that saved the
				// daemon implementer's UUIDv7 before hk-lap landed). Clear it and
				// re-bind to the live UUIDv4 so no manual rm of .managed is needed.
				// Refs: hk-6mp (this fix), hk-lap (latch-time guard).
				if managedSID != "" && isUUIDv7(managedSID) {
					slog.WarnContext(ctx, "keeper: stale UUIDv7 in .managed — clearing for re-bind to live UUIDv4",
						"agent", w.cfg.AgentName, "stale_sid", managedSID)
					if clearErr := w.cfg.WriteManagedSessionFn(w.cfg.ProjectDir, w.cfg.AgentName, ""); clearErr != nil {
						slog.WarnContext(ctx, "keeper: clear stale UUIDv7 from .managed", "agent", w.cfg.AgentName, "err", clearErr)
					}
					managedSID = "" // treat as unbound; fall through to latch path below
				}

				// Defense-in-depth: a daemon dispatch can transiently overwrite
				// captain.ctx with its UUIDv7 session_id. When .managed holds a
				// UUIDv4 (the real captain) and the gauge now carries a UUIDv7,
				// skip-and-retain — keep the last good gauge in place rather than
				// emitting no_gauge:foreign_session for one tick. The daemon ROOT
				// fix (hk-lap) is the durable cure; this is the one-tick backstop.
				// Refs: hk-y1h, epic hk-3js5m.
				if managedSID != "" && !isUUIDv7(managedSID) && isUUIDv7(ctxFile.SessionID) {
					slog.DebugContext(ctx, "keeper: transient UUIDv7 in .ctx while .managed is UUIDv4 — skipping tick, retaining last gauge",
						"agent", w.cfg.AgentName, "managed_sid", managedSID, "ctx_sid", ctxFile.SessionID)
					continue
				}

				if managedSID != "" && ctxFile.SessionID != "" && ctxFile.SessionID != managedSID {
					// Foreign session — treat as absent.
					slog.DebugContext(ctx, "keeper: gauge session_id mismatch; ignoring foreign session",
						"agent", w.cfg.AgentName, "expected_sid", managedSID, "got_sid", ctxFile.SessionID)
					w.maybeReemitNoGauge(ctx, "foreign_session", lastNoGaugeEmit, &lastNoGaugeEmit)
					noGaugeEmittedAtBoot = true
					warnArmed = true
					warnFired = false
					pendingInject = false
					continue
				} else if managedSID == "" && ctxFile.SessionID != "" {
					// Latch: first valid gauge seen — bind its session_id into .managed.
					// Reject UUIDv7 SIDs: daemon-spawned implementers use UUIDv7;
					// interactive captain sessions use UUIDv4. After a clear->resume
					// cycle that timed out and cleared .managed, latching a UUIDv7 would
					// bind the keeper to the wrong session, causing no_gauge:foreign_session
					// on every subsequent tick. (Refs: hk-lap)
					if isUUIDv7(ctxFile.SessionID) {
						slog.DebugContext(ctx, "keeper: skipping latch of UUIDv7 (daemon implementer) session",
							"agent", w.cfg.AgentName, "sid", ctxFile.SessionID)
						w.maybeReemitNoGauge(ctx, "foreign_session", lastNoGaugeEmit, &lastNoGaugeEmit)
						noGaugeEmittedAtBoot = true
						warnArmed = true
						warnFired = false
						pendingInject = false
						continue
					}
					if latchErr := w.cfg.WriteManagedSessionFn(w.cfg.ProjectDir, w.cfg.AgentName, ctxFile.SessionID); latchErr != nil {
						slog.WarnContext(ctx, "keeper: latch managed session_id", "agent", w.cfg.AgentName, "err", latchErr)
						// Non-fatal: continue monitoring without persisting the binding.
					}
				}
			}

			// Gauge is fresh (and belongs to the managed session): reset no_gauge
			// and respawn tracking so they re-arm if the gauge goes stale again.
			noGaugeEmittedAtBoot = false
			lastNoGaugeEmit = time.Time{}
			gaugeStaleSince = time.Time{}

			// ── idle-gate ────────────────────────────────────────────────────
			// The pane is considered idle when the gauge file's mod-time has not
			// changed since the previous tick for at least IdleQuiesce.
			gaugeQuiesced := !modTime.IsZero() && !lastModTime.IsZero() &&
				modTime.Equal(lastModTime) &&
				time.Since(modTime) >= w.cfg.IdleQuiesce
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
			if w.cfg.Cycler != nil {
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
			if w.cfg.Cycler != nil && HasPrecompactTrigger(w.cfg.ProjectDir, w.cfg.AgentName) {
				if pcErr := w.cfg.Cycler.RunForPrecompact(ctx, ctxFile); pcErr != nil {
					slog.WarnContext(ctx, "keeper: precompact cycle error", "agent", w.cfg.AgentName, "err", pcErr)
				}
			}

			// ── warn state machine ───────────────────────────────────────────
			if w.cfg.belowWarnThreshold(ctxFile) {
				// Below threshold: reset so the next upward crossing will warn.
				warnArmed = true
				warnFired = false
				pendingInject = false
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
				warnFired = true
				warnArmed = false
				if w.cfg.TmuxTarget != "" {
					pendingInject = true
				}
			}

			// Attempt inject delivery — on the crossing tick or any subsequent tick
			// once the pane has quiesced. Retries on each tick until success so a
			// non-quiesced crossing tick never permanently suppresses injection.
			if pendingInject && gaugeQuiesced {
				inject := w.cfg.InjectFn
				if inject == nil {
					inject = InjectWrapUpWarning
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
// hk-061) when the reaper is enabled (cfg.ReapDecisions). It emits
// decision_withdrawn(orphaned, by=keeper) for every open decision whose
// blocked_agent is Offline (an explicit leave beat OR age ≥ presence.StaleCutoff,
// never merely Stale — N9), via the canonical presence.ReapOrphanedDecisions.
//
// It is a no-op when ReapDecisions is false. The emitter is cfg.DecisionEmitter
// when set, else the watcher's primary emitter (the standalone keeper's
// FileEmitter, which appends to the same events.jsonl). A reap error or a
// per-decision emit failure is logged and swallowed — the next tick retries
// (the pass is idempotent: the open set is re-read fresh each call, N3).
func (w *Watcher) maybeReapOrphanedDecisions(ctx context.Context) {
	if !w.cfg.ReapDecisions {
		return
	}
	emitter := w.cfg.DecisionEmitter
	if emitter == nil {
		emitter = w.emitter
	}
	res, err := presence.ReapOrphanedDecisions(ctx, w.cfg.EventsJSONLPath, emitter)
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
	if time.Since(modTime) >= w.cfg.Staleness {
		return true, "stale"
	}
	return false, ""
}

// maybeReemitNoGauge emits session_keeper_no_gauge if the staleness interval
// has elapsed since the last emission. Updates *lastEmit on emission.
func (w *Watcher) maybeReemitNoGauge(ctx context.Context, reason string, lastEmit time.Time, lastEmitOut *time.Time) {
	if lastEmit.IsZero() || time.Since(lastEmit) >= w.cfg.Staleness {
		w.emitNoGauge(ctx, reason)
		*lastEmitOut = time.Now()
	}
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
	if w.cfg.RespawnCmd == "" || w.cfg.TmuxTarget == "" {
		return
	}
	if staleSince.IsZero() || time.Since(staleSince) < w.cfg.RespawnGrace {
		return
	}
	if !lastRespawnAt.IsZero() && time.Since(*lastRespawnAt) < w.cfg.RespawnCooldown {
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

	*lastRespawnAt = time.Now()
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
