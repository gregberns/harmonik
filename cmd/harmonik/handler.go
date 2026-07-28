// handler.go — `harmonik handler status` and `harmonik handler resume` subcommand
// implementations.
//
// Semantics (hk-39ryh — status):
//  1. Parse --type and --format flags.
//  2. Resolve the project directory.
//  3. Read .harmonik/handler-state.json (absent → all handlers live; show empty).
//  4. If --type is given, filter to that handler only.
//  5. Output JSON (--format json) or human-readable text (default).
//
// Semantics (hk-ejyku — resume):
//  1. Parse --type (required) and --project flags.
//  2. Resolve the project directory.
//  3. Read .harmonik/handler-state.json to validate handler type and current state.
//  4. Validate: unknown type → exit 2; already-live (without --force) → exit 3.
//  5. Update handler entry to "live", clear cause, bump paused_epoch.
//  6. Atomic-write updated handler-state.json (tmp → fsync → rename).
//  7. Append handler_resumed event to .harmonik/events/events.jsonl.
//  8. Print prior cause, in_flight_at_pause count, and confirmation.
//
// Both verbs read/write handler-state.json directly (no daemon socket required).
// HandlerPauseController (hk-9hwbw) is not yet wired; direct file I/O with
// atomic-write discipline (WM-026) is consistent with how `status` operates.
// When hk-9hwbw + hk-m0k0a land the daemon-side controller will own the state
// file; resume can then delegate to the socket. This is noted as a wiring site.
//
// Exit-code contract (status):
//
//	0  — success (output written)
//	1  — argument or file-parse error
//	2  — forward-incompatible schema version
//
// Exit-code contract (resume):
//
//	0  — success (handler resumed)
//	1  — argument or I/O error
//	2  — unknown handler type (not in handler-state.json)
//	3  — handler already live (not paused); use --force to no-op
//	4  — socket-unreachable (reserved for post-hk-9hwbw wiring; unused for now)
//
// Spec ref: docs/components/internal/handler-pause-and-resume.md §7.
// Spec ref: specs/event-model.md §8.11.2 (handler_resumed event).
// Bead ref: hk-39ryh (status), hk-ejyku (resume).

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// handlerStateSchemaVersion is the only schema version the CLI accepts.
// A higher schema version causes exit 2 (forward-incompatible, mirrors QM-002).
const handlerStateSchemaVersion = 1

// handlerStateFile is the on-disk file written by HandlerPauseController.
// Sibling to queue.json; atomic-write discipline per WM-026.
const handlerStateFile = "handler-state.json"

// ---------------------------------------------------------------------------
// On-disk schema types (must stay in sync with hk-m0k0a persistence structs)
// ---------------------------------------------------------------------------

// handlerStateDisk is the top-level on-disk structure of .harmonik/handler-state.json.
type handlerStateDisk struct {
	SchemaVersion int                         `json:"schema_version"`
	Handlers      map[string]handlerEntryDisk `json:"handlers"`
}

// handlerEntryDisk represents one handler-type entry in handler-state.json.
// Cause reuses core.HandlerPauseCause — same JSON shape (failure_class, sub_reason,
// source_run_id, source_bead_id, tripped_at) per specs/handler-pause.md §5.1.
// core.FailureClass is type string so serialisation is identical to a bare string field.
type handlerEntryDisk struct {
	Status          string                  `json:"status"`
	Cause           *core.HandlerPauseCause `json:"cause"`
	InFlightAtPause []inFlightRunDisk       `json:"in_flight_at_pause"`
	PausedEpoch     int                     `json:"paused_epoch"`
}

// inFlightRunDisk is a single entry in in_flight_at_pause.
// No equivalent core type exists (core.HandlerPausedPayload uses InFlightCount int,
// not a per-run list), so this remains a CLI-local disk-schema struct.
type inFlightRunDisk struct {
	RunID        string `json:"run_id"`
	BeadID       string `json:"bead_id"`
	DispatchedAt string `json:"dispatched_at"`
}

