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
// Both verbs read/write handler-state.json directly (no daemon socket required
// at MVH). HandlerPauseController (hk-9hwbw) is not yet wired; direct file I/O
// with atomic-write discipline (WM-026) is consistent with how `status` operates.
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
//	4  — socket-unreachable (reserved for post-hk-9hwbw wiring; unused at MVH)
//
// Spec ref: docs/components/internal/handler-pause-and-resume.md §7.
// Spec ref: specs/event-model.md §8.11.2 (handler_resumed event).
// Bead ref: hk-39ryh (status), hk-ejyku (resume).

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// handlerStateSchemaVersion is the only schema version the CLI accepts at MVH.
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
	SchemaVersion int                        `json:"schema_version"`
	Handlers      map[string]handlerEntryDisk `json:"handlers"`
}

// handlerEntryDisk represents one handler-type entry in handler-state.json.
type handlerEntryDisk struct {
	Status           string              `json:"status"`
	Cause            *handlerCauseDisk   `json:"cause"`
	InFlightAtPause  []inFlightRunDisk   `json:"in_flight_at_pause"`
	PausedEpoch      int                 `json:"paused_epoch"`
}

// handlerCauseDisk is the cause sub-object inside a paused handler entry.
type handlerCauseDisk struct {
	FailureClass  string `json:"failure_class"`
	SubReason     string `json:"sub_reason"`
	SourceRunID   string `json:"source_run_id"`
	SourceBeadID  string `json:"source_bead_id"`
	TrippedAt     string `json:"tripped_at"`
}

// inFlightRunDisk is a single entry in in_flight_at_pause.
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
	SchemaVersion int                           `json:"schema_version"`
	Handlers      map[string]handlerEntryJSON   `json:"handlers"`
}

