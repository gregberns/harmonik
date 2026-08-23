package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
)

const handlerStateSchemaVersionDaemon = 2

const handlerStateFileName = "handler-state.json"

type handlerStateDiskDaemon struct {
	SchemaVersion int                               `json:"schema_version"`
	Handlers      map[string]handlerEntryDiskDaemon `json:"handlers"`
}

type handlerEntryDiskDaemon struct {
	Status          string                              `json:"status"`
	Cause           *handlerCauseDiskDaemon             `json:"cause"`
	InFlightAtPause []inFlightRunDiskDaemon             `json:"in_flight_at_pause"`
	PausedEpoch     int                                 `json:"paused_epoch"`
	Accounts        map[string]handlerAccountDiskDaemon `json:"accounts,omitempty"` // v2+
}

type handlerAccountDiskDaemon struct {
	Status          string                  `json:"status"`
	Cause           *handlerCauseDiskDaemon `json:"cause"`
	InFlightAtPause []inFlightRunDiskDaemon `json:"in_flight_at_pause"`
	PausedEpoch     int                     `json:"paused_epoch"`
}

type handlerCauseDiskDaemon struct {
	FailureClass string `json:"failure_class"`
	SubReason    string `json:"sub_reason"`
	SourceRunID  string `json:"source_run_id"`
	SourceBeadID string `json:"source_bead_id"`
	TrippedAt    string `json:"tripped_at"`
}

type inFlightRunDiskDaemon struct {
	RunID        string `json:"run_id"`
	BeadID       string `json:"bead_id"`
	DispatchedAt string `json:"dispatched_at"`
}

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

func atomicWriteHandlerStateDaemon(statePath string, snapshots []HandlerPauseStatusSnapshot) error {
	disk := snapshotsToDisk(snapshots)

	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("atomicWriteHandlerStateDaemon: marshal: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(statePath)

	tmp, err := os.CreateTemp(dir, ".handler-state-tmp-")
	if err != nil {
		return fmt.Errorf("atomicWriteHandlerStateDaemon: CreateTemp in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()

	if _, writeErr := tmp.Write(data); writeErr != nil {
		writeErr = errors.Join(writeErr, tmp.Close(), os.Remove(tmpPath))
		return fmt.Errorf("atomicWriteHandlerStateDaemon: write %s: %w", tmpPath, writeErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		syncErr = errors.Join(syncErr, tmp.Close(), os.Remove(tmpPath))
		return fmt.Errorf("atomicWriteHandlerStateDaemon: fsync %s: %w", tmpPath, syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		closeErr = errors.Join(closeErr, os.Remove(tmpPath))
		return fmt.Errorf("atomicWriteHandlerStateDaemon: close %s: %w", tmpPath, closeErr)
	}

	if renameErr := os.Rename(tmpPath, statePath); renameErr != nil {
		renameErr = errors.Join(renameErr, os.Remove(tmpPath))
		return fmt.Errorf("atomicWriteHandlerStateDaemon: rename %s → %s: %w", tmpPath, statePath, renameErr)
	}

	dirF, openErr := os.Open(dir) //nolint:gosec // G304: operator-controlled project dir (== filepath.Dir(statePath); see os.ReadFile below)
	if openErr != nil {
		return fmt.Errorf("atomicWriteHandlerStateDaemon: open dir %s: %w", dir, openErr)
	}
	syncErr := dirF.Sync()
	closeErr := dirF.Close()
	if syncErr != nil {
		return fmt.Errorf("atomicWriteHandlerStateDaemon: fsync dir %s: %w", dir, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("atomicWriteHandlerStateDaemon: close dir %s: %w", dir, closeErr)
	}

	return nil
}

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
			return nil
		}
		return fmt.Errorf("LoadHandlerPauseState: read %s: %w", statePath, err)
	}

	var disk handlerStateDiskDaemon
	if jsonErr := json.Unmarshal(data, &disk); jsonErr != nil {
		return fmt.Errorf("LoadHandlerPauseState: parse %s: %w", statePath, jsonErr)
	}

	if disk.SchemaVersion > handlerStateSchemaVersionDaemon {
		return &ErrHandlerStateSchemaUnsupported{
			Path: statePath,
			Got:  disk.SchemaVersion,
			Max:  handlerStateSchemaVersionDaemon,
		}
	}

	for agentTypeStr, entry := range disk.Handlers {
		agentType := core.AgentType(agentTypeStr)
		if !agentType.Valid() {
			continue
		}

		if entry.Status == "paused" && entry.Cause != nil {
			cause := diskCauseToCore(entry.Cause)
			if cause.Valid() {
				inFlight := diskInFlightToCore(entry.InFlightAtPause)
				if pauseErr := ctrl.Pause(ctx, agentType, cause, inFlight); pauseErr != nil {
					return fmt.Errorf("LoadHandlerPauseState: restore pause for %q: %w", agentTypeStr, pauseErr)
				}
				_ = entry.PausedEpoch
			}
		}

		if disk.SchemaVersion == 1 && entry.Status == "paused" && entry.Cause != nil {
			cause := diskCauseToCore(entry.Cause)
			if cause.Valid() {
				inFlight := diskInFlightToCore(entry.InFlightAtPause)
				if pauseErr := ctrl.PauseAccount(ctx, agentType, AnonymousAccountID, cause, inFlight); pauseErr != nil {
					return fmt.Errorf("LoadHandlerPauseState: restore anonymous account pause for %q: %w", agentTypeStr, pauseErr)
				}
			}
		}

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
				entry.InFlightAtPause[i] = inFlightRunDiskDaemon(r)
			}
		} else {
			entry.InFlightAtPause = []inFlightRunDiskDaemon{}
		}
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
						adisk.InFlightAtPause[i] = inFlightRunDiskDaemon(r)
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

func diskCauseToCore(d *handlerCauseDiskDaemon) core.HandlerPauseCause {
	return core.HandlerPauseCause{
		FailureClass: core.FailureClass(d.FailureClass),
		SubReason:    d.SubReason,
		SourceRunID:  d.SourceRunID,
		SourceBeadID: d.SourceBeadID,
		TrippedAt:    d.TrippedAt,
	}
}

func diskInFlightToCore(rs []inFlightRunDiskDaemon) []InFlightBeadRecord {
	out := make([]InFlightBeadRecord, 0, len(rs))
	for _, r := range rs {
		out = append(out, InFlightBeadRecord(r))
	}
	return out
}