// ---------------------------------------------------------------------------
// JSON output types (adds derived held_count per §8.2 and §8.3)
// ---------------------------------------------------------------------------

// handlerStatusJSONOutput is the top-level JSON response for --format json.
// Mirrors handler-state.json plus a derived held_count per §8.2.
type handlerStatusJSONOutput struct {
	SchemaVersion int                         `json:"schema_version"`
	Handlers      map[string]handlerEntryJSON `json:"handlers"`
}

// handlerEntryJSON is one handler entry in the JSON output.
// Cause reuses core.HandlerPauseCause (same JSON shape as the disk struct).
type handlerEntryJSON struct {
	Status          string                  `json:"status"`
	Cause           *core.HandlerPauseCause `json:"cause"`
	InFlightAtPause []inFlightRunDisk       `json:"in_flight_at_pause"`
	PausedEpoch     int                     `json:"paused_epoch"`
	// HeldCount is the number of pending queue items whose resolved agent_type
	// is this handler. Omitted when 0 — the live count is owned by the
	// dispatcher; the CLI cannot query it without a socket connection (hk-9hwbw).
	// Submitter agents MAY derive the count from queue-status if needed.
	// Wiring site: once hk-m0k0a / hk-xlq2e land, populate from daemon socket.
	HeldCount int `json:"held_count,omitempty"`
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

// handlerUsage prints the help text for `harmonik handler --help`.
func handlerUsage(out io.Writer) error {
	if _, err := fmt.Fprint(out, `harmonik handler — inspect or resume a paused handler

USAGE
  harmonik handler <verb> [flags]

VERBS
  status   Show handler pause state (no daemon required)
  resume   Resume a paused handler

FLAGS (status)
  --type AGENT-TYPE       Filter to a single handler type (e.g. claude-code)
  --format json|text      Output format (default text)
  --json                  Shorthand for --format json
  --project DIR           Project directory (default: current working directory)

FLAGS (resume)
  --type AGENT-TYPE       Handler type to resume (required)
  --force                 No-op if already live, instead of error
  --project DIR           Project directory (default: current working directory)

EXAMPLES
  harmonik handler status
  harmonik handler status --type claude-code --format json
  harmonik handler resume --type claude-code
`); err != nil {
		return fmt.Errorf("print handler usage: %w", err)
	}
	return nil
}

// statusUsage prints per-verb help for `harmonik handler status --help`.
func statusUsage(out io.Writer) error {
	if _, err := fmt.Fprint(out, `harmonik handler status — show handler pause state

USAGE
  harmonik handler status [flags]

FLAGS
  --type AGENT-TYPE       Filter to a single handler type (e.g. claude-code)
  --format json|text      Output format (default text)
  --json                  Shorthand for --format json
  --project DIR           Project directory (default: current working directory)

EXIT CODES
  0   Success (output written)
  1   Argument or file-parse error
  2   Forward-incompatible schema version

EXAMPLES
  harmonik handler status
  harmonik handler status --type claude-code
  harmonik handler status --type claude-code --format json
`); err != nil {
		return fmt.Errorf("print handler status usage: %w", err)
	}
	return nil
}

// resumeUsage prints per-verb help for `harmonik handler resume --help`.
func resumeUsage(out io.Writer) error {
	if _, err := fmt.Fprint(out, `harmonik handler resume — resume a paused handler

USAGE
  harmonik handler resume --type AGENT-TYPE [flags]

FLAGS
  --type AGENT-TYPE       Handler type to resume (required)
  --force                 No-op if already live, instead of error
  --project DIR           Project directory (default: current working directory)

EXIT CODES
  0   Success (handler resumed)
  1   Argument or I/O error
  2   Unknown handler type (not in handler-state.json)
  3   Handler already live; use --force to no-op

EXAMPLES
  harmonik handler resume --type claude-code
  harmonik handler resume --type claude-code --force
`); err != nil {
		return fmt.Errorf("print handler resume usage: %w", err)
	}
	return nil
}

// runHandlerSubcommand implements `harmonik handler <verb> [flags]`.
// subArgs is os.Args[2:] (everything after "handler").
func runHandlerSubcommand(subArgs []string) int {
	return runHandlerSubcommandIO(subArgs, os.Stdout, os.Stderr)
}

// runHandlerSubcommandIO is the testable variant that accepts explicit writers.
func runHandlerSubcommandIO(subArgs []string, out, errOut io.Writer) int {
	if len(subArgs) == 0 {
		if _, err := fmt.Fprintln(errOut, "harmonik handler: missing verb"); err != nil {
			return 1
		}
		if _, err := fmt.Fprintln(errOut, "usage: harmonik handler <verb> [flags]"); err != nil {
			return 1
		}
		if _, err := fmt.Fprintln(errOut, "  status  [--type <agent-type>] [--format json|text] [--project DIR]"); err != nil {
			return 1
		}
		if _, err := fmt.Fprintln(errOut, "  resume  --type <agent-type> [--force] [--project DIR]"); err != nil {
			return 1
		}
		return 1
	}

	// Intercept --help / -h before verb dispatch.
	if subArgs[0] == "--help" || subArgs[0] == "-h" {
		if err := handlerUsage(out); err != nil {
			return 1
		}
		return 0
	}

	verb := subArgs[0]
	switch verb {
	case "status":
		return runHandlerStatus(subArgs[1:], out, errOut)
	case "resume":
		return runHandlerResume(subArgs[1:], out, errOut)
	default:
		if _, err := fmt.Fprintf(errOut, "harmonik handler: unrecognised verb %q; supported verbs: status, resume\n", verb); err != nil {
			return 1
		}
		return 1
	}
}

// ---------------------------------------------------------------------------
// resume verb
// ---------------------------------------------------------------------------

// resumeExitUnknownType is exit 2 — handler type not found in handler-state.json.
const resumeExitUnknownType = 2

// resumeExitAlreadyLive is exit 3 — handler is already live (not paused).
const resumeExitAlreadyLive = 3

// handlerResumedEventType is the event type per specs/event-model.md §8.11.2.
const handlerResumedEventType = "handler_resumed"

// handlerResumedEvent is the JSONL envelope written to events.jsonl on success.
// Fields per event-model.md §8.11.2: agent_type, by, prior_cause, paused_epoch.
// PriorCause reuses core.HandlerPauseCause directly — same JSON shape.
type handlerResumedEvent struct {
	EventType   string                  `json:"event_type"`
	EmittedAt   string                  `json:"emitted_at"`
	AgentType   string                  `json:"agent_type"`
	By          string                  `json:"by"`
	PriorCause  *core.HandlerPauseCause `json:"prior_cause"`
	PausedEpoch int                     `json:"paused_epoch"`
}

// runHandlerResume implements `harmonik handler resume --type <agent-type> [--force] [--project DIR]`.
//
// Exit-code contract:
//
//	0  — success
//	1  — argument or I/O error
//	2  — unknown handler type (not in handler-state.json)
//	3  — handler already live (use --force to no-op)
func runHandlerResume(subArgs []string, out io.Writer, errOut io.Writer) int {
	// --- Parse flags ---

	typeFlag := ""
	projectDirFlag := ""
	forceFlag := false

	for i := 0; i < len(subArgs); i++ {
		switch {
		case subArgs[i] == "--help" || subArgs[i] == "-h":
			if err := resumeUsage(out); err != nil {
				return 1
			}
			return 0

		case subArgs[i] == "--type" && i+1 < len(subArgs):
			i++
			typeFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--type="):
			typeFlag = strings.TrimPrefix(subArgs[i], "--type=")

		case subArgs[i] == "--project" && i+1 < len(subArgs):
			i++
			projectDirFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--project="):
			projectDirFlag = strings.TrimPrefix(subArgs[i], "--project=")

		case subArgs[i] == "--force":
			forceFlag = true

		case strings.HasPrefix(subArgs[i], "-"):
			if _, err := fmt.Fprintf(errOut, "harmonik handler resume: unknown flag %q\n", subArgs[i]); err != nil {
				return 1
			}
			return 1
		default:
			if _, err := fmt.Fprintf(errOut, "harmonik handler resume: unexpected argument %q\n", subArgs[i]); err != nil {
				return 1
			}
			return 1
		}
	}

	if typeFlag == "" {
		if _, err := fmt.Fprintln(errOut, "harmonik handler resume: --type is required"); err != nil {
			return 1
		}
		return 1
	}

	// --- Resolve project directory ---

	if projectDirFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(errOut, "harmonik handler resume: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		projectDirFlag = wd
	}
	projectDir, err := filepath.Abs(projectDirFlag)
	if err != nil {
		if _, writeErr := fmt.Fprintf(errOut, "harmonik handler resume: cannot resolve project path %q: %v\n", projectDirFlag, err); writeErr != nil {
			return 1
		}
		return 1
	}

	// --- Read handler-state.json ---

	statePath := filepath.Join(projectDir, ".harmonik", handlerStateFile)
	// TOCTOU note: reading statePath here and renaming over it below creates a
	// window in which a concurrent writer (e.g., a future daemon socket handler)
	// could overwrite the file between our read and our rename, silently losing
	// their update. This is acceptable for now because only the operator runs
	// `harmonik handler resume` and the daemon does not yet write handler-state.json
	// directly (HandlerPauseController is not yet wired). When hk-9hwbw lands the
	// daemon-socket delegation path, the CLI will delegate to the controller and
	// this file-level TOCTOU window will be eliminated. Until then, no advisory
	// lock is held between read and rename.
	state, exitCode := loadHandlerState(statePath, errOut)
	if exitCode != 0 {
		return exitCode
	}

	// --- Validate: type must be known (present in file) ---

	entry, known := state.Handlers[typeFlag]
	if !known {
		// Type not in file at all → there's no pause record; we can't resume
		// something that was never paused. Exit 2 per bead spec.
		if _, err := fmt.Fprintf(errOut, "harmonik handler resume: handler type %q not found in handler-state.json (never paused)\n", typeFlag); err != nil {
			return 1
		}
		return resumeExitUnknownType
	}

	// --- Validate: must be paused (or --force allows already-live) ---

	currentStatus := entry.Status
	if currentStatus == "" {
		currentStatus = "live"
	}
	if currentStatus != "paused" {
		if forceFlag {
			// --force: treat as no-op, print notice, exit 0.
			if _, err := fmt.Fprintf(out, "handler %q is already live (--force: no-op)\n", typeFlag); err != nil {
				return 1
			}
			return 0
		}
		if _, err := fmt.Fprintf(errOut, "harmonik handler resume: handler %q is already live (status=%s); use --force to no-op\n", typeFlag, currentStatus); err != nil {
			return 1
		}
		return resumeExitAlreadyLive
	}

	// Capture prior cause and paused_epoch before mutation.
	priorCause := entry.Cause
	priorEpoch := entry.PausedEpoch
	inFlightCount := len(entry.InFlightAtPause)

	// --- Update entry: set live, clear cause ---

	state.Handlers[typeFlag] = handlerEntryDisk{
		Status:          "live",
		Cause:           nil,
		InFlightAtPause: []inFlightRunDisk{},
		PausedEpoch:     priorEpoch, // epoch preserved; HandlerPauseController will increment on next pause
	}

	// --- Atomic-write updated handler-state.json (WM-026: tmp → fsync → rename) ---

	if writeErr := atomicWriteHandlerState(statePath, state); writeErr != nil {
		if err := handlerWritef(errOut, "harmonik handler resume: %v\n", writeErr); err != nil {
			return 1
		}
		return 1
	}

	// --- Emit handler_resumed event to events.jsonl (event-model §8.11.2) ---
	// Best-effort: event emission failure does not roll back the state update.
	// The state file is authoritative; the event log is observational.

	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	if emitErr := emitHandlerResumedEvent(eventsPath, typeFlag, priorCause, priorEpoch); emitErr != nil {
		handlerBestEffortWarning(errOut, "harmonik handler resume: warning: handler_resumed event not recorded: %v\n", emitErr)
	}

	// --- Print confirmation ---

	if err := printHandlerResumeSuccess(out, typeFlag, priorCause, inFlightCount); err != nil {
		return 1
	}

	return 0
}

func printHandlerResumeSuccess(out io.Writer, agentType string, priorCause *core.HandlerPauseCause, inFlightCount int) error {
	if _, err := fmt.Fprintf(out, "handler %q resumed\n", agentType); err != nil {
		return err
	}
	if priorCause != nil {
		if _, err := fmt.Fprintln(out, "  prior cause:"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    failure_class: %s\n", priorCause.FailureClass); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    sub_reason:    %s\n", priorCause.SubReason); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    source_bead:   %s\n", priorCause.SourceBeadID); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    source_run:    %s\n", priorCause.SourceRunID); err != nil {
			return err
		}
		trippedAt := priorCause.TrippedAt
		if t, err := time.Parse(time.RFC3339Nano, priorCause.TrippedAt); err == nil {
			trippedAt = t.Format(time.RFC3339)
		}
		if _, err := fmt.Fprintf(out, "    tripped_at:    %s\n", trippedAt); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "  in_flight_at_pause: %d\n", inFlightCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "  dispatcher_backlog_held: (unavailable — HandlerPauseController not yet wired)"); err != nil {
		return err
	}
	return nil
}

