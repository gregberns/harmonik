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

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
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

	// HandlerEnv is the environment for handler subprocesses in "KEY=VALUE" form.
	//
	// When nil the child inherits no environment. Production callers MUST inject
	// at minimum HARMONIK_PROJECT_HASH (lifecycle.ProvenanceEnvVar). Tests may
	// supply a minimal environment or nil.
	//
	// Bead ref: hk-ecrxy.
	HandlerEnv []string
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
	// hk-8i31.83, hk-8mup.63). Seal immediately — MVH has no subscribers yet;
	// this will be unsealed when handlers register (post-MVH beads add Subscribe
	// calls before Seal).
	bus := eventbus.NewBusImplWithWriter(registry, jsonlWriter)
	if sealErr := bus.Seal(); sealErr != nil {
		return fmt.Errorf("daemon.Start: seal bus: %w", sealErr)
	}

	// Emit daemon_started (§8.7.1, hk-iarcy): F-class event marking the
	// startup landmark for post-crash-window detection (EV-023).
	// binary_commit_hash: use a placeholder until build-info injection lands;
	// the field is required by the spec so we emit "unknown" to keep the
	// envelope well-formed.
	daemonStartTime := time.Now().UTC()
	startedPayload := core.DaemonStartedPayload{
		StartedAt:        daemonStartTime.Format(time.RFC3339),
		PID:              os.Getpid(),
		BinaryCommitHash: "unknown", // TODO(follow-up): inject from ldflags at build time
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
		sweepResult, sweepErr := RunOrphanSweep(
			ctx,
			cfg.ProjectDir,
			projectHash,
			daemonStartTime,
			OrphanSweepConfig{}, // nil fields fall back to OS-backed implementations
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
	// seal.  The work loop uses the registry indirectly via handler.NewHandler; the
	// registry is not currently forwarded to the handler (post-MVH wiring adds that
	// seam).  Construct and seal here to satisfy PL-020a composition-root ordering.
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

	// Skip the work loop when BrPath is not configured (unit-test mode).
	if cfg.BrPath != "" {
		deps, depsErr := newWorkLoopDeps(cfg, bus)
		if depsErr != nil {
			return fmt.Errorf("daemon.Start: work loop deps: %w", depsErr)
		}

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
