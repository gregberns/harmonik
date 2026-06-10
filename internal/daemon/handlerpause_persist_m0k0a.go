package daemon

// handlerpause_persist_m0k0a.go — Handler-pause persistence layer (hk-m0k0a).
//
// Implements the .harmonik/handler-state.json read/write layer used by
// HandlerPauseController.  Three public entry points:
//
//   - MakeHandlerPausePersistFn(stateDir) returns a persistFn closure for
//     injection into NewHandlerPauseController.  Each call atomically writes
//     the full handler state to handler-state.json per WM-026.
//
//   - LoadHandlerPauseState(ctx, stateDir, ctrl) reads handler-state.json at
//     daemon startup and seeds ctrl with any persisted paused handlers.
//     File absent → all handlers default live.
//     Forward-incompatible schema → ErrHandlerStateSchemaUnsupported (caller
//     treats this as a fatal startup error, exit code 2).
//
// On-disk schema (handler-state.json):
//
//	{
//	  "schema_version": 1,
//	  "handlers": {
//	    "claude-code": {
//	      "status": "paused",
//	      "cause": { ... },
//	      "in_flight_at_pause": [ ... ],
//	      "paused_epoch": 1
//	    }
//	  }
//	}
//
// Schema is intentionally isomorphic to the shapes defined in
// cmd/harmonik/handler.go (handlerStateDisk / handlerEntryDisk).  The CLI and
// daemon share the same file; CLI reads it for `handler status`, daemon writes
// it on Pause/Resume.
//
// Atomic-write discipline (WM-026): CreateTemp → Write → Sync → Close →
// Rename → parent-dir Sync.  Same sequence used by cmd/harmonik/handler.go
// atomicWriteHandlerState.
//
// Spec ref: specs/handler-pause.md §3.5.
// Bead ref: hk-m0k0a.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
)

// handlerStateSchemaVersionDaemon is the schema version this daemon binary
// reads and writes.  Forward-incompatible versions (schema_version >
// handlerStateSchemaVersionDaemon) cause LoadHandlerPauseState to return
// ErrHandlerStateSchemaUnsupported, which the caller maps to exit code 2.
//
// v2 introduces the per-account sub-map (HP-072 / hk-lhxzc).  A v2 daemon
// reads v1 files via backwards-compat migration: a v1 paused handler is loaded
// as an anonymous account (AnonymousAccountID) within that handler type.
//
// Matches handlerStateSchemaVersion in cmd/harmonik/handler.go.
const handlerStateSchemaVersionDaemon = 2

// handlerStateFileName is the on-disk filename.
// Sibling to queue.json inside <ProjectDir>/.harmonik/.
const handlerStateFileName = "handler-state.json"

// ---------------------------------------------------------------------------
// On-disk schema types (daemon-side mirror of cmd/harmonik/handler.go shapes)
// ---------------------------------------------------------------------------

// handlerStateDiskDaemon is the top-level on-disk JSON structure.
type handlerStateDiskDaemon struct {
	SchemaVersion int                               `json:"schema_version"`
	Handlers      map[string]handlerEntryDiskDaemon `json:"handlers"`
}

// handlerEntryDiskDaemon is one handler-type entry in handler-state.json.
type handlerEntryDiskDaemon struct {
	Status          string                              `json:"status"`
	Cause           *handlerCauseDiskDaemon             `json:"cause"`
	InFlightAtPause []inFlightRunDiskDaemon             `json:"in_flight_at_pause"`
	PausedEpoch     int                                 `json:"paused_epoch"`
	Accounts        map[string]handlerAccountDiskDaemon `json:"accounts,omitempty"` // v2+
}

// handlerAccountDiskDaemon is a per-account entry inside handlers.<type>.accounts
// introduced in schema v2 (HP-072 / hk-lhxzc).
type handlerAccountDiskDaemon struct {
	Status          string                  `json:"status"`
	Cause           *handlerCauseDiskDaemon `json:"cause"`
	InFlightAtPause []inFlightRunDiskDaemon `json:"in_flight_at_pause"`
	PausedEpoch     int                     `json:"paused_epoch"`
}

// handlerCauseDiskDaemon is the cause sub-object inside a paused handler entry.
type handlerCauseDiskDaemon struct {
	FailureClass string `json:"failure_class"`
	SubReason    string `json:"sub_reason"`
	SourceRunID  string `json:"source_run_id"`
	SourceBeadID string `json:"source_bead_id"`
	TrippedAt    string `json:"tripped_at"`
}