// atomicWriteHandlerState writes state to statePath using WM-026 atomic discipline:
// write to a temp file, fsync, rename over the target, fsync the parent directory.
// Returns 0 on success, 1 on any I/O error.
func atomicWriteHandlerState(statePath string, state *handlerStateDisk) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("serialise handler-state.json: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(statePath)
	tmpFile, err := os.CreateTemp(dir, ".handler-state-tmp-")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmpFile.Name()

	// Write content.
	if _, writeErr := tmpFile.Write(data); writeErr != nil {
		return fmt.Errorf("write temp file %s: %w", tmpPath, errors.Join(writeErr, cleanupHandlerStateTemp(tmpFile, tmpPath)))
	}

	// fsync the temp file before rename.
	if syncErr := tmpFile.Sync(); syncErr != nil {
		return fmt.Errorf("fsync %s: %w", tmpPath, errors.Join(syncErr, cleanupHandlerStateTemp(tmpFile, tmpPath)))
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		return fmt.Errorf("close %s: %w", tmpPath, errors.Join(closeErr, cleanupHandlerStateTemp(nil, tmpPath)))
	}

	// Rename (atomic on POSIX). This is the other end of the TOCTOU window noted
	// at the loadHandlerState call above: a concurrent writer that read the file
	// before this rename completes will have its update silently lost when this
	// rename lands. Acceptable for now (single operator, no daemon writer). Resolved
	// by hk-9hwbw daemon-socket delegation.
	if renameErr := os.Rename(tmpPath, statePath); renameErr != nil {
		return fmt.Errorf("rename %s → %s: %w", tmpPath, statePath, errors.Join(renameErr, cleanupHandlerStateTemp(nil, tmpPath)))
	}

	// fsync the parent directory to flush the directory entry.
	//nolint:gosec // G304: dir is the parent directory of the caller-supplied state path.
	dirF, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open %s for directory fsync: %w", dir, err)
	}
	syncErr := dirF.Sync()
	closeErr := dirF.Close()
	if syncErr != nil {
		return fmt.Errorf("fsync directory %s: %w", dir, errors.Join(syncErr, closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("close directory %s: %w", dir, closeErr)
	}

	return nil
}

func cleanupHandlerStateTemp(tmpFile *os.File, tmpPath string) error {
	var cleanupErr error
	if tmpFile != nil {
		cleanupErr = errors.Join(cleanupErr, tmpFile.Close())
	}
	return errors.Join(cleanupErr, os.Remove(tmpPath))
}

func handlerWritef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

func handlerBestEffortWarning(w io.Writer, format string, args ...any) {
	if err := handlerWritef(w, format, args...); err != nil {
		return
	}
}

// emitHandlerResumedEvent appends a handler_resumed event line to eventsPath.
// Best-effort: failures are reported as warnings by the caller but never roll back
// the authoritative state-file update per §8.11.
//
// Before emitting, the function constructs a core.HandlerResumedPayload and calls
// .Valid() on it (event-model §8.11.2 payload contract). If the payload is invalid
// (e.g. PausedEpoch < 1 or cause fields empty), the emit is skipped and a warning
// is printed to stderr. This prevents replay tooling from ingesting a malformed
// handler_resumed event that would fail schema validation.
func emitHandlerResumedEvent(eventsPath, agentType string, priorCause *core.HandlerPauseCause, pausedEpoch int) error {
	// Build a typed HandlerResumedPayload and validate before emitting.
	// priorCause is already core.HandlerPauseCause (no conversion needed — the disk
	// struct now reuses the core type directly per hk-n8yyk dedupe).
	var typedPriorCause core.HandlerPauseCause
	if priorCause != nil {
		typedPriorCause = *priorCause
	}
	typedPayload := core.HandlerResumedPayload{
		AgentType:   core.AgentType(agentType),
		By:          core.HandlerResumedByOperator,
		PriorCause:  typedPriorCause,
		PausedEpoch: pausedEpoch,
	}
	if !typedPayload.Valid() {
		// The payload does not satisfy event-model §8.11.2 validation rules.
		// Skip the emit rather than writing a malformed event that replay tooling
		// would reject. The state file update already succeeded; this is observable
		// from handler-state.json. The caller reports the skipped event as a
		// best-effort warning.
		return fmt.Errorf("payload invalid (paused_epoch=%d agent_type=%q)", pausedEpoch, agentType)
	}

	evt := handlerResumedEvent{
		EventType:   handlerResumedEventType,
		EmittedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		AgentType:   agentType,
		By:          string(core.HandlerResumedByOperator),
		PriorCause:  priorCause,
		PausedEpoch: pausedEpoch,
	}
	line, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal handler_resumed event: %w", err)
	}
	line = append(line, '\n')

	// Ensure the events directory exists (the daemon may not have run yet). A
	// failure here guarantees the OpenFile below fails too, so bail out on the
	// same terms: this whole emit path is observational and the state file has
	// already been updated.
	if mkErr := os.MkdirAll(filepath.Dir(eventsPath), core.HarmonikDirMode); mkErr != nil {
		return fmt.Errorf("create events directory: %w", mkErr)
	}

	f, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644) //nolint:gosec // G304: operator-controlled project dir
	if err != nil {
		return fmt.Errorf("open events file: %w", err)
	}
	_, writeErr := f.Write(line)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("append handler_resumed event: %w", errors.Join(writeErr, closeErr))
	}
	return nil
}

