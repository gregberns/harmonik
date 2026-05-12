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

	// ProjectDir is the root directory of the harmonik project being managed.
	// Used to resolve daemon-local paths (pidfile, run directory, etc.).
	// Required for non-test invocations; empty string skips pidfile acquisition.
	//
	// Spec ref: specs/process-lifecycle.md §4.6 PL-020.
	ProjectDir string
}

// Start is the composition-root entry point for the harmonik daemon.
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
func Start(cfg Config) error {
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
	startedPayload := core.DaemonStartedPayload{
		StartedAt:        time.Now().UTC().Format(time.RFC3339),
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

	return nil
}
