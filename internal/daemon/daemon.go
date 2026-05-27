package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

// Config holds the startup configuration for the harmonik daemon.
//
// At MVH the struct is intentionally minimal: subsystem-specific fields are
// added by the per-registry beads (hk-8mup.62, hk-8i31.83) as each registry
// is wired into [Start].
//
// Spec ref: specs/process-lifecycle.md §4.6 PL-020 — internal/daemon is the
// composition root; Config is its public configuration surface.
type Config struct {
	// ProjectDir is the root directory of the harmonik project. It is the
	// directory that contains .beads/, .harmonik/, and the worktree parent.
	// Must be an absolute path resolved by the caller (cmd/harmonik) before
	// passing in. An empty string is only valid in unit tests that do not
	// exercise path-dependent behaviour.
	//
	// MVH_ROADMAP row #1 (hk-56ajv).
	ProjectDir string

	// WorkflowModeDefault is the daemon-level default workflow mode loaded once
	// at PL-005 step 0 per §PL-004a.  It is the second-lowest-precedence tier
	// of the four-tier resolution chain (execution-model.md §4.3 EM-012a):
	// per-bead label → per-project → daemon-default (this field) → built-in
	// fallback (single).
	//
	// The zero value (empty string) is treated as [core.WorkflowModeSingle] —
	// operators who do not set the field get the built-in default.  Any other
	// unrecognised value is rejected at startup with an error so the daemon
	// fails fast rather than silently degrading.
	//
	// The field is immutable for the daemon's lifetime; mid-run changes require
	// a daemon restart (or exec-replacement via harmonik upgrade per PL-027).
	//
	// Spec ref: specs/process-lifecycle.md §4.1 PL-004a; §4.2 PL-005 step 0.
	// Bead ref: hk-7om2q.8.
	WorkflowModeDefault core.WorkflowMode

	// LogWriter is the destination for structured daemon log output.
	// A nil LogWriter silences all log output (useful in tests).
	LogWriter io.Writer

	// JSONLLogPath is the absolute path for the durable JSONL event log.
	//
	// When non-empty, daemon.Start opens this file with O_CREATE|O_WRONLY|O_APPEND
	// and threads the resulting [eventbus.JSONLWriter] into the event bus so that
	// every Emit call appends a JSONL line. F-class (fsync-boundary) events are
	// fsynced before Emit returns per EV-016 / EV-016a.
	//
	// The canonical MVH path is <ProjectDir>/.harmonik/events/events.jsonl
	// (event-model.md §6.2). When empty, JSONL logging is disabled (useful for
	// unit tests that use an in-memory bus only).
	//
	// Spec ref: specs/event-model.md §4.4 EV-015, EV-016; §6.2 EV-020.
	// Bead ref: hk-8mup.63.
	JSONLLogPath string

	// BrPath is the absolute path to the `br` CLI binary.
	//
	// Must be non-empty when the work loop is active (i.e., ProjectDir is set).
	// Production callers resolve it via exec.LookPath("br") at startup.
	// When empty the work loop is skipped (unit-test mode without a bead ledger).
	//
	// Bead ref: hk-ecrxy.
	BrPath string

	// HandlerBinary is the executable to spawn for each bead dispatch.
	//
	// When empty the work loop defaults to "claude". The exploratory-testing wave
	// (EXPLORATORY_TESTING_PLAN.md §6) overrides this field with a twin binary
	// path so that test runs do not consume API credits.
	//
	// Bead ref: hk-ecrxy.
	HandlerBinary string

	// HandlerArgs are extra arguments appended to the handler binary invocation
	// for every bead dispatch.
	//
	// The work loop sets LaunchSpec.Args = HandlerArgs on each iteration. When
	// nil no extra arguments are passed. Tests may supply ["-c", "exit 0"] to
	// exercise the handler path without a real claude binary (hk-4e5b5).
	//
	// Bead ref: hk-4e5b5.
	HandlerArgs []string

	// HandlerEnv is the environment for handler subprocesses in "KEY=VALUE" form.
	//
	// When nil the child inherits no environment. Production callers MUST inject
	// at minimum HARMONIK_PROJECT_HASH (lifecycle.ProvenanceEnvVar). Tests may
	// supply a minimal environment or nil.
	//
	// Bead ref: hk-ecrxy.
	HandlerEnv []string

	// MaxConcurrent is the maximum number of beads the work loop may dispatch
	// concurrently. A value of zero is treated as 1, preserving MVH
	// single-threaded semantics for any caller that does not set the field
	// (zero-value compatibility).
	//
	// Ceiling enforcement lives in the work-loop scheduler (row 5, hk-e61c3.2),
	// NOT in the bus or adapter. Row 5 reads this field and gates goroutine
	// creation accordingly.
	//
	// The composition root (cmd/harmonik/main.go) exposes this as --max-concurrent.
	// Default: 1. Valid range: ≥1. Values >1 are inert until hk-e61c3.2 lands.
	//
	// Bead ref: hk-e61c3.1. POST_MVH_PARALLELISM_ROADMAP row 6.
	MaxConcurrent int

	// AgentReadyTimeout is the maximum duration the daemon waits for an
	// agent_ready event after launching a handler subprocess per HC-056.
	//
	// A zero value falls back to the defaultAgentReadyTimeout constant (30s)
	// declared in agentready.go. Operators may reduce this value in environments
	// with fast cold-start paths or increase it for slow disk-cache warm-up.
	// The timeout is applied per-dispatch (not per-daemon lifetime).
	//
	// On expiry: the session context is cancelled, the subprocess is reaped,
	// agent_failed{class=structural, sub_reason=agent_ready_timeout} is emitted,
	// and the bead is reopened (HC-056 steps 1–4). Wiring into the workloop
	// completion path lands in hk-gql20.14/.15.
	//
	// Spec ref: specs/handler-contract.md §4.9 HC-056.
	// Bead ref: hk-gql20.18.
	AgentReadyTimeout time.Duration

	// Substrate is the optional tmux substrate for handler.Launch.
	//
	// When non-nil it is injected into workLoopDeps.substrate so that each bead
	// dispatch spawns a new tmux window instead of forking a subprocess directly
	// via exec.CommandContext.
	//
	// The production composition root (cmd/harmonik/main.go) reads $TMUX, resolves
	// the current session name via tmux display-message, constructs a
	// daemon.NewTmuxSubstrate, and stores it here. When nil the work loop falls
	// back to exec.CommandContext (unit-test mode / non-tmux environments).
	//
	// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
	// Bead ref: hk-kqdpf.4.
	Substrate handler.Substrate

	// DaemonBinaryPath is the absolute path to the running harmonik binary,
	// resolved via os.Executable() at daemon startup (hk-kqdpf.6).
	//
	// It is materialized as the hook "command" field in every workspace's
	// .claude/settings.json so that Claude's hook-relay subprocess can be found
	// regardless of the tmux window's $PATH. If the daemon binary is run from a
	// non-installed path (e.g. /tmp/hk), a bare "harmonik" command would fail in
	// the tmux window — this field avoids that failure.
	//
	// MUST be non-empty in production; cmd/harmonik/main.go resolves it via
	// os.Executable() and fails fast if that call errors. When empty, the work
	// loop substitutes the literal string "harmonik" as a fallback for legacy
	// unit-test callers that do not set this field.
	//
	// Spec ref: specs/claude-hook-bridge.md §4.1 CHB-003 (hook command field).
	// Bead ref: hk-kqdpf.6.
	DaemonBinaryPath string

	// ProjectCfg is the decoded .harmonik/config.yaml loaded once at startup
	// (EM-012b tier-2). Populated by Start via LoadProjectConfig; callers may
	// leave it zero-value for tests that do not exercise project-config resolution.
	//
	// The zero value (ProjectConfig{}) is safe: LookupAgent returns ("","") for
	// all agent types, causing resolution to fall through to tier 3 and tier 4.
	//
	// Spec ref: specs/execution-model.md §4.3 EM-012b — tier-2 slot.
	// Bead ref: hk-bfvk7.
	ProjectCfg ProjectConfig

	// BinaryCommitHash is the git commit hash of the running daemon binary,
	// injected at build time via -ldflags "-X main.commitHash=<sha>" and
	// forwarded here from the composition root (cmd/harmonik/main.go).
	//
	// It is emitted verbatim in the daemon_started event payload
	// (binary_commit_hash field, §8.7.1). When empty Start falls back to
	// "unknown" so that the field is always well-formed per the spec.
	//
	// The zero value ("") is safe for unit tests that do not care about the
	// stamped hash; they will see "unknown" in the emitted payload.
	//
	// Spec ref: specs/event-model.md §8.7.1 (daemon_started payload).
	// Bead ref: hk-mz0x4.
	BinaryCommitHash string

	// CancelOnQueueDrain, when non-nil, is called once after the queue
	// transitions to all-success and ClearQueue completes.  The cancel causes
	// the daemon context to expire so harmonik exits cleanly instead of
	// idle-spinning waiting for more work.
	//
	// Set by the `harmonik run <bead-id>` subcommand (hk-icecw) to implement
	// exit-on-empty semantics: a queue of one item terminates naturally after
	// CompleteAndUnlink + ClearQueue, and the cancel propagates through the
	// daemon context to runWorkLoop.
	//
	// The zero value (nil) is safe: the daemon continues running after the
	// queue drains, which is the normal daemon behaviour.
	//
	// Bead ref: hk-icecw.
	CancelOnQueueDrain context.CancelFunc

	// CancelOnQueueExit, when non-nil, is called once when the queue reaches
	// a terminal state — either all-success (after ClearQueue) OR
	// paused-by-failure (after Persist).  Together with CancelOnQueueDrain
	// this ensures harmonik run <bead-id> exits promptly on both outcome
	// paths instead of idle-spinning waiting for more work.
	//
	// The zero value (nil) is safe: the daemon continues running after the
	// queue exits, which is the normal daemon behaviour.
	//
	// Bead ref: hk-8jh26.
	CancelOnQueueExit context.CancelFunc

	// StopDispatchCtx, when non-nil, is the context checked by the work loop's
	// outer poll to decide whether to pull new beads. When this context is
	// cancelled the loop stops dispatching and waits for in-flight goroutines to
	// drain — but in-flight goroutines continue running on the main ctx.
	//
	// This separates "stop dispatching new work" (StopDispatchCtx) from "cancel
	// in-flight work" (ctx passed to daemon.Start). Without this split,
	// CancelOnQueueDrain/CancelOnQueueExit cancel the shared runCtx, which kills
	// in-flight reviewer goroutines (hk-2o2i9).
	//
	// When nil the work loop uses ctx (the daemon context) for both dispatch-halt
	// and in-flight lifetime, preserving backward-compatible behaviour.
	//
	// Bead ref: hk-2o2i9.
	StopDispatchCtx context.Context

	// SkipWALCheckpoint, when true, disables the advisory WAL-checkpoint
	// pre-flight that runs at PL-005 step 0 before the first brcli call.
	//
	// The pre-flight is non-fatal and transparent in production. This field
	// exists solely for unit tests that operate on fake or absent .beads
	// databases where spawning sqlite3 would be a no-op at best and
	// confusing at worst.
	//
	// Default (false): pre-flight runs when ProjectDir is set and
	// .beads/beads.db-wal exceeds 1 MB.
	//
	// Bead ref: hk-5dewt.
	SkipWALCheckpoint bool

	// SkipBrHistoryRotation, when true, disables the advisory .br_history/
	// rotation pre-flight that runs at PL-005 step 0 immediately after the
	// WAL-checkpoint pre-flight.
	//
	// The pre-flight is non-fatal and transparent in production. This field
	// exists solely for unit tests that operate on temp directories where
	// history-dir presence/absence is deliberately controlled and a rotation
	// run would interfere with fixture state.
	//
	// Default (false): pre-flight runs when ProjectDir is set, keeping the 20
	// most-recent .br_history/ entries and archiving the rest.
	//
	// Bead ref: hk-5dewt.
	SkipBrHistoryRotation bool

	// QueueStore, when non-nil, is used directly instead of creating a fresh
	// QueueStore inside daemon.Start.  The caller retains the pointer and can
	// inspect queue status after Start returns (Fix 2 of hk-8jh26).
	//
	// The zero value (nil) is safe: daemon.Start creates its own QueueStore
	// as before.
	//
	// Bead ref: hk-8jh26.
	QueueStore *QueueStore

	// HandlerPauseController, when non-nil, is wired into the work loop to
	// enable the skip-on-paused dispatch gate (hk-kac8g).  When nil the gate
	// is disabled: all items are dispatched regardless of handler pause state.
	//
	// Production callers (cmd/harmonik/main.go) construct a controller and wire
	// it here so `harmonik handler pause` can trip the gate mid-run.
	// Unit tests that do not exercise handler-pause behaviour may leave this nil.
	//
	// Bead ref: hk-kac8g.
	HandlerPauseController *HandlerPauseController

	// NotifyStream, when non-nil, receives one line per bead completion event
	// as each bead's run_completed or run_failed event lands.
	//
	// Format: "[hk-XXX] success (commit abcdef)" or "[hk-XXX] failed (reason: ...)".
	// Lines are written in real time; the stream is not closed by the daemon.
	//
	// Production callers set this to os.Stdout (--notify-stream default) or to
	// an open FIFO/file (--notify-stream=path). When nil no per-bead lines are
	// written (backward-compatible default).
	//
	// Bead ref: hk-ibilr.
	NotifyStream io.Writer
}