// runHandlerStatus implements `harmonik handler status`.
func runHandlerStatus(subArgs []string, out io.Writer, errOut io.Writer) int {
	// --- Parse flags ---

	typeFlag := ""
	formatFlag := "text"
	projectDirFlag := ""

	for i := 0; i < len(subArgs); i++ {
		switch {
		case subArgs[i] == "--help" || subArgs[i] == "-h":
			if err := statusUsage(out); err != nil {
				return 1
			}
			return 0

		case subArgs[i] == "--type" && i+1 < len(subArgs):
			i++
			typeFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--type="):
			typeFlag = strings.TrimPrefix(subArgs[i], "--type=")

		case subArgs[i] == "--format" && i+1 < len(subArgs):
			i++
			formatFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--format="):
			formatFlag = strings.TrimPrefix(subArgs[i], "--format=")

		case subArgs[i] == "--json":
			// Convenience alias: --json ≡ --format json (mirrors queue status conventions).
			formatFlag = "json"

		case subArgs[i] == "--project" && i+1 < len(subArgs):
			i++
			projectDirFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--project="):
			projectDirFlag = strings.TrimPrefix(subArgs[i], "--project=")

		case strings.HasPrefix(subArgs[i], "-"):
			if _, err := fmt.Fprintf(errOut, "harmonik handler status: unknown flag %q\n", subArgs[i]); err != nil {
				return 1
			}
			return 1
		default:
			if _, err := fmt.Fprintf(errOut, "harmonik handler status: unexpected argument %q\n", subArgs[i]); err != nil {
				return 1
			}
			return 1
		}
	}

	if formatFlag != "json" && formatFlag != "text" {
		if _, err := fmt.Fprintf(errOut, "harmonik handler status: --format must be json or text (got %q)\n", formatFlag); err != nil {
			return 1
		}
		return 1
	}

	// --- Resolve project directory ---

	if projectDirFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(errOut, "harmonik handler status: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		projectDirFlag = wd
	}
	projectDir, err := filepath.Abs(projectDirFlag)
	if err != nil {
		if _, writeErr := fmt.Fprintf(errOut, "harmonik handler status: cannot resolve project path %q: %v\n", projectDirFlag, err); writeErr != nil {
			return 1
		}
		return 1
	}

	// --- Read handler-state.json ---

	statePath := filepath.Join(projectDir, ".harmonik", handlerStateFile)
	state, exitCode := loadHandlerState(statePath, errOut)
	if exitCode != 0 {
		return exitCode
	}

	// --- Filter by --type if given ---

	if typeFlag != "" {
		entry, ok := state.Handlers[typeFlag]
		if !ok {
			// Handler type not in the file → it is live (file-absent = all live).
			entry = handlerEntryDisk{
				Status:          "live",
				Cause:           nil,
				InFlightAtPause: []inFlightRunDisk{},
				PausedEpoch:     0,
			}
		}
		state.Handlers = map[string]handlerEntryDisk{typeFlag: entry}
	}

	// --- Render output ---

	if formatFlag == "json" {
		return renderJSON(state, out, errOut)
	}
	return renderText(state, typeFlag, out)
}