// handlerEntryJSON is one handler entry in the JSON output.
type handlerEntryJSON struct {
	Status          string            `json:"status"`
	Cause           *handlerCauseDisk `json:"cause"`
	InFlightAtPause []inFlightRunDisk `json:"in_flight_at_pause"`
	PausedEpoch     int               `json:"paused_epoch"`
	// HeldCount is the number of pending queue items whose resolved agent_type
	// is this handler. At MVH this is always 0 — the live count is owned by the
	// dispatcher; the CLI cannot query it without a socket connection. Submitter
	// agents MAY derive the count from queue-status if needed. Kept in the
	// output schema for forward-compatibility (hk-m0k0a / hk-xlq2e).
	HeldCount int `json:"held_count"`
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

// runHandlerSubcommand implements `harmonik handler <verb> [flags]`.
// subArgs is os.Args[2:] (everything after "handler").
func runHandlerSubcommand(subArgs []string) int {
	return runHandlerSubcommandIO(subArgs, os.Stdout, os.Stderr)
}

// runHandlerSubcommandIO is the testable variant that accepts explicit writers.
func runHandlerSubcommandIO(subArgs []string, out io.Writer, errOut io.Writer) int {
	if len(subArgs) == 0 {
		fmt.Fprintln(errOut, "harmonik handler: missing verb")
		fmt.Fprintln(errOut, "usage: harmonik handler <verb> [flags]")
		fmt.Fprintln(errOut, "  status  [--type <agent-type>] [--format json|text] [--project DIR]")
		fmt.Fprintln(errOut, "  resume  --type <agent-type> [--force] [--project DIR]")
		return 1
	}

	verb := subArgs[0]
	switch verb {
	case "status":
		return runHandlerStatus(subArgs[1:], out, errOut)
	case "resume":
		return runHandlerResume(subArgs[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "harmonik handler: unrecognised verb %q; supported verbs: status, resume\n", verb)
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
type handlerResumedEvent struct {
	EventType   string            `json:"event_type"`
	EmittedAt   string            `json:"emitted_at"`
	AgentType   string            `json:"agent_type"`
	By          string            `json:"by"`
	PriorCause  *handlerCauseDisk `json:"prior_cause"`
	PausedEpoch int               `json:"paused_epoch"`
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
			fmt.Fprintf(errOut, "harmonik handler resume: unknown flag %q\n", subArgs[i])
			return 1
		default:
			fmt.Fprintf(errOut, "harmonik handler resume: unexpected argument %q\n", subArgs[i])
			return 1
		}
	}

	if typeFlag == "" {
		fmt.Fprintln(errOut, "harmonik handler resume: --type is required")
		return 1
	}

	// --- Resolve project directory ---

	if projectDirFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(errOut, "harmonik handler resume: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDirFlag = wd
	}
	projectDir, err := filepath.Abs(projectDirFlag)
	if err != nil {
		fmt.Fprintf(errOut, "harmonik handler resume: cannot resolve project path %q: %v\n", projectDirFlag, err)
		return 1
	}

	// --- Read handler-state.json ---

	statePath := filepath.Join(projectDir, ".harmonik", handlerStateFile)
	// TOCTOU note: reading statePath here and renaming over it below creates a
	// window in which a concurrent writer (e.g., a future daemon socket handler)
	// could overwrite the file between our read and our rename, silently losing
	// their update. This is acceptable for MVH because only the operator runs
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
		fmt.Fprintf(errOut, "harmonik handler resume: handler type %q not found in handler-state.json (never paused)\n", typeFlag)
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
			fmt.Fprintf(out, "handler %q is already live (--force: no-op)\n", typeFlag)
			return 0
		}
		fmt.Fprintf(errOut, "harmonik handler resume: handler %q is already live (status=%s); use --force to no-op\n", typeFlag, currentStatus)
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

	if writeErr := atomicWriteHandlerState(statePath, state, errOut); writeErr != 0 {
		return writeErr
	}

	// --- Emit handler_resumed event to events.jsonl (event-model §8.11.2) ---
	// Best-effort: event emission failure does not roll back the state update.
	// The state file is authoritative; the event log is observational.

	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	emitHandlerResumedEvent(eventsPath, typeFlag, priorCause, priorEpoch)

	// --- Print confirmation ---

	fmt.Fprintf(out, "handler %q resumed\n", typeFlag)
	if priorCause != nil {
		fmt.Fprintf(out, "  prior cause:\n")
		fmt.Fprintf(out, "    failure_class: %s\n", priorCause.FailureClass)
		fmt.Fprintf(out, "    sub_reason:    %s\n", priorCause.SubReason)
		fmt.Fprintf(out, "    source_bead:   %s\n", priorCause.SourceBeadID)
		fmt.Fprintf(out, "    source_run:    %s\n", priorCause.SourceRunID)
		if t, parseErr := time.Parse(time.RFC3339Nano, priorCause.TrippedAt); parseErr == nil {
			fmt.Fprintf(out, "    tripped_at:    %s\n", t.Format(time.RFC3339))
		} else {
			fmt.Fprintf(out, "    tripped_at:    %s\n", priorCause.TrippedAt)
		}
	}
	fmt.Fprintf(out, "  in_flight_at_pause: %d\n", inFlightCount)
	// Wiring site (hk-9hwbw / hk-m0k0a): once HandlerPauseController lands,
	// query the dispatcher backlog via socket and print held count here.
	fmt.Fprintf(out, "  dispatcher_backlog_held: (unavailable at MVH — HandlerPauseController not yet wired)\n")

	return 0
}

// atomicWriteHandlerState writes state to statePath using WM-026 atomic discipline:
// write to a temp file, fsync, rename over the target, fsync the parent directory.
// Returns 0 on success, 1 on any I/O error.
func atomicWriteHandlerState(statePath string, state *handlerStateDisk, errOut io.Writer) int {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		fmt.Fprintf(errOut, "harmonik handler resume: cannot serialise handler-state.json: %v\n", err)
		return 1
	}
	data = append(data, '\n')

	dir := filepath.Dir(statePath)
	tmpFile, err := os.CreateTemp(dir, ".handler-state-tmp-")
	if err != nil {
		fmt.Fprintf(errOut, "harmonik handler resume: cannot create temp file in %s: %v\n", dir, err)
		return 1
	}
	tmpPath := tmpFile.Name()

	// Write content.
	if _, writeErr := tmpFile.Write(data); writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		fmt.Fprintf(errOut, "harmonik handler resume: cannot write temp file %s: %v\n", tmpPath, writeErr)
		return 1
	}

	// fsync the temp file before rename.
	if syncErr := tmpFile.Sync(); syncErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		fmt.Fprintf(errOut, "harmonik handler resume: fsync %s: %v\n", tmpPath, syncErr)
		return 1
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		_ = os.Remove(tmpPath)
		fmt.Fprintf(errOut, "harmonik handler resume: close %s: %v\n", tmpPath, closeErr)
		return 1
	}

	// Rename (atomic on POSIX). This is the other end of the TOCTOU window noted
	// at the loadHandlerState call above: a concurrent writer that read the file
	// before this rename completes will have its update silently lost when this
	// rename lands. Acceptable at MVH (single operator, no daemon writer). Resolved
	// by hk-9hwbw daemon-socket delegation.
	if renameErr := os.Rename(tmpPath, statePath); renameErr != nil {
		_ = os.Remove(tmpPath)
		fmt.Fprintf(errOut, "harmonik handler resume: rename %s → %s: %v\n", tmpPath, statePath, renameErr)
		return 1
	}

	// fsync the parent directory to flush the directory entry.
	dirF, err := os.Open(dir)
	if err == nil {
		_ = dirF.Sync()
		_ = dirF.Close()
	}

	return 0
}