// daemonTestHooks carries test-only injection points that are absent from the
// production Config surface.  The zero value is safe for production use: all
// fields are nil and the hooks are no-ops.
//
// Tests use StartForTesting (internal/daemon/testopts_test.go) to supply these
// hooks via functional options; production callers go through Start which always
// passes a zero-value daemonTestHooks.
//
// Bead ref: hk-j192n.
type daemonTestHooks struct {
	// busObserver, when non-nil, is called with the event bus immediately after
	// all pre-Seal subscriptions have been registered and before bus.Seal() is
	// called.  Mirrors the former Config.TestOnlyBusObserver.
	//
	// Bead ref: hk-37zy8.
	busObserver func(bus eventbus.EventBus)

	// brAdapterFactory, when non-nil, replaces brcli.NewForProject at all three
	// call sites in startWithHooks.  Mirrors the former Config.TestOnlyBrAdapterFactory.
	//
	// Spec ref: specs/beads-integration.md §4.10 BI-031b.
	// Bead ref: hk-th378.
	brAdapterFactory func(brPath, projectDir string) (*brcli.Adapter, error)
}

// newBrAdapter constructs a *brcli.Adapter using hooks.brAdapterFactory when set
// (test mode) or brcli.NewForProject in production.
//
// Centralising the call avoids three duplicate factory-selection blocks across
// the three brcli.NewForProject call sites in startWithHooks (hk-th378).
func newBrAdapter(hooks daemonTestHooks, brPath, projectDir string) (*brcli.Adapter, error) {
	if hooks.brAdapterFactory != nil {
		return hooks.brAdapterFactory(brPath, projectDir)
	}
	return brcli.NewForProject(brPath, projectDir)
}

