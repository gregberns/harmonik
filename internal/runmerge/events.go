package runmerge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

type mergeRunBranchToMainPayload struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id"`
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
}

func reportEmitFailure(evType core.EventType, err error) {
	fmt.Fprintf(os.Stderr, "daemon: runmerge: emit %s failed: %v\n", evType, err)
}

// EmitOutcomeEmitted emits an outcome_emitted event with the given kind and
// optional reason. kind is "approved" on success, "rejected" on failure.
//
// Spec ref: specs/execution-model.md §4.12.EM-052, EM-053.
// Bead: hk-ftyvo.
func EmitOutcomeEmitted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, kind, reason string) {
	pl := mergeRunBranchToMainPayload{
		RunID:  runID.String(),
		BeadID: string(beadID),
		Kind:   kind,
		Reason: reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		reportEmitFailure(core.EventTypeOutcomeEmitted, fmt.Errorf("marshal payload: %w", err))
		return
	}
	if emitErr := bus.Emit(ctx, core.EventTypeOutcomeEmitted, b); emitErr != nil {
		reportEmitFailure(core.EventTypeOutcomeEmitted, emitErr)
	}
}

func emitWorkingTreeRefreshFailed(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, refreshErr error) {
	pl := core.WorkingTreeRefreshFailedPayload{
		RunID:  runID,
		BeadID: beadID,
		Error:  refreshErr.Error(),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		reportEmitFailure(core.EventTypeWorkingTreeRefreshFailed, fmt.Errorf("marshal payload: %w", err))
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeWorkingTreeRefreshFailed, b); emitErr != nil {
		reportEmitFailure(core.EventTypeWorkingTreeRefreshFailed, emitErr)
	}
}

func emitWorkingTreeLocalEditsOverwritten(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, mainPath string, paths []string, recoveryPatch string) {
	pl := core.WorkingTreeLocalEditsOverwrittenPayload{
		RunID:         runID,
		BeadID:        string(beadID),
		MainPath:      mainPath,
		Paths:         paths,
		RecoveryPatch: recoveryPatch,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		reportEmitFailure(core.EventTypeWorkingTreeLocalEditsOverwritten, fmt.Errorf("marshal payload: %w", err))
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeWorkingTreeLocalEditsOverwritten, b); emitErr != nil {
		reportEmitFailure(core.EventTypeWorkingTreeLocalEditsOverwritten, emitErr)
	}
}

func emitMergeBuildFailed(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, buildErr error, output []byte) {
	errMsg := buildErr.Error()
	if len(output) > 0 {
		errMsg = fmt.Sprintf("%s\n%s", errMsg, strings.TrimRight(string(output), "\n"))
	}
	pl := core.MergeBuildFailedPayload{
		RunID:  runID,
		BeadID: beadID,
		Error:  errMsg,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		reportEmitFailure(core.EventTypeMergeBuildFailed, fmt.Errorf("marshal payload: %w", err))
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeMergeBuildFailed, b); emitErr != nil {
		reportEmitFailure(core.EventTypeMergeBuildFailed, emitErr)
	}
}

const mergeStatusChangedAtLayout = "2006-01-02T15:04:05.000Z07:00"

func emitWorkspaceMergeStatusMerged(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, sourceBranch, targetBranch, mergeCommit string) {
	pl := core.WorkspaceMergeStatusPayload{
		WorkspaceID:     core.WorkspaceID(runID),
		RunID:           runID,
		Status:          core.WorkspaceMergeStatusMerged,
		SourceBranch:    sourceBranch,
		TargetBranch:    targetBranch,
		MergeCommitHash: &mergeCommit,
		ChangedAt:       time.Now().UTC().Format(mergeStatusChangedAtLayout),
	}
	if !pl.Valid() {
		reportEmitFailure(core.EventTypeWorkspaceMergeStatus, fmt.Errorf(
			"refusing to emit a payload its own Valid() rejects: run %s source %q target %q commit %q",
			runID.String(), sourceBranch, targetBranch, mergeCommit))
		return
	}
	b, err := json.Marshal(pl)
	if err != nil {
		reportEmitFailure(core.EventTypeWorkspaceMergeStatus, fmt.Errorf("marshal payload: %w", err))
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeWorkspaceMergeStatus, b); emitErr != nil {
		reportEmitFailure(core.EventTypeWorkspaceMergeStatus, emitErr)
	}
}

func emitBeadSyncFailed(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, syncErr error, output []byte) {
	errMsg := syncErr.Error()
	if len(output) > 0 {
		errMsg = fmt.Sprintf("%s\n%s", errMsg, strings.TrimRight(string(output), "\n"))
	}
	pl := core.BeadSyncFailedPayload{
		RunID:     runID.String(),
		Error:     errMsg,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		reportEmitFailure(core.EventTypeBeadSyncFailed, fmt.Errorf("marshal payload: %w", err))
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeBeadSyncFailed, b); emitErr != nil {
		reportEmitFailure(core.EventTypeBeadSyncFailed, emitErr)
	}
}