// loadHandlerState reads and parses handler-state.json.
// Returns a synthesised empty state (all live) when the file is absent.
// Returns (nil, 1) on parse error and (nil, 2) on forward-incompatible schema.
func loadHandlerState(statePath string, errOut io.Writer) (result *handlerStateDisk, exitCode int) {
	data, err := os.ReadFile(statePath) //nolint:gosec // G304: operator-controlled project dir
	if err != nil {
		if os.IsNotExist(err) {
			// File absent → no handlers have ever been paused; return empty state.
			return &handlerStateDisk{
				SchemaVersion: handlerStateSchemaVersion,
				Handlers:      map[string]handlerEntryDisk{},
			}, 0
		}
		if writeErr := handlerWritef(errOut, "harmonik handler status: cannot read %s: %v\n", statePath, err); writeErr != nil {
			return nil, 1
		}
		return nil, 1
	}

	var state handlerStateDisk
	if jsonErr := json.Unmarshal(data, &state); jsonErr != nil {
		if writeErr := handlerWritef(errOut, "harmonik handler status: cannot parse %s: %v\n", statePath, jsonErr); writeErr != nil {
			return nil, 1
		}
		return nil, 1
	}

	// Schema-version guard: mirrors QM-002 forward-incompatible handling.
	if state.SchemaVersion > handlerStateSchemaVersion {
		if writeErr := handlerWritef(errOut,
			"harmonik handler status: %s schema_version %d is newer than this binary supports (%d); upgrade harmonik\n",
			statePath, state.SchemaVersion, handlerStateSchemaVersion); writeErr != nil {
			return nil, 1
		}
		return nil, 2
	}
	if state.Handlers == nil {
		state.Handlers = map[string]handlerEntryDisk{}
	}

	return &state, 0
}