// Start is the composition-root entry point for the harmonik daemon.
//
// ctx controls the lifetime of the work loop. The caller is responsible for
// cancelling ctx when a clean shutdown is desired (e.g. via
// signal.NotifyContext). This makes the stop mechanism testable without sending
// OS signals to the test process (hk-7oz2f).
//
// It executes the deterministic startup sequence defined by
// specs/process-lifecycle.md §4.2 PL-005:
//
// Step 1 (PL-002, hk-iarcy): acquire the advisory pidfile lock at
// <ProjectDir>/.harmonik/daemon.pid. Returns an error immediately if another
// daemon holds the lock (lifecycle.ErrPidfileLocked → exit code 5 per PL-008a).
// Pidfile is released on return via defer.
//
// Step 0 (PL-005 / hk-8mup.63):
//   - Instantiates the RedactionRegistry (handlercontract.NewRedactionRegistry)
//     per HC-032. No seed patterns are registered at this scope; handlers
//     register their own patterns when they land.
//   - Opens the JSONL event log at cfg.JSONLLogPath (when non-empty) per
//     EV-015 / EV-016 (hk-8mup.63).
//   - Instantiates the EventBus (eventbus.NewBusImplWithWriter) with the
//     registry and writer per EV-035, EV-016.
//
// daemon_started event (§8.7.1, hk-iarcy): emitted after the bus is
// constructed. Payload: started_at, pid, binary_commit_hash.
//
// Spec ref: specs/process-lifecycle.md §4.6 PL-020, PL-020a, PL-005 step 0;
// §4.1 PL-002, PL-002a, PL-002b.
func Start(ctx context.Context, cfg Config) error {
	return startWithHooks(ctx, cfg, daemonTestHooks{})
}