// inFlightRunDiskDaemon is a single entry in in_flight_at_pause.
type inFlightRunDiskDaemon struct {
	RunID        string `json:"run_id"`
	BeadID       string `json:"bead_id"`
	DispatchedAt string `json:"dispatched_at"`
}

// ---------------------------------------------------------------------------
// ErrHandlerStateSchemaUnsupported
// ---------------------------------------------------------------------------

// ErrHandlerStateSchemaUnsupported is returned when the on-disk schema_version
// is newer than this binary supports.  The caller (daemon.Start) should treat
// this as a fatal startup error and exit with code 2, mirroring QM-002.
//
// Bead ref: hk-m0k0a.
type ErrHandlerStateSchemaUnsupported struct {
	// Path is the file that triggered the error.
	Path string
	// Got is the schema_version found in the file.
	Got int
	// Max is the highest schema_version this binary supports.
	Max int
}

// Error implements the error interface.
func (e *ErrHandlerStateSchemaUnsupported) Error() string {
	return fmt.Sprintf(
		"handler-state.json at %q has schema_version %d which is newer than this binary supports (%d); upgrade harmonik",
		e.Path, e.Got, e.Max,
	)
}

// IsErrHandlerStateSchemaUnsupported reports whether err wraps
// *ErrHandlerStateSchemaUnsupported.
func IsErrHandlerStateSchemaUnsupported(err error) bool {
	var e *ErrHandlerStateSchemaUnsupported
	return errors.As(err, &e)
}

// ---------------------------------------------------------------------------
// MakeHandlerPausePersistFn — closure factory
// ---------------------------------------------------------------------------

// MakeHandlerPausePersistFn returns a persistFn closure for injection into
// NewHandlerPauseController.
//
// stateDir is the .harmonik/ directory path (e.g. <ProjectDir>/.harmonik).
// The file is written to <stateDir>/handler-state.json using WM-026
// atomic-write discipline.
//
// The returned function serialises the supplied snapshots and writes them
// atomically.  It is called by HandlerPauseController.Pause and .Resume under
// the controller's mu lock.
//
// Bead ref: hk-m0k0a.
func MakeHandlerPausePersistFn(stateDir string) func(ctx context.Context, snapshots []HandlerPauseStatusSnapshot) error {
	statePath := filepath.Join(stateDir, handlerStateFileName)
	return func(_ context.Context, snapshots []HandlerPauseStatusSnapshot) error {
		return atomicWriteHandlerStateDaemon(statePath, snapshots)
	}
}

// ---------------------------------------------------------------------------
// atomicWriteHandlerStateDaemon — WM-026 atomic write
// ---------------------------------------------------------------------------

