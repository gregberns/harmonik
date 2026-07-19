package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// InterruptStateClearCause is the reason driving an interrupt_state → none
// transition. WM-040 requires that any clearing of interrupt_state back to
// none MUST be driven by one of the two declared causes; silent clears are
// forbidden.
//
// Spec ref: workspace-model.md §4.10 WM-040 — "Clearing interrupt_state back
// to none MUST be driven by either (a) an operator_resuming event … or (b) a
// reconciliation verdict."
type InterruptStateClearCause string

const (
	// InterruptStateClearCauseOperatorResuming is the cause for an
	// interrupt_state → none transition driven by an operator_resuming event
	// per operator-nfr.md §4.3 ON-013.
	//
	// Spec ref: workspace-model.md §4.10 WM-040 clause (a).
	InterruptStateClearCauseOperatorResuming InterruptStateClearCause = "operator_resuming"

	// InterruptStateClearCauseReconciliationVerdict is the cause for an
	// interrupt_state → none transition driven by a reconciliation verdict per
	// reconciliation/spec.md §4.5 (e.g., no-op-accept on a daemon-crash run).
	//
	// Spec ref: workspace-model.md §4.10 WM-040 clause (b).
	InterruptStateClearCauseReconciliationVerdict InterruptStateClearCause = "reconciliation_verdict"
)

// ErrInterruptStateClearRequiresCause is returned by SetInterruptStateToNone
// when the caller passes an empty or unrecognised cause. WM-040 forbids silent
// clears.
//
// Spec ref: workspace-model.md §4.10 WM-040 — "The workspace manager MUST NOT
// silently clear the field."
var ErrInterruptStateClearRequiresCause = errors.New(
	"workspace: interrupt_state reset to none requires a declared cause (WM-040)",
)

// SetInterruptStateToNone clears ws.InterruptState to core.InterruptStateNone
// and appends a durable interrupt_state_changed JSONL marker to the
// workspace-local events file per WM-038a.
//
// The caller MUST supply a non-empty cause matching one of the two WM-040
// clauses:
//
//   - InterruptStateClearCauseOperatorResuming  — operator_resuming event
//   - InterruptStateClearCauseReconciliationVerdict — reconciliation verdict
//
// Returns ErrInterruptStateClearRequiresCause if cause is unrecognised.
// Returns an I/O error if the marker write or fsync fails; in that case
// ws.InterruptState is NOT mutated (the operation is aborted).
//
// workspacePath is the absolute path of the workspace's worktree directory
// (the ws.Path field). runID and workspaceID are the corresponding workspace
// identifiers. The prior interrupt_state value is captured before mutation.
//
// Spec ref: workspace-model.md §4.10 WM-040 — "Clearing interrupt_state back
// to none MUST be driven by either (a) an operator_resuming event … or (b) a
// reconciliation verdict."
//
// Spec ref: workspace-model.md §4.10 WM-038a — "workspace manager MUST on
// every interrupt_state mutation … append a single workspace-scoped JSONL line
// … and fsync."
func SetInterruptStateToNone(
	ws *Workspace,
	workspacePath, runID string,
	cause InterruptStateClearCause,
) error {
	switch cause {
	case InterruptStateClearCauseOperatorResuming, InterruptStateClearCauseReconciliationVerdict:
		// valid causes — proceed
	default:
		return fmt.Errorf("%w: got %q", ErrInterruptStateClearRequiresCause, cause)
	}

	prior := ws.InterruptState

	// Write the durable marker first (durability-before-mutation discipline).
	// If the marker write fails, the field is not mutated.
	if err := WriteInterruptStateChangedMarker(
		workspacePath,
		ws.WorkspaceID,
		runID,
		string(prior),
		string(core.InterruptStateNone),
		string(cause),
	); err != nil {
		return err
	}

	ws.InterruptState = core.InterruptStateNone
	return nil
}

// WriteInterruptStateChangedMarker appends an interrupt_state_changed JSONL
// line to the workspace-local events file and fsyncs it before returning.
//
// Marker shape per workspace-model.md §4.10 WM-038a:
//
//	{
//	  "event":               "interrupt_state_changed",
//	  "workspace_id":        "<ws_id>",
//	  "run_id":              "<run_id>",
//	  "prior_interrupt_state":"<enum>",
//	  "new_interrupt_state": "<enum>",
//	  "cause":               "<operator-event-type | verdict-kind>",
//	  "changed_at":          "<rfc3339>"
//	}
//
// The file is created if it does not exist. All fields are required. The
// function fsyncs both the file and its parent directory for durability.
//
// Spec ref: workspace-model.md §4.10 WM-038a — "The workspace-local marker is
// the authoritative record of the transition for reconciliation's consumer pass."
func WriteInterruptStateChangedMarker(
	workspacePath, workspaceID, runID, priorInterruptState, newInterruptState, cause string,
) error {
	eventsPath := WorkspaceLocalEventsPath(workspacePath, workspaceID)
	eventsDir := filepath.Dir(eventsPath)

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		return fmt.Errorf("workspace: WriteInterruptStateChangedMarker: MkdirAll %q: %w", eventsDir, err)
	}

	payload, err := json.Marshal(struct {
		Event               string `json:"event"`
		WorkspaceID         string `json:"workspace_id"`
		RunID               string `json:"run_id"`
		PriorInterruptState string `json:"prior_interrupt_state"`
		NewInterruptState   string `json:"new_interrupt_state"`
		Cause               string `json:"cause"`
		ChangedAt           string `json:"changed_at"`
	}{
		Event:               "interrupt_state_changed",
		WorkspaceID:         workspaceID,
		RunID:               runID,
		PriorInterruptState: priorInterruptState,
		NewInterruptState:   newInterruptState,
		Cause:               cause,
		ChangedAt:           time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("workspace: WriteInterruptStateChangedMarker: marshal: %w", err)
	}
	line := string(payload) + "\n"

	//nolint:gosec // G304: path constructed from workspace_path (lease-acquired) + known relative segments; not user input
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("workspace: WriteInterruptStateChangedMarker: open %q: %w", eventsPath, err)
	}

	_, writeErr := f.WriteString(line)
	syncErr := f.Sync()
	closeErr := f.Close()

	if writeErr != nil {
		return fmt.Errorf("workspace: WriteInterruptStateChangedMarker: write: %w", writeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("workspace: WriteInterruptStateChangedMarker: fsync file: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("workspace: WriteInterruptStateChangedMarker: close: %w", closeErr)
	}

	// fsync the parent directory for unlink durability.
	//nolint:gosec // G304: eventsDir is derived from workspacePath + .harmonik/events, not user input
	dirFd, err := os.Open(eventsDir)
	if err != nil {
		// Non-fatal: file content is already fsynced; dir fsync is best-effort.
		return nil
	}
	_ = dirFd.Sync()  //nolint:errcheck // dir fsync failure is non-fatal
	_ = dirFd.Close() //nolint:errcheck // cleanup error unactionable
	return nil
}