// startWithHooks is the implementation of Start.  Production callers use Start
// which passes a zero-value daemonTestHooks.  Test callers use StartForTesting
// (internal/daemon/testopts_test.go) which supplies non-nil hook fields.
//
// Bead ref: hk-j192n.
func startWithHooks(ctx context.Context, cfg Config, hooks daemonTestHooks) error {
	// Step 1 (PL-002, hk-iarcy): acquire the advisory pidfile lock.
	//
	// AcquirePidfile constructs the path internally as
	// <ProjectDir>/.harmonik/daemon.pid (PL-002b). The bead body described a
	// path under .harmonik/run/; the actual lifecycle.AcquirePidfile API uses
	// .harmonik/daemon.pid — the code wins per implementer-protocol §Path-discrepancy.
	// Follow-up: patch bead body / spec cross-ref for the .harmonik/run/ path form.
	//
	// Skip pidfile acquisition when ProjectDir is empty (unit-test mode).
	var pidfile *lifecycle.Pidfile
	if cfg.ProjectDir != "" {
		// mkdir-p <ProjectDir>/.harmonik/ so AcquirePidfile can open the file.
		harmonikDir := filepath.Join(cfg.ProjectDir, ".harmonik")
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if mkErr := os.MkdirAll(harmonikDir, 0o755); mkErr != nil {
			return fmt.Errorf("daemon.Start: mkdir-p .harmonik: %w", mkErr)
		}

		pid := os.Getpid()
		pgid := syscall.Getpgrp()
		// Generate a UUIDv7 as the daemon instance ID (PL-005 step 0).
		instanceUID, uidErr := uuid.NewV7()
		if uidErr != nil {
			return fmt.Errorf("daemon.Start: generate instance ID: %w", uidErr)
		}
		instanceID := instanceUID.String()

		var acquireErr error
		pidfile, acquireErr = lifecycle.AcquirePidfile(cfg.ProjectDir, pid, pgid, instanceID)
		if acquireErr != nil {
			return fmt.Errorf("daemon.Start: pidfile: %w", acquireErr)
		}
		defer func() { _ = pidfile.Release() }()
	}

	// Step 0 (PL-005): bootstrap cross-subsystem registries.

	// PL-004a: resolve and cache workflow_mode_default once at step 0.
	//
	// The zero value (empty string) is treated as WorkflowModeSingle — the
	// built-in fallback per PL-004a ("When the field is absent, the daemon's
	// default workflow mode MUST be `single`").  Any unrecognised non-empty
	// value is rejected at startup so the daemon fails fast.
	//
	// Bead ref: hk-7om2q.8.
	workflowModeDefault := cfg.WorkflowModeDefault
	if workflowModeDefault == "" {
		workflowModeDefault = core.WorkflowModeSingle
	} else if !workflowModeDefault.Valid() {
		return fmt.Errorf("daemon.Start: invalid workflow_mode_default %q: must be one of single, review-loop, dot (PL-004a)", workflowModeDefault)
	}

	// EM-012b tier-2: load .harmonik/config.yaml once at startup and cache in cfg.
	// A parse error or unsupported schema_version causes the daemon to refuse to start
	// (loud failure per bead spec; operators must fix the config before restarting).
	// A missing file is silently treated as "no project config" (zero-value ProjectConfig).
	//
	// Bead ref: hk-bfvk7.
	if cfg.ProjectDir != "" {
		projectCfg, cfgErr := LoadProjectConfig(cfg.ProjectDir)
		if cfgErr != nil {
			return fmt.Errorf("daemon.Start: load .harmonik/config.yaml: %w", cfgErr)
		}
		cfg.ProjectCfg = projectCfg
	}

	// WAL-checkpoint pre-flight (hk-5dewt): if .beads/beads.db-wal exists and
	// exceeds 1 MB, run PRAGMA wal_checkpoint(TRUNCATE) via sqlite3 before the
	// first br write.  This prevents the 10s wall-clock timeout in
	// brcli/timeout.go from firing when WAL bloat causes `br close` to take
	// >10s (dogfood-2/3/4 diagnosis: 0.35s on clean DB vs 19.4s with 12MB WAL).
	// The call is non-fatal and is a no-op when sqlite3 is not on PATH.
	// Skipped when SkipWALCheckpoint is true (test isolation) or when ProjectDir
	// is empty (unit-test mode).
	if cfg.ProjectDir != "" && !cfg.SkipWALCheckpoint {
		_ = runWALCheckpointPreflight(ctx, cfg.ProjectDir)
	}

	// .br_history/ rotation pre-flight (hk-5dewt): each `br` write appends a
	// ~1.2 MB snapshot to .beads/.br_history/. With 200+ entries (226 MB) the
	// per-write scan cost reaches ~19.5 s, exceeding the 10 s brcli timeout
	// regardless of WAL state. Archiving all but the 20 most-recent snapshots
	// restores sub-second write latency (validated: 19.5 s → 0.15 s).
	// The call is non-fatal. Skipped when SkipBrHistoryRotation is true (test
	// isolation) or when ProjectDir is empty (unit-test mode).
	if cfg.ProjectDir != "" && !cfg.SkipBrHistoryRotation {
		_ = runBrHistoryRotationPreflight(ctx, cfg.ProjectDir, brHistoryRotationDefaultKeep)
	}

	// Instantiate the RedactionRegistry (HC-032; hk-8i31.83).
	// No seed patterns here — handlers call registry.RegisterPattern when they
	// are wired (per PL-005 step 0 semantics).
	registry := handlercontract.NewRedactionRegistry()

	// Open the JSONL event log when a path is configured (hk-8mup.63).
	// The log dir must exist before Start is called; daemon callers are
	// responsible for mkdir-p (canonically <ProjectDir>/.harmonik/events/).
	var jsonlWriter *eventbus.JSONLWriter
	if cfg.JSONLLogPath != "" {
		var openErr error
		jsonlWriter, openErr = eventbus.OpenJSONLWriter(cfg.JSONLLogPath)
		if openErr != nil {
			return fmt.Errorf("daemon.Start: open JSONL log %q: %w",
				filepath.Base(cfg.JSONLLogPath), openErr)
		}
		defer func() { _ = jsonlWriter.Close() }()
	}

	// Instantiate the EventBus with the registry and writer (EV-035; hk-8mup.62,
	// hk-8i31.83, hk-8mup.63).
	//
	// Subscribers MUST be registered before Seal (EV-009). The
	// HandlerPausePolicyGoroutine (hk-37zy8) is the first production subscriber;
	// it is wired below before bus.Seal() is called.
	bus := eventbus.NewBusImplWithWriter(registry, jsonlWriter)

	// PL-005 step 0 (hk-m0k0a, hk-37zy8, hk-7urls): construct HandlerPauseController,
	// RunRegistry, and QueueStore at the composition root so all are available
	// pre-Seal for their respective Subscribe(bus) calls.
	//
	// Controller construction is moved here (before Seal) because the policy
	// goroutine constructor requires it. LoadHandlerPauseState (persistence seed)
	// runs further below, after Seal, where cfg.ProjectDir is checked — that
	// sequencing is unchanged.
	//
	// RunRegistry is created here so the policy goroutine and the work loop share
	// the same instance. The work loop receives it via deps.runRegistry (injected
	// post-newWorkLoopDeps, same pattern as queueStore / handlerPauseController).
	//
	// QueueStore is instantiated here (pre-Seal) so QueueOperatorEventConsumer can
	// reference it in its Subscribe call. The store is populated later via
	// LoadQueueAtStartup (PL-005 step 8a). When cfg.QueueStore is non-nil the
	// caller-supplied instance is used directly (hk-8jh26 Fix 2).
	//
	// Spec ref: specs/queue-model.md §9.1 QM-060.
	// Bead ref: hk-7urls.
	qs := cfg.QueueStore
	if qs == nil {
		qs = newQueueStore()
	}
	handlerPauseCtrl := NewHandlerPauseController(bus, nil) // persistFn patched below when ProjectDir is set
	sharedRunRegistry := NewRunRegistry()

	// Construct the HandlerPausePolicyGoroutine and subscribe it to the bus
	// BEFORE Seal so event delivery is wired for the production run.
	//
	// At MVH we monitor AgentTypeClaudeCode (the only handler type in use).
	// Subscribe registers two asynchronous consumers: agent_rate_limit_status
	// and budget_exhausted. Bus worker-pool delivers events; no additional
	// goroutine is needed.
	//
	// Spec ref: docs/components/internal/handler-pause-and-resume.md §4 event flow.
	// Bead ref: hk-37zy8.
	pausePolicy := NewHandlerPausePolicyGoroutine(HandlerPausePolicyConfig{
		AgentType:  core.AgentTypeClaudeCode,
		Controller: handlerPauseCtrl,
		Registry:   sharedRunRegistry,
	})
	if subscribeErr := pausePolicy.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: HandlerPausePolicyGoroutine.Subscribe: %w", subscribeErr)
	}

	// Construct and subscribe the QueueOperatorEventConsumer BEFORE Seal so
	// operator_pause_status and operator_resuming events are delivered during the
	// production run (EV-009: subscribers MUST register before Seal).
	//
	// The consumer drives queue-level active ↔ paused-by-drain transitions per
	// QM-054 and QM-055. qs was constructed above (pre-Seal); cfg.ProjectDir is
	// forwarded for the persist-before-emit step (QM-063).
	//
	// Spec ref: specs/queue-model.md §8.5 QM-054, §8.6 QM-055.
	// Bead ref: hk-7urls.
	queueOpConsumer := NewQueueOperatorEventConsumer(QueueOperatorEventConsumerConfig{
		QueueStore: qs,
		ProjectDir: cfg.ProjectDir,
		Bus:        bus,
	})
	if subscribeErr := queueOpConsumer.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: QueueOperatorEventConsumer.Subscribe: %w", subscribeErr)
	}

	// Wire the per-bead completion notifier when --notify-stream is set (hk-ibilr).
	if cfg.NotifyStream != nil {
		notifyConsumer := NewNotifyStreamConsumer(cfg.NotifyStream)
		if subscribeErr := notifyConsumer.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: NotifyStreamConsumer.Subscribe: %w", subscribeErr)
		}
	}

	// Wire the SubscribeHub — long-lived wildcard observer that fans events
	// out to "subscribe" socket-op connections (hk-6ynv4). Always registered;
	// the hub is dormant until a subscribe op connects.
	subscribeHub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             bus,
		ActiveRuns:      sharedRunRegistry,
		EventsJSONLPath: cfg.JSONLLogPath, // for since_event_id replay (hk-a5sil)
	})
	if subscribeErr := subscribeHub.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: SubscribeHub.Subscribe: %w", subscribeErr)
	}

	// Wire the StaleWatcher — wildcard observer that emits run_stale when an
	// active run produces no event for M minutes (hk-wkzlc). Subscribed before
	// Seal so event delivery is wired for the production run (EV-009).
	// StartWatcher is called after Seal so the background goroutine runs within
	// the daemon's normal context lifetime.
	//
	// Bead ref: hk-wkzlc.
	staleWatcher := NewStaleWatcher(StaleWatcherConfig{
		SubscribeBus: bus,
		Emitter:      bus,
		Registry:     sharedRunRegistry,
	})
	if subscribeErr := staleWatcher.Subscribe(); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: StaleWatcher.Subscribe: %w", subscribeErr)
	}

	// Notify the test-only observer (when set) so tests can inspect bus
	// subscription state before Seal locks it.  Only reachable via
	// StartForTesting; production Start always passes a zero-value daemonTestHooks.
	//
	// Bead ref: hk-37zy8, hk-j192n.
	if hooks.busObserver != nil {
		hooks.busObserver(bus)
	}

	if sealErr := bus.Seal(); sealErr != nil {
		return fmt.Errorf("daemon.Start: seal bus: %w", sealErr)
	}

	// Start the stale-watch background goroutine after Seal so the bus is in
	// live-delivery mode (EV-009 sealed bus semantics). The goroutine runs until
	// ctx is cancelled. Non-fatal: if context is already cancelled the goroutine
	// exits immediately.
	//
	// Bead ref: hk-wkzlc.
	staleWatcher.StartWatcher(ctx)

	// Emit daemon_started (§8.7.1, hk-iarcy): F-class event marking the
	// startup landmark for post-crash-window detection (EV-023).
	// binary_commit_hash: forwarded from cfg.BinaryCommitHash which is stamped
	// at build time via -ldflags "-X main.commitHash=<sha>" (hk-mz0x4).
	// Falls back to "unknown" when the caller does not set the field (unit tests,
	// unstamped builds) to keep the envelope well-formed per the spec.
	binaryCommitHash := cfg.BinaryCommitHash
	if binaryCommitHash == "" {
		binaryCommitHash = "unknown"
	}
	daemonStartTime := time.Now().UTC()
	startedPayload := core.DaemonStartedPayload{
		StartedAt:        daemonStartTime.Format(time.RFC3339),
		PID:              os.Getpid(),
		BinaryCommitHash: binaryCommitHash,
	}
	payloadBytes, marshalErr := json.Marshal(startedPayload)
	if marshalErr != nil {
		return fmt.Errorf("daemon.Start: marshal daemon_started payload: %w", marshalErr)
	}
	if emitErr := bus.Emit(context.Background(), core.EventTypeDaemonStarted, payloadBytes); emitErr != nil {
		return fmt.Errorf("daemon.Start: emit daemon_started: %w", emitErr)
	}

	// Step 3 (PL-005 / PL-006, hk-60uvn): orphan sweep — BEFORE any socket
	// or listener bind. Sweep errors are non-fatal: orphan presence is
	// recoverable. Errors are surfaced via a daemon_orphan_sweep_completed
	// event with an errors summary field.
	//
	// Skip sweep when ProjectDir is empty (unit-test mode).
	if cfg.ProjectDir != "" {
		ctx := context.Background()
		projectHash := lifecycle.ComputeProjectHash(cfg.ProjectDir)

		// Construct the BI adapter early — BEFORE the orphan sweep emits — so
		// the PL-006 sixth-bullet stale-in_progress bead-reset can route through
		// the adapter (BI-010d) and roll its count into the same
		// daemon_orphan_sweep_completed event. The adapter construction
		// requires cfg.BrPath; when BrPath is unset (unit-test mode), the
		// bead-reset path is skipped and the rest of RunOrphanSweep proceeds.
		//
		// Sequencing rationale: see the package doc in
		// internal/lifecycle/orphansweepbeads.go. The bead-reset sweep runs
		// AFTER the existing filesystem+process sweep AND AFTER the BI-024a
		// `br --version` handshake has succeeded. At MVH the BI-024a handshake
		// is performed lazily by the adapter on first invocation; calling
		// `br list --status in_progress` inside the bead-reset sweep is the
		// first BI-write-surface adjacent call and therefore the handshake
		// effectively precedes the reset writes.
		//
		// Bead ref: hk-iuaed.4.
		var beadLedger lifecycle.InFlightBeadLedger
		var beadResetter lifecycle.BeadResetter
		var beadCat3cCloser lifecycle.BeadCat3cCloser
		var intentLogDir string
		if cfg.BrPath != "" {
			brAdapter, brAdapterErr := newBrAdapter(hooks, cfg.BrPath, cfg.ProjectDir)
			if brAdapterErr != nil {
				// Classify + emit divergence_inconclusive for BrSchemaMismatch per
				// BI-031b; other error categories are also classified so the event
				// bus always carries a structured observation.  Non-fatal: bead-reset
				// sweep is best-effort; Cat 0 (schema mismatch / unavailable) means
				// we proceed queue-less and the bead remains in_progress until the
				// next restart.
				//
				// Spec ref: specs/beads-integration.md §4.10 BI-031b.
				// Bead ref: hk-th378.
				_ = brcli.BrErrReconciliationCategoryWithEmit(ctx, brAdapterErr, "br-new-for-project-sweep", bus)
			} else {
				beadLedger = brAdapter
				beadResetter = brAdapter
				beadCat3cCloser = brAdapter // Cat 3c auto-reconciler (hk-lgtq2)
				intentLogDir = lifecycle.BeadsIntentsDir(cfg.ProjectDir)
			}
		}

		// Raw queue.json load for the orphan-sweep bead-provenance check
		// (hk-2ty0g). This is a lightweight read-only load — no QM-002a cross-
		// check, no bead-ledger queries — performed BEFORE the full
		// LoadQueueAtStartup at PL-005 step 8a. The two sets it produces let
		// the bead-reset sweep:
		//   - establish queue-based provenance for beads whose intent files
		//     were fully drained after a previous SIGKILL recovery (QueueOwned),
		//   - skip beads that the queue still considers live (QueueDispatched).
		//
		// Errors are non-fatal: a missing or corrupt queue.json yields empty
		// sets and the sweep falls back to intent-log provenance only (the
		// pre-fix behaviour, which is safe; it just misses the SIGKILL case).
		//
		// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet.
		// Bug ref: hk-2ty0g.
		var queueDispatched lifecycle.QueueDispatchedSet
		var queueOwned lifecycle.QueueOwnedSet
		rawQ, rawQErr := queue.Load(ctx, cfg.ProjectDir)
		if rawQErr != nil {
			logW := cfg.LogWriter
			if logW == nil {
				logW = os.Stderr
			}
			fmt.Fprintf(logW, "warn: pre-sweep queue.Load failed: %v — falling back to intent-log-only provenance\n", rawQErr)
		}
		if rawQErr == nil && rawQ != nil {
			queueDispatched = make(lifecycle.QueueDispatchedSet)
			queueOwned = make(lifecycle.QueueOwnedSet)
			for gi := range rawQ.Groups {
				for _, item := range rawQ.Groups[gi].Items {
					queueOwned[item.BeadID] = struct{}{}
					if item.Status == queue.ItemStatusDispatched {
						queueDispatched[item.BeadID] = struct{}{}
					}
				}
			}
		}

		sweepResult, sweepErr := RunOrphanSweep(
			ctx,
			cfg.ProjectDir,
			projectHash,
			daemonStartTime,
			OrphanSweepConfig{
				BeadLedger:      beadLedger,
				BeadResetter:    beadResetter,
				BeadCat3cCloser: beadCat3cCloser,
				MergeCommitScanner: lifecycle.GitMergeCommitScanner{
					ProjectDir:   cfg.ProjectDir,
					TargetBranch: "", // defaults to "main" inside the scanner
				},
				IntentLogDir:    intentLogDir,
				DaemonStartNS:   daemonStartTime.UnixNano(),
				QueueDispatched: queueDispatched,
				QueueOwned:      queueOwned,
			},
		)

		// Build and emit daemon_orphan_sweep_completed (§8.7.14, O-class).
		// Do NOT abort Start on sweep error per PL-006.
		sweepPayload := sweepResult.ToPayload()
		sweepPayloadBytes, sweepMarshalErr := json.Marshal(sweepPayload)
		if sweepMarshalErr != nil {
			// Marshal failure should not block startup; log and continue.
			sweepPayloadBytes = []byte(`{}`)
		}
		if sweepEmitErr := bus.Emit(ctx, core.EventTypeDaemonOrphanSweepCompleted, sweepPayloadBytes); sweepEmitErr != nil {
			// Non-fatal: bus emit failure at this stage does not block startup.
			_ = sweepEmitErr
		}
		// Surface sweep errors as the return value only if no other errors
		// occurred — but per bead spec, do NOT abort Start on sweep error.
		_ = sweepErr
	}

	// Step 4 (hk-ecrxy): register adapters and launch the work loop.
	//
	// AdapterRegistry: construct, register ClaudeCodeAdapter for core.AgentTypeClaudeCode,
	// seal.  The sealed registry is forwarded into handler.NewHandler as a latent
	// seam for post-MVH adapter-selection (hk-gql20.16).  Construct and seal here
	// to satisfy PL-020a composition-root ordering.
	adapterReg := handlercontract.NewAdapterRegistry()
	if regErr := handler.Register(adapterReg); regErr != nil {
		return fmt.Errorf("daemon.Start: register ClaudeCodeAdapter: %w", regErr)
	}
	// Seal the registry immediately: no further adapters at MVH.
	// The first ForAgent call would seal it anyway; explicit seal here makes the
	// ordering contract observable.
	if _, forAgentErr := adapterReg.ForAgent(core.AgentTypeClaudeCode); forAgentErr != nil {
		// ForAgent only fails if no adapter is registered — that would be a bug
		// in the Register call above; treat as fatal.
		return fmt.Errorf("daemon.Start: seal adapter registry: %w", forAgentErr)
	}

	// Construct the hook-session store once at the composition root (hk-gql20.21).
	// The same instance is forwarded to RunSocketListener (as HookRelayHandler)
	// and into workLoopDeps so the work loop can call WaitForOutcome in the
	// completion path (hk-gql20.22).
	//
	// Spec ref: specs/claude-hook-bridge.md §4.10 CHB-025.
	hookStore := newHookSessionStore()

	// PL-005 step 8a (QM-002 / QM-002a): load queue.json at startup BEFORE the
	// socket listener or work loop start.  Only runs when both ProjectDir and
	// BrPath are set (production mode); unit-test callers that omit one or both
	// skip the load cleanly.
	//
	// A forward-incompatible schema_version causes a fatal return with exit-code-2
	// semantics per QM-002.  Corrupt but parseable files produce a warning and a
	// nil queue (daemon proceeds without a queue).
	//
	// Spec ref: specs/queue-model.md §3.2 QM-002, §3.2a QM-002a.
	// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
	if cfg.ProjectDir != "" && cfg.BrPath != "" {
		brAdapterForQueue, brAdapterErr := newBrAdapter(hooks, cfg.BrPath, cfg.ProjectDir)
		if brAdapterErr != nil {
			// Classify + emit divergence_inconclusive for BrSchemaMismatch per
			// BI-031b.  Non-fatal: daemon proceeds without a queue; queue-* ops
			// return errors until a queue is submitted.
			//
			// Spec ref: specs/beads-integration.md §4.10 BI-031b.
			// Bead ref: hk-th378.
			_ = brcli.BrErrReconciliationCategoryWithEmit(context.Background(), brAdapterErr, "br-new-for-project-queue", bus)
		} else {
			loadedQueue, loadErr := lifecycle.LoadQueueAtStartup(
				context.Background(),
				cfg.ProjectDir,
				brAdapterForQueue,
				bus,
				nil, // slog.Default() is used when nil
			)
			if loadErr != nil {
				// ErrQueueSchemaUnsupported → fatal (exit code 2 per QM-002).
				return fmt.Errorf("daemon.Start: queue.json load: %w", loadErr)
			}
			if loadedQueue != nil {
				qs.SetQueue(loadedQueue)
			}
		}
	}

	// PL-005 step 8a (hk-m0k0a): wire the persistence function into the
	// HandlerPauseController (constructed pre-Seal above) and load any persisted
	// handler state from .harmonik/handler-state.json.
	//
	// The controller was constructed above (pre-Seal) with a nil persistFn so
	// that HandlerPausePolicyGoroutine.Subscribe could reference it before Seal.
	// Here we patch in the real persistFn (when ProjectDir is set) and then seed
	// the controller from disk.
	//
	// LoadHandlerPauseState seeds the controller with any paused handlers that
	// survived the last daemon run, ensuring "paused status MUST persist across
	// restarts" per specs/handler-pause.md §8.3 HP-008 (QM-055 analog).
	//
	// A forward-incompatible schema_version causes a fatal return (exit code 2).
	// File absent → all handlers default live (no-op).
	//
	// Spec ref: specs/handler-pause.md §3.5.
	// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
	// Bead ref: hk-m0k0a, hk-37zy8.
	if cfg.ProjectDir != "" {
		harmonikDir := filepath.Join(cfg.ProjectDir, ".harmonik")
		handlerPauseCtrl.SetPersistFn(MakeHandlerPausePersistFn(harmonikDir))
		if loadErr := LoadHandlerPauseState(context.Background(), harmonikDir, handlerPauseCtrl); loadErr != nil {
			return fmt.Errorf("daemon.Start: handler-state.json load: %w", loadErr)
		}
	}

	// PL-003 / CHB-025 (hk-tjl40): bind the Unix-domain socket so hook-relay
	// subprocesses can deliver outcome_emitted envelopes to the daemon.
	//
	// Only bind when ProjectDir is set; unit-test callers that omit ProjectDir
	// skip the socket (no path to bind). The socket listener runs concurrently
	// with the work loop and shuts down on the same ctx.
	//
	// QueueHandler: queue.NewHandlerAdapter wired when BrPath is set. A nil
	// QueueHandler causes all queue-* ops to return -32099 (no queue loaded).
	//
	// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 3a; §4.1 PL-003.
	if cfg.ProjectDir != "" {
		sockPath := filepath.Join(cfg.ProjectDir, ".harmonik", "daemon.sock")
		// .harmonik/ was already created above (pidfile block), but when
		// ProjectDir is set with BrPath="" (test mode skipping pidfile) we still
		// need the dir. MkdirAll is idempotent.
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if mkErr := os.MkdirAll(filepath.Dir(sockPath), 0o755); mkErr != nil {
			return fmt.Errorf("daemon.Start: mkdir-p .harmonik (socket): %w", mkErr)
		}

		// Construct the QueueHandler adapter. Nil when BrPath is unset (unit-test
		// mode); RunSocketListener accepts nil and returns -32099 for queue-* ops.
		// qs and bus are threaded in so the adapter can update the in-memory
		// QueueStore and emit events after each persist (hk-4ukkq, hk-lzs8r,
		// hk-peucr).
		var queueHandler QueueHandler
		if cfg.BrPath != "" {
			brAdapterForHandler, brHandlerErr := newBrAdapter(hooks, cfg.BrPath, cfg.ProjectDir)
			if brHandlerErr != nil {
				// Classify + emit divergence_inconclusive for BrSchemaMismatch per
				// BI-031b.  Non-fatal: socket handler proceeds without queue support;
				// queue-* ops return errors until a queue is submitted.
				//
				// Spec ref: specs/beads-integration.md §4.10 BI-031b.
				// Bead ref: hk-th378.
				_ = brcli.BrErrReconciliationCategoryWithEmit(context.Background(), brHandlerErr, "br-new-for-project-handler", bus)
			} else {
				queueHandler = queue.NewHandlerAdapter(newBRQueueLedger(brAdapterForHandler), cfg.ProjectDir, qs, bus)
			}
		}

		// Non-fatal: socket bind errors do not abort the daemon (PL-003 intent;
		// the absence of the socket is observable externally). Drain the done
		// channel to avoid goroutine leaks; error is discarded per the same
		// reasoning as defer ln.Close() discards errors in RunSocketListener.
		socketDone := make(chan error, 1)
		go func() {
			if queueHandler != nil {
				socketDone <- RunSocketListenerWithSubscribe(ctx, sockPath, &noopRequestHandler{}, hookStore, subscribeHub, queueHandler)
			} else {
				socketDone <- RunSocketListenerWithSubscribe(ctx, sockPath, &noopRequestHandler{}, hookStore, subscribeHub)
			}
		}()
		go func() { <-socketDone }() // drain: non-fatal; socket bind error discarded (see comment above)
	}

	// Skip the work loop when BrPath is not configured (unit-test mode).
	if cfg.BrPath != "" {
		deps, depsErr := newWorkLoopDeps(cfg, bus, workflowModeDefault, adapterReg, hookStore)
		if depsErr != nil {
			return fmt.Errorf("daemon.Start: work loop deps: %w", depsErr)
		}
		// Inject the QueueStore singleton so the work loop can pull from the
		// active queue (queue-pull dispatch path per execution-model.md §7.4 TS-1).
		//
		// Spec ref: specs/queue-model.md §9.1 QM-060; specs/execution-model.md §7.4.
		deps.queueStore = qs
			// Wire the wake channel so queue-submit RPCs immediately unblock the
			// workloop's idle sleep (hk-24xn1).
			deps.submitWakeC = qs.WakeCh()

			// Inject the HandlerPauseController so the dispatcher skip-on-paused gate
		// (hk-kac8g) can consult pause state before claiming each item.
		// nil → gate disabled; pre-hk-kac8g behaviour preserved for callers that
		// don't set the field.
		deps.handlerPauseController = cfg.HandlerPauseController

		// Inject the drain-cancel so harmonik run <bead-id> exits after the queue
		// completes (hk-icecw). The zero value (nil) preserves normal daemon behaviour.
		deps.cancelOnQueueDrain = cfg.CancelOnQueueDrain

		// Inject the exit-cancel so harmonik run <bead-id> exits on both
		// all-success AND paused-by-failure outcomes (hk-8jh26 Fix 1).
		// The zero value (nil) preserves normal daemon behaviour.
		deps.cancelOnQueueExit = cfg.CancelOnQueueExit

		// Inject the stop-dispatch context (hk-2o2i9): when set, the work loop's
		// outer poll checks this context for dispatch-halt instead of the main ctx,
		// so CancelOnQueueDrain/Exit do not kill in-flight reviewer goroutines.
		// The zero value (nil) falls back to ctx (backward-compat).
		deps.stopDispatchCtx = cfg.StopDispatchCtx

		// Inject the HandlerPauseController so the work loop can gate dispatch
		// on handler pause state (hk-m0k0a).
		deps.handlerPauseController = handlerPauseCtrl

		// Inject the shared RunRegistry so the work loop and the
		// HandlerPausePolicyGoroutine operate on the same in-flight snapshot.
		// The policy goroutine calls Registry.snapshotWithKeys() at pause time;
		// using the same instance as the work loop ensures the freeze-list reflects
		// actual in-flight runs rather than an empty registry.
		//
		// Bead ref: hk-37zy8.
		deps.runRegistry = sharedRunRegistry

		// Emit the composition-root wiring audit log when HARMONIK_DEBUG_WIRING=1
		// is set in the operator environment.  All 31 wiring points have been
		// established at this point; the log is a stable diff surface for catching
		// silent drops between daemon versions.
		//
		// Bead ref: hk-4mupj.
		logCompositionRoot(cfg.LogWriter)

		// Use the caller-supplied ctx to drive a clean shutdown. The production
		// caller (cmd/harmonik/main.go) passes a signal.NotifyContext so that
		// Ctrl-C / SIGTERM cancels the work loop without sending signals into
		// the test process (hk-7oz2f).
		loopDone := make(chan error, 1)
		go func() {
			loopDone <- runWorkLoop(ctx, deps)
		}()

		// Block until the work loop exits (either ctx cancelled or fatal error).
		<-loopDone
	}

	return nil
}
