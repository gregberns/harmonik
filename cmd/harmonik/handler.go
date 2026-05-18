// handler.go — `harmonik handler status` subcommand implementation.
//
// Semantics (hk-39ryh):
//  1. Parse --type and --format flags.
//  2. Resolve the project directory.
//  3. Read .harmonik/handler-state.json (absent → all handlers live; show empty).
//  4. If --type is given, filter to that handler only.
//  5. Output JSON (--format json) or human-readable text (default).
//
// This is a read-only CLI that reads the on-disk handler-state.json directly.
// It does NOT connect to the daemon socket (no daemon required). The file is
// written by hk-m0k0a (HandlerPauseController persistence); for now the file
// may be absent, in which case all handlers are treated as live.
//
// Exit-code contract:
//
//	0  — success (output written)
//	1  — argument or file-parse error
//	2  — forward-incompatible schema version
//
// Spec ref: docs/components/internal/handler-pause-and-resume.md §8.2 §8.3.
// Bead ref: hk-39ryh.

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
		fmt.Fprintln(errOut, "usage: harmonik handler status [--type <agent-type>] [--format json|text] [--project DIR]")
		return 1
	}

	verb := subArgs[0]
	switch verb {
	case "status":
		return runHandlerStatus(subArgs[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "harmonik handler: unrecognised verb %q; supported verbs: status\n", verb)
		return 1
	}
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