// atomicWriteHandlerStateDaemon serialises snapshots to handler-state.json
// using WM-026 discipline:
//
//  1. CreateTemp in the same directory as the target file.
//  2. Write JSON bytes.
//  3. fsync the temp file.
//  4. Close the temp file.
//  5. Rename temp → target (atomic on POSIX).
//  6. fsync the parent directory.
//
// Bead ref: hk-m0k0a, WM-026.
func atomicWriteHandlerStateDaemon(statePath string, snapshots []HandlerPauseStatusSnapshot) error {
	disk := snapshotsToDisk(snapshots)

	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("atomicWriteHandlerStateDaemon: marshal: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(statePath)

	// Step 1: create temp file in the same directory so that rename is atomic.
	tmp, err := os.CreateTemp(dir, ".handler-state-tmp-")
	if err != nil {
		return fmt.Errorf("atomicWriteHandlerStateDaemon: CreateTemp in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()

	// Steps 2–4: write, fsync, close.
	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteHandlerStateDaemon: write %s: %w", tmpPath, writeErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteHandlerStateDaemon: fsync %s: %w", tmpPath, syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteHandlerStateDaemon: close %s: %w", tmpPath, closeErr)
	}

	// Step 5: atomic rename.
	if renameErr := os.Rename(tmpPath, statePath); renameErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomicWriteHandlerStateDaemon: rename %s → %s: %w", tmpPath, statePath, renameErr)
	}

	// Step 6: fsync the parent directory to flush the new directory entry.
	if dirF, openErr := os.Open(dir); openErr == nil {
		_ = dirF.Sync()
		_ = dirF.Close()
	}

	return nil
}

// ---------------------------------------------------------------------------
// LoadHandlerPauseState — startup read
// ---------------------------------------------------------------------------

// LoadHandlerPauseState reads <stateDir>/handler-state.json at daemon startup
// and seeds ctrl with any persisted paused handlers.
//
// Behaviour:
//   - File absent → no-op (all handlers default live per §5.3).
//   - File unparseable → returns an error (caller should fail-fast).
//   - schema_version > handlerStateSchemaVersionDaemon → returns
//     *ErrHandlerStateSchemaUnsupported; caller maps to exit code 2.
//   - Paused handlers → Pause is called on ctrl to restore their state.
//   - Live (status != "paused") handlers → skipped; absent = live.
//
// Spec ref: specs/handler-pause.md §8.2 HP-007.
// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
// Bead ref: hk-m0k0a.
func LoadHandlerPauseState(ctx context.Context, stateDir string, ctrl *HandlerPauseController) error {
	statePath := filepath.Join(stateDir, handlerStateFileName)

	data, err := os.ReadFile(statePath) //nolint:gosec // G304: operator-controlled project dir
	if err != nil {
		if os.IsNotExist(err) {
			// File absent → all handlers default live; no-op.
			return nil
		}
		return fmt.Errorf("LoadHandlerPauseState: read %s: %w", statePath, err)
	}

	var disk handlerStateDiskDaemon
	if jsonErr := json.Unmarshal(data, &disk); jsonErr != nil {
		return fmt.Errorf("LoadHandlerPauseState: parse %s: %w", statePath, jsonErr)
	}

	// Schema-version guard (mirrors QM-002 forward-incompatible handling).
	if disk.SchemaVersion > handlerStateSchemaVersionDaemon {
		return &ErrHandlerStateSchemaUnsupported{
			Path: statePath,
			Got:  disk.SchemaVersion,
			Max:  handlerStateSchemaVersionDaemon,
		}
	}

	// Seed the controller with persisted paused handlers.
	for agentTypeStr, entry := range disk.Handlers {
		agentType := core.AgentType(agentTypeStr)
		if !agentType.Valid() {
			// Unknown agent type in file; skip silently.
			continue
		}

		// Restore handler-level pause (present in both v1 and v2).
		if entry.Status == "paused" && entry.Cause != nil {
			cause := diskCauseToCore(entry.Cause)
			if cause.Valid() {
				inFlight := diskInFlightToCore(entry.InFlightAtPause)
				// Call Pause to restore persisted handler-level state.
				if pauseErr := ctrl.Pause(ctx, agentType, cause, inFlight); pauseErr != nil {
					return fmt.Errorf("LoadHandlerPauseState: restore pause for %q: %w", agentTypeStr, pauseErr)
				}
				// NOTE on paused_epoch: same as before — restores epoch=1, not the exact value.
				_ = entry.PausedEpoch
			}
		}

		// v1 backwards compat (HP-072): if schema_version == 1 and the handler is
		// paused, also restore the state as the anonymous account so callers using
		// IsAccountPaused("", ...) observe the same pause.
		if disk.SchemaVersion == 1 && entry.Status == "paused" && entry.Cause != nil {
			cause := diskCauseToCore(entry.Cause)
			if cause.Valid() {
				inFlight := diskInFlightToCore(entry.InFlightAtPause)
				if pauseErr := ctrl.PauseAccount(ctx, agentType, AnonymousAccountID, cause, inFlight); pauseErr != nil {
					return fmt.Errorf("LoadHandlerPauseState: restore anonymous account pause for %q: %w", agentTypeStr, pauseErr)
				}
			}
		}

		// Restore per-account pauses (schema v2+).
		for accountIDStr, acct := range entry.Accounts {
			if acct.Status != "paused" || acct.Cause == nil {
				continue
			}
			cause := diskCauseToCore(acct.Cause)
			if !cause.Valid() {
				continue
			}
			inFlight := diskInFlightToCore(acct.InFlightAtPause)
			accountID := AccountID(accountIDStr)
			if pauseErr := ctrl.PauseAccount(ctx, agentType, accountID, cause, inFlight); pauseErr != nil {
				return fmt.Errorf("LoadHandlerPauseState: restore account %q pause for handler %q: %w", accountIDStr, agentTypeStr, pauseErr)
			}
			_ = acct.PausedEpoch // not restored exactly; see NOTE above
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

// snapshotsToDisk converts []HandlerPauseStatusSnapshot to handlerStateDiskDaemon.
func snapshotsToDisk(snapshots []HandlerPauseStatusSnapshot) *handlerStateDiskDaemon {
	disk := &handlerStateDiskDaemon{
		SchemaVersion: handlerStateSchemaVersionDaemon,
		Handlers:      make(map[string]handlerEntryDiskDaemon, len(snapshots)),
	}
	for _, s := range snapshots {
		status := "live"
		if s.Paused {
			status = "paused"
		}
		entry := handlerEntryDiskDaemon{
			Status:      status,
			PausedEpoch: s.PausedEpoch,
		}
		if s.Cause != nil {
			entry.Cause = &handlerCauseDiskDaemon{
				FailureClass: string(s.Cause.FailureClass),
				SubReason:    s.Cause.SubReason,
				SourceRunID:  s.Cause.SourceRunID,
				SourceBeadID: s.Cause.SourceBeadID,
				TrippedAt:    s.Cause.TrippedAt,
			}
		}
		if len(s.InFlightAtPause) > 0 {
			entry.InFlightAtPause = make([]inFlightRunDiskDaemon, len(s.InFlightAtPause))
			for i, r := range s.InFlightAtPause {
				entry.InFlightAtPause[i] = inFlightRunDiskDaemon{
					RunID:        r.RunID,
					BeadID:       r.BeadID,
					DispatchedAt: r.DispatchedAt,
				}
			}
		} else {
			entry.InFlightAtPause = []inFlightRunDiskDaemon{}
		}
		// Write per-account state (v2+ schema).
		if len(s.Accounts) > 0 {
			entry.Accounts = make(map[string]handlerAccountDiskDaemon, len(s.Accounts))
			for aid, as := range s.Accounts {
				acctStatus := "live"
				if as.Paused {
					acctStatus = "paused"
				}
				adisk := handlerAccountDiskDaemon{
					Status:      acctStatus,
					PausedEpoch: as.PausedEpoch,
				}
				if as.Cause != nil {
					adisk.Cause = &handlerCauseDiskDaemon{
						FailureClass: string(as.Cause.FailureClass),
						SubReason:    as.Cause.SubReason,
						SourceRunID:  as.Cause.SourceRunID,
						SourceBeadID: as.Cause.SourceBeadID,
						TrippedAt:    as.Cause.TrippedAt,
					}
				}
				if len(as.InFlightAtPause) > 0 {
					adisk.InFlightAtPause = make([]inFlightRunDiskDaemon, len(as.InFlightAtPause))
					for i, r := range as.InFlightAtPause {
						adisk.InFlightAtPause[i] = inFlightRunDiskDaemon{
							RunID:        r.RunID,
							BeadID:       r.BeadID,
							DispatchedAt: r.DispatchedAt,
						}
					}
				} else {
					adisk.InFlightAtPause = []inFlightRunDiskDaemon{}
				}
				entry.Accounts[string(aid)] = adisk
			}
		}
		disk.Handlers[string(s.AgentType)] = entry
	}
	return disk
}

// diskCauseToCore converts a handlerCauseDiskDaemon to core.HandlerPauseCause.
func diskCauseToCore(d *handlerCauseDiskDaemon) core.HandlerPauseCause {
	return core.HandlerPauseCause{
		FailureClass: core.FailureClass(d.FailureClass),
		SubReason:    d.SubReason,
		SourceRunID:  d.SourceRunID,
		SourceBeadID: d.SourceBeadID,
		TrippedAt:    d.TrippedAt,
	}
}

// diskInFlightToCore converts []inFlightRunDiskDaemon to []InFlightBeadRecord.
func diskInFlightToCore(rs []inFlightRunDiskDaemon) []InFlightBeadRecord {
	out := make([]InFlightBeadRecord, 0, len(rs))
	for _, r := range rs {
		out = append(out, InFlightBeadRecord{
			RunID:        r.RunID,
			BeadID:       r.BeadID,
			DispatchedAt: r.DispatchedAt,
		})
	}
	return out
}
