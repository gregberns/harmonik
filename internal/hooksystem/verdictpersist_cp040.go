package hooksystem

// verdictpersist_cp040.go — Hook verdict persistence per CP-040.
//
// Every cognition-tagged Hook evaluator's verdict MUST be persisted to the
// run's task branch at invocation time as a HookVerdictRecord written to
// .harmonik/hooks/<run_id>/<hook_invocation_id>.json, and the
// hook_verdict_persisted event MUST be emitted after the write.
//
// Spec ref: specs/control-points.md §4.8 CP-040.
// Bead ref: hk-a8bg.41
// Tags: mechanism (the persistence write; verdict production is cognition-tagged
//        per CP-042)

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// VerdictFileWriter is the interface through which the hook verdict persister
// writes a HookVerdictRecord to the run's task branch.
//
// The concrete implementation in the daemon writes the file to the worktree,
// stages it, and issues a git commit, then returns the resulting commit SHA.
// Tests supply a stub that records what was written.
//
// Tags: mechanism (the Write call itself is a pure I/O operation)
type VerdictFileWriter interface {
	// WriteAndCommit writes contents to the relative path within the
	// worktree for the run and commits it to the run's task branch.
	// Returns the git commit SHA of the resulting commit.
	WriteAndCommit(ctx context.Context, relPath string, contents []byte) (commitSHA string, err error)
}

// PersistHookVerdict writes a HookVerdictRecord to the run's task branch at
// .harmonik/hooks/<run_id>/<invocation_id>.json and emits a
// hook_verdict_persisted event per specs/control-points.md §4.8.CP-040 and
// specs/event-model.md §8.2.3.
//
// Steps:
//  1. Validate that verdict.Valid() is true (defence-in-depth guard).
//  2. Marshal verdict to JSON.
//  3. Compute the canonical file path via core.HookVerdictFilePath.
//  4. Call writer.WriteAndCommit to write and commit the file.
//  5. Emit hook_verdict_persisted with run_id, hook_invocation_id,
//     hook_name, verdict_path, and the commit_hash returned by the writer.
//
// Tags: mechanism
func PersistHookVerdict(
	ctx context.Context,
	runID core.RunID,
	verdict core.HookVerdictRecord,
	writer VerdictFileWriter,
	bus eventbus.EventBus,
) error {
	if !verdict.Valid() {
		return fmt.Errorf("hooksystem: PersistHookVerdict: invalid HookVerdictRecord for hook %q", verdict.HookName)
	}

	data, err := json.Marshal(verdict)
	if err != nil {
		return fmt.Errorf("hooksystem: PersistHookVerdict: marshal verdict for hook %q: %w", verdict.HookName, err)
	}

	verdictPath := core.HookVerdictFilePath(runID, verdict.InvocationID)

	commitSHA, err := writer.WriteAndCommit(ctx, verdictPath, data)
	if err != nil {
		return fmt.Errorf("hooksystem: PersistHookVerdict: write hook %q verdict to %q: %w",
			verdict.HookName, verdictPath, err)
	}

	eventPayload := core.HookVerdictPersistedPayload{
		RunID:            runID,
		HookInvocationID: verdict.InvocationID.String(),
		HookName:         core.HookName(verdict.HookName),
		VerdictPath:      verdictPath,
		CommitHash:       commitSHA,
	}
	raw, err := json.Marshal(eventPayload)
	if err != nil {
		return fmt.Errorf("hooksystem: PersistHookVerdict: marshal event payload for hook %q: %w", verdict.HookName, err)
	}

	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeHookVerdictPersisted, raw); emitErr != nil {
		return fmt.Errorf("hooksystem: PersistHookVerdict: emit hook_verdict_persisted for hook %q: %w",
			verdict.HookName, emitErr)
	}

	return nil
}
