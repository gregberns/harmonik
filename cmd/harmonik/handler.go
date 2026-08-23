package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

const handlerStateSchemaVersion = 1

const handlerSubsystemID = "github.com/gregberns/harmonik/cmd/harmonik"

func init() {
	if err := core.RegisterSourceSubsystem(handlerSubsystemID); err != nil {
		log.Fatalf("harmonik: RegisterSourceSubsystem: %v", err)
	}
}

const handlerStateFile = "handler-state.json"

type handlerStateDisk struct {
	SchemaVersion int                         `json:"schema_version"`
	Handlers      map[string]handlerEntryDisk `json:"handlers"`
}

type handlerEntryDisk struct {
	Status          string                  `json:"status"`
	Cause           *core.HandlerPauseCause `json:"cause"`
	InFlightAtPause []inFlightRunDisk       `json:"in_flight_at_pause"`
	PausedEpoch     int                     `json:"paused_epoch"`
}

type inFlightRunDisk struct {
	RunID        string `json:"run_id"`
	BeadID       string `json:"bead_id"`
	DispatchedAt string `json:"dispatched_at"`
}

type handlerStatusJSONOutput struct {
	SchemaVersion int                         `json:"schema_version"`
	Handlers      map[string]handlerEntryJSON `json:"handlers"`
}

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

EXIT CODES
  0   Success. Also 0 for this help.
  1   No verb, a verb this command does not have, a bad argument, or a file
      that could not be read, parsed or written.
  2   status: the state file records a schema version this binary is too old
      to read.
      resume: handler-state.json holds no record for that handler type.
  3   resume: the handler is already live. Add --force to make that a no-op.
`); err != nil {
		return fmt.Errorf("print handler usage: %w", err)
	}
	return nil
}

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

func runHandlerSubcommand(subArgs []string) int {
	return runHandlerSubcommandIO(subArgs, os.Stdout, os.Stderr)
}

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

const resumeExitUnknownType = 2

const resumeExitAlreadyLive = 3

func runHandlerResume(subArgs []string, out io.Writer, errOut io.Writer) int {
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

	statePath := filepath.Join(projectDir, ".harmonik", handlerStateFile)
	state, exitCode := loadHandlerState(statePath, errOut)
	if exitCode != 0 {
		return exitCode
	}

	entry, known := state.Handlers[typeFlag]
	if !known {
		if _, err := fmt.Fprintf(errOut, "harmonik handler resume: handler type %q not found in handler-state.json (never paused)\n", typeFlag); err != nil {
			return 1
		}
		return resumeExitUnknownType
	}

	currentStatus := entry.Status
	if currentStatus == "" {
		currentStatus = "live"
	}
	if currentStatus != "paused" {
		if forceFlag {
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

	priorCause := entry.Cause
	priorEpoch := entry.PausedEpoch
	inFlightCount := len(entry.InFlightAtPause)

	state.Handlers[typeFlag] = handlerEntryDisk{
		Status:          "live",
		Cause:           nil,
		InFlightAtPause: []inFlightRunDisk{},
		PausedEpoch:     priorEpoch, // epoch preserved; HandlerPauseController will increment on next pause
	}

	if writeErr := atomicWriteHandlerState(statePath, state); writeErr != nil {
		if err := handlerWritef(errOut, "harmonik handler resume: %v\n", writeErr); err != nil {
			return 1
		}
		return 1
	}

	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	if emitErr := emitHandlerResumedEvent(eventsPath, typeFlag, priorCause, priorEpoch); emitErr != nil {
		handlerBestEffortWarning(errOut, "harmonik handler resume: warning: handler_resumed event not recorded: %v\n", emitErr)
	}

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

	if _, writeErr := tmpFile.Write(data); writeErr != nil {
		return fmt.Errorf("write temp file %s: %w", tmpPath, errors.Join(writeErr, cleanupHandlerStateTemp(tmpFile, tmpPath)))
	}

	if syncErr := tmpFile.Sync(); syncErr != nil {
		return fmt.Errorf("fsync %s: %w", tmpPath, errors.Join(syncErr, cleanupHandlerStateTemp(tmpFile, tmpPath)))
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		return fmt.Errorf("close %s: %w", tmpPath, errors.Join(closeErr, cleanupHandlerStateTemp(nil, tmpPath)))
	}

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

func emitHandlerResumedEvent(eventsPath, agentType string, priorCause *core.HandlerPauseCause, pausedEpoch int) error {
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
		return fmt.Errorf("payload invalid (paused_epoch=%d agent_type=%q)", pausedEpoch, agentType)
	}

	payload, err := json.Marshal(typedPayload)
	if err != nil {
		return fmt.Errorf("marshal handler_resumed payload: %w", err)
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("new handler_resumed event ID: %w", err)
	}
	event := core.Event{
		EventID:         core.EventID(eventID),
		SchemaVersion:   1,
		Type:            core.EventTypeHandlerResumed,
		TimestampWall:   time.Now().UTC(),
		SourceSubsystem: handlerSubsystemID,
		Payload:         payload,
	}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal handler_resumed event: %w", err)
	}
	line = append(line, '\n')

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

func runHandlerStatus(subArgs []string, out io.Writer, errOut io.Writer) int {
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

	statePath := filepath.Join(projectDir, ".harmonik", handlerStateFile)
	state, exitCode := loadHandlerState(statePath, errOut)
	if exitCode != 0 {
		return exitCode
	}

	if typeFlag != "" {
		entry, ok := state.Handlers[typeFlag]
		if !ok {
			entry = handlerEntryDisk{
				Status:          "live",
				Cause:           nil,
				InFlightAtPause: []inFlightRunDisk{},
				PausedEpoch:     0,
			}
		}
		state.Handlers = map[string]handlerEntryDisk{typeFlag: entry}
	}

	if formatFlag == "json" {
		return renderJSON(state, out, errOut)
	}
	return renderText(state, typeFlag, out)
}

func loadHandlerState(statePath string, errOut io.Writer) (result *handlerStateDisk, exitCode int) {
	data, err := os.ReadFile(statePath) //nolint:gosec // G304: operator-controlled project dir
	if err != nil {
		if os.IsNotExist(err) {
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