// emitHandlerResumedEvent appends a handler_resumed event line to eventsPath.
// Best-effort: errors are silently discarded per §8.11 (state file is authoritative).
//
// Before emitting, the function constructs a core.HandlerResumedPayload and calls
// .Valid() on it (event-model §8.11.2 payload contract). If the payload is invalid
// (e.g. PausedEpoch < 1 or cause fields empty), the emit is skipped and a warning
// is printed to stderr. This prevents replay tooling from ingesting a malformed
// handler_resumed event that would fail schema validation.
func emitHandlerResumedEvent(eventsPath, agentType string, priorCause *handlerCauseDisk, pausedEpoch int) {
	// Build a typed HandlerResumedPayload and validate before emitting.
	// This guards against payload-shape drift between the CLI's flat envelope and
	// the core schema (event-model §8.11.2 / handlerpauseevents_ifqnj.go).
	var typedPriorCause core.HandlerPauseCause
	if priorCause != nil {
		typedPriorCause = core.HandlerPauseCause{
			FailureClass: core.FailureClass(priorCause.FailureClass),
			SubReason:    priorCause.SubReason,
			SourceRunID:  priorCause.SourceRunID,
			SourceBeadID: priorCause.SourceBeadID,
			TrippedAt:    priorCause.TrippedAt,
		}
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
		// from handler-state.json. A warning is written to stderr (best-effort).
		_, _ = fmt.Fprintf(os.Stderr, //nolint:errcheck // best-effort
			"harmonik handler resume: warning: skipping handler_resumed event emit — payload invalid (paused_epoch=%d agent_type=%q); state file updated successfully\n",
			pausedEpoch, agentType)
		return
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
		return
	}
	line = append(line, '\n')

	// Ensure the events directory exists (best-effort; daemon may not have run).
	_ = os.MkdirAll(filepath.Dir(eventsPath), 0o755)

	f, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644) //nolint:gosec // G304: operator-controlled project dir
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // best-effort
	_, _ = f.Write(line)             //nolint:errcheck // best-effort
}

