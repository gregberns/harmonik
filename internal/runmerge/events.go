package runmerge

// events.go — the merge path's event emissions and their JSON payloads.
//
// Carved out of internal/daemon/workloop.go by P2 unit E5 RT13 (pure move).

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

// RunBranchToTargetPayload is the JSON payload for outcome_emitted and
// bead_closed events emitted during the merge-to-main sequence.
type mergeRunBranchToMainPayload struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id"`
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
}

// reportEmitFailure records a failure to publish one of this file's events.
//
// Every emitter here is informational and returns no error — the merge they
// describe has already happened, so a publish failure must not change control
// flow. It must not be invisible either: these events are the only record that
// a refresh overwrote local edits, that a build gate failed, or that the bead
// store went out of sync, so losing one silently loses the audit trail for a
// merge that already landed. Reported on stderr, matching gitRebaseAbort and
// the rest of this package.
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

// emitWorkingTreeRefreshFailed emits a working_tree_refresh_failed event when
// git reset --hard HEAD fails after a successful merge-to-main (EM-054).
// The event is informational: the merge is already durable.
//
// Spec ref: specs/execution-model.md §4.12 EM-054.
// Bead: hk-4goy3.
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

// emitWorkingTreeLocalEditsOverwritten emits a
// working_tree_local_edits_overwritten event naming the uncommitted local edits
// the post-merge refresh overwrote, and where to recover them from.
//
// Informational: the merge is already durable and the overwrite is intended
// (the merged commit owns its own paths). The event exists so the overwrite is
// never silent — the 2026-07-22 incident was silence, not refresh.
//
// Spec ref: specs/execution-model.md §4.12 EM-054. Bead: hk-7qmpp.
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

// emitMergeBuildFailed emits a merge_build_failed event when go build or go
// vet fails on the freshly fast-forwarded merged tree (hk-o68j3). The
// update-ref has already been rolled back before this is called.
//
// Bead: hk-o68j3.
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

// mergeStatusChangedAtLayout is RFC 3339 carrying the millisecond resolution
// that event-model.md §8.9(h) requires of a paired-phase changed_at field. Same
// layout as the operator_pause_status emitter in internal/daemon.
const mergeStatusChangedAtLayout = "2006-01-02T15:04:05.000Z07:00"

// emitWorkspaceMergeStatusMerged says out loud that the run branch landed: onto
// which branch, and at which commit.
//
// Until this call existed the merge path emitted NOTHING on success. The only
// terminal signal was outcome_emitted{approved}, and that payload is identical
// whether the target branch advanced or the run short-circuited as no-change.
// So the event stream could not answer "did this run's work land", and the way
// to answer it was to read git by hand. Somebody did, read the wrong branch,
// and filed a P1 blocker against a run that had merged correctly.
//
// The event type is not new. workspace_merge_status has been specified since
// event-model.md §8.5.3, registered in core, and classified fsync-durable by
// the bus. Nothing ever emitted it.
//
// workspace_id is derived from run_id per workspace-model.md §4.1 WM-004
// (workspace_id = "ws-" + run_id, so the UUID part IS the run_id), the same
// derivation workspace.BuildConflictEscalationPayload uses.
//
// Only the status=merged half of the paired-phase lifecycle is emitted here.
// This path holds no merge-pending workspace state to transition into, and the
// merge retries, so a per-attempt pending would break the emit-on-transition-
// only rule of §8.9(h). One emission per landing, never two.
//
// Spec ref: specs/event-model.md §8.5.3, §8.9(h); specs/workspace-model.md
// §4.5 WM-021. Bead: hk-3kw4a.
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
	// The payload owns its validity rule and this path asks before it emits,
	// matching runlaunch.EmitPreExecMessage. An event that announces a landing
	// at an empty commit is worse than a missing one, because a consumer would
	// believe it.
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

// emitBeadSyncFailed emits a bead_sync_failed event when `br sync --import-only`
// fails after a merge touching .beads/issues.jsonl (BL-MRG-004). The merge is
// already durable; this event flags that the SQLite DB is out of sync with the
// JSONL so the Cat-BL2 routing obligation can be fulfilled.
//
// Spec ref: event-model.md §8.15.1 BL-MRG-004.
// Bead: hk-zgt4u.
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