// ---------------------------------------------------------------------------
// Renderers
// ---------------------------------------------------------------------------

// renderJSON writes the JSON status output to out.
func renderJSON(state *handlerStateDisk, out, errOut io.Writer) int {
	result := handlerStatusJSONOutput{
		SchemaVersion: handlerStateSchemaVersion,
		Handlers:      make(map[string]handlerEntryJSON, len(state.Handlers)),
	}
	for agentType, entry := range state.Handlers {
		inFlight := entry.InFlightAtPause
		if inFlight == nil {
			inFlight = []inFlightRunDisk{}
		}
		result.Handlers[agentType] = handlerEntryJSON{
			Status:          entry.Status,
			Cause:           entry.Cause,
			InFlightAtPause: inFlight,
			PausedEpoch:     entry.PausedEpoch,
			HeldCount:       0, // derived; see struct comment — always 0 at the CLI level
		}
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		if writeErr := handlerWritef(errOut, "harmonik handler status: cannot encode JSON: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

// renderText writes human-readable status to out.
func renderText(state *handlerStateDisk, typeFilter string, out io.Writer) int {
	if len(state.Handlers) == 0 {
		if typeFilter != "" {
			if _, err := fmt.Fprintf(out, "handler %q: live (no pause record)\n", typeFilter); err != nil {
				return 1
			}
		} else {
			if _, err := fmt.Fprintln(out, "no handler-pause records (all handlers live)"); err != nil {
				return 1
			}
		}
		return 0
	}

	// Sort for deterministic output.
	types := make([]string, 0, len(state.Handlers))
	for t := range state.Handlers {
		types = append(types, t)
	}
	sort.Strings(types)

	for _, agentType := range types {
		entry := state.Handlers[agentType]
		if err := printHandlerTextEntry(out, agentType, entry); err != nil {
			return 1
		}
	}
	return 0
}

// printHandlerTextEntry renders one handler entry in human-readable form.
func printHandlerTextEntry(out io.Writer, agentType string, entry handlerEntryDisk) error {
	status := entry.Status
	if status == "" {
		status = "live"
	}

	if _, err := fmt.Fprintf(out, "handler: %s\n", agentType); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  status: %s\n", status); err != nil {
		return err
	}

	if status == "paused" && entry.Cause != nil {
		c := entry.Cause
		if _, err := fmt.Fprintln(out, "  cause:"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    failure_class: %s\n", c.FailureClass); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    sub_reason:    %s\n", c.SubReason); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    source_bead:   %s\n", c.SourceBeadID); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "    source_run:    %s\n", c.SourceRunID); err != nil {
			return err
		}
		trippedAt := c.TrippedAt
		if t, err := time.Parse(time.RFC3339Nano, c.TrippedAt); err == nil {
			trippedAt = t.Format(time.RFC3339)
		}
		if _, err := fmt.Fprintf(out, "    tripped_at:    %s\n", trippedAt); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "  paused_epoch: %d\n", entry.PausedEpoch); err != nil {
			return err
		}

		if len(entry.InFlightAtPause) > 0 {
			if _, err := fmt.Fprintf(out, "  in_flight_at_pause (%d):\n", len(entry.InFlightAtPause)); err != nil {
				return err
			}
			for _, r := range entry.InFlightAtPause {
				if _, err := fmt.Fprintf(out, "    - bead %s (run %s)\n", r.BeadID, r.RunID); err != nil {
					return err
				}
			}
		} else {
			if _, err := fmt.Fprintln(out, "  in_flight_at_pause: (none)"); err != nil {
				return err
			}
		}
	}
	return nil
}