// runHandlerStatus implements `harmonik handler status`.
func runHandlerStatus(subArgs []string, out io.Writer, errOut io.Writer) int {
	// --- Parse flags ---

	typeFlag := ""
	formatFlag := "text"
	projectDirFlag := ""

	for i := 0; i < len(subArgs); i++ {
		switch {
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
			fmt.Fprintf(errOut, "harmonik handler status: unknown flag %q\n", subArgs[i])
			return 1
		default:
			fmt.Fprintf(errOut, "harmonik handler status: unexpected argument %q\n", subArgs[i])
			return 1
		}
	}

	if formatFlag != "json" && formatFlag != "text" {
		fmt.Fprintf(errOut, "harmonik handler status: --format must be json or text (got %q)\n", formatFlag)
		return 1
	}

	// --- Resolve project directory ---

	if projectDirFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(errOut, "harmonik handler status: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDirFlag = wd
	}
	projectDir, err := filepath.Abs(projectDirFlag)
	if err != nil {
		fmt.Fprintf(errOut, "harmonik handler status: cannot resolve project path %q: %v\n", projectDirFlag, err)
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
func loadHandlerState(statePath string, errOut io.Writer) (*handlerStateDisk, int) {
	data, err := os.ReadFile(statePath) //nolint:gosec // G304: operator-controlled project dir
	if err != nil {
		if os.IsNotExist(err) {
			// File absent → no handlers have ever been paused; return empty state.
			return &handlerStateDisk{
				SchemaVersion: handlerStateSchemaVersion,
				Handlers:      map[string]handlerEntryDisk{},
			}, 0
		}
		fmt.Fprintf(errOut, "harmonik handler status: cannot read %s: %v\n", statePath, err)
		return nil, 1
	}

	var state handlerStateDisk
	if jsonErr := json.Unmarshal(data, &state); jsonErr != nil {
		fmt.Fprintf(errOut, "harmonik handler status: cannot parse %s: %v\n", statePath, jsonErr)
		return nil, 1
	}

	// Schema-version guard: mirrors QM-002 forward-incompatible handling.
	if state.SchemaVersion > handlerStateSchemaVersion {
		fmt.Fprintf(errOut,
			"harmonik handler status: %s schema_version %d is newer than this binary supports (%d); upgrade harmonik\n",
			statePath, state.SchemaVersion, handlerStateSchemaVersion)
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
func renderJSON(state *handlerStateDisk, out io.Writer, errOut io.Writer) int {
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
			HeldCount:       0, // derived; see struct comment — always 0 at MVH CLI level
		}
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(errOut, "harmonik handler status: cannot encode JSON: %v\n", err)
		return 1
	}
	return 0
}

// renderText writes human-readable status to out.
func renderText(state *handlerStateDisk, typeFilter string, out io.Writer) int {
	if len(state.Handlers) == 0 {
		if typeFilter != "" {
			fmt.Fprintf(out, "handler %q: live (no pause record)\n", typeFilter)
		} else {
			fmt.Fprintln(out, "no handler-pause records (all handlers live)")
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
		printHandlerTextEntry(out, agentType, entry)
	}
	return 0
}

// printHandlerTextEntry renders one handler entry in human-readable form.
func printHandlerTextEntry(out io.Writer, agentType string, entry handlerEntryDisk) {
	status := entry.Status
	if status == "" {
		status = "live"
	}

	fmt.Fprintf(out, "handler: %s\n", agentType)
	fmt.Fprintf(out, "  status: %s\n", status)

	if status == "paused" && entry.Cause != nil {
		c := entry.Cause
		fmt.Fprintf(out, "  cause:\n")
		fmt.Fprintf(out, "    failure_class: %s\n", c.FailureClass)
		fmt.Fprintf(out, "    sub_reason:    %s\n", c.SubReason)
		fmt.Fprintf(out, "    source_bead:   %s\n", c.SourceBeadID)
		fmt.Fprintf(out, "    source_run:    %s\n", c.SourceRunID)
		if t, err := time.Parse(time.RFC3339Nano, c.TrippedAt); err == nil {
			fmt.Fprintf(out, "    tripped_at:    %s\n", t.Format(time.RFC3339))
		} else {
			fmt.Fprintf(out, "    tripped_at:    %s\n", c.TrippedAt)
		}
		fmt.Fprintf(out, "  paused_epoch: %d\n", entry.PausedEpoch)

		if len(entry.InFlightAtPause) > 0 {
			fmt.Fprintf(out, "  in_flight_at_pause (%d):\n", len(entry.InFlightAtPause))
			for _, r := range entry.InFlightAtPause {
				fmt.Fprintf(out, "    - bead %s (run %s)\n", r.BeadID, r.RunID)
			}
		} else {
			fmt.Fprintf(out, "  in_flight_at_pause: (none)\n")
		}
	}
}
