package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
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
			ctxFile, modTime, err := ReadCtxFile(w.cfg.ProjectDir, w.cfg.AgentName)

			// ── gauge absent ────────────────────────────────────────────────
			if errors.Is(err, os.ErrNotExist) {
				w.maybeReemitNoGauge(ctx, "absent", lastNoGaugeEmit, &lastNoGaugeEmit)
				noGaugeEmittedAtBoot = true
				warnArmed = true
				warnFired = false
				pendingInject = false
				continue
			}
			if err != nil {
				// parse / stat error: treat as absent, log and continue
				slog.WarnContext(ctx, "keeper: read ctx file", "err", err)
				w.maybeReemitNoGauge(ctx, "absent", lastNoGaugeEmit, &lastNoGaugeEmit)
				noGaugeEmittedAtBoot = true
				pendingInject = false
				continue
			}

			// ── gauge stale ──────────────────────────────────────────────────
			if time.Since(modTime) >= w.cfg.Staleness {
				w.maybeReemitNoGauge(ctx, "stale", lastNoGaugeEmit, &lastNoGaugeEmit)
				noGaugeEmittedAtBoot = true
				warnArmed = true
				warnFired = false
				pendingInject = false
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
			} else if managedSID != "" && ctxFile.SessionID != "" && ctxFile.SessionID != managedSID {
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
				if latchErr := w.cfg.WriteManagedSessionFn(w.cfg.ProjectDir, w.cfg.AgentName, ctxFile.SessionID); latchErr != nil {
					slog.WarnContext(ctx, "keeper: latch managed session_id", "agent", w.cfg.AgentName, "err", latchErr)
					// Non-fatal: continue monitoring without persisting the binding.
				}
			}

			// Gauge is fresh (and belongs to the managed session): reset no_gauge
			// tracking so it will re-emit if the gauge goes absent or stale again.
			noGaugeEmittedAtBoot = false
			lastNoGaugeEmit = time.Time{}

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
