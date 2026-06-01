package daemon

// verdictexecutor_rc025a.go — Daemon-side verdict-executor (RC-025a).
//
// RC-025a: the daemon-side verdict-executor (a deterministic Go subroutine,
// NOT a workflow node) consumes the VerdictEvent from RC-022a's outcome
// envelope and executes the 7-step sequence:
//
//  1. Validates the verdict per RC-020/RC-023; on failure routes fallback.
//  2. Re-captures snapshot per RC-024 staleness check; on stale routes Cat 3b.
//  3. Constructs and commits reconciliation_verdict_emitted commit (verdict
//     body + Harmonik-Run-ID / Harmonik-Workflow-Class / Harmonik-Target-Run-ID
//     trailers) on the investigator's task branch.
//  4. Mechanically applies the verdict's action per schemas.md §6.2.
//  5. Constructs and commits reconciliation_verdict_executed commit with the
//     Harmonik-Verdict-Executed: true trailer (a descendant of step 3).
//  6. Emits reconciliation_verdict_emitted and reconciliation_verdict_executed
//     events.
//  7. Releases the RC-002a lock per RC-002b.
//
// The executor is panic-safe (PL-018a): a per-function recover() catches any
// mid-step panic and returns an error. On panic between steps 3 and 5 the next
// daemon startup detects the incomplete pair via Cat 3b (RC-026).
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-025a.
// Bead ref: hk-63oh.36.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/workspace"
)

// VerdictExecutorConfig holds construction-time parameters for ExecuteVerdict.
type VerdictExecutorConfig struct {
	// ProjectDir is the harmonik project root. Required.
	ProjectDir string

	// TargetBeadID is the Beads bead ID for the outer (target) run being
	// reconciled. Used for the RC-024 beads-audit staleness dimension and for
	// Beads-bound mechanical actions (reopen-bead, accept-close-with-note).
	// May be empty for bead-free runs; empty disables the beads-audit staleness
	// check and Beads writes without failing those steps.
	TargetBeadID core.BeadID

	// BrAdapter is the Beads CLI adapter. Nil disables Beads-bound actions.
	// When non-nil and TargetBeadID is non-empty, used for the beads-audit
	// staleness re-capture and for reopen-bead / accept-close-with-note.
	BrAdapter *brcli.Adapter

	// IntentLogDir is the directory used by the brcli terminal-transition intent
	// log per BI-029. Required when BrAdapter is non-nil and a Beads-writing
	// verdict (reopen-bead, accept-close-with-note) may be executed.
	IntentLogDir string

	// BrTimeoutConfig carries br invocation timeout overrides. Zero value uses
	// brcli defaults.
	BrTimeoutConfig brcli.TimeoutConfig

	// Emitter is the event bus for emitting reconciliation events (steps 6 + any
	// malformed/stale events in steps 1–2). Required.
	Emitter handlercontract.EventEmitter
}

// VerdictExecutorResult captures the high-level outcome of ExecuteVerdict.
type VerdictExecutorResult struct {
	// Executed is true when verdict execution reached step 5 (verdict-executed
	// commit landed). False when validation failed or a stale route fired.
	Executed bool

	// Stale is true when the RC-024 staleness check triggered a re-dispatch
	// route (step 2). Mutually exclusive with Executed.
	Stale bool

	// Malformed is true when the verdict failed RC-020 validation (step 1) and
	// the escalate-to-human fallback was applied. Mutually exclusive with Executed.
	Malformed bool
}

// ExecuteVerdict runs the RC-025a 7-step verdict-executor sequence.
//
// ve is the VerdictEvent from the investigator's outcome envelope (RC-022a).
// lock is the per-run reconciliation lock acquired before dispatch (RC-002a);
// it is always released in step 7 via defer, even on error.
//
// The function is panic-safe: any panic is caught by an outer recover(), the
// error is returned, and the lock is still released.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-025a.
func ExecuteVerdict(
	ctx context.Context,
	ve core.VerdictEvent,
	lock *lifecycle.ReconciliationLock,
	cfg VerdictExecutorConfig,
) (result VerdictExecutorResult, retErr error) {
	// Panic safety (PL-018a): recover from any mid-step panic, return error,
	// release lock. Cat 3b re-execution on next startup handles partial work.
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("daemon: ExecuteVerdict: panic in verdict-executor: %v", r)
		}
		// Step 7: release the RC-002a lock (always, even on error/panic).
		if lock != nil {
			if releaseErr := lock.Release(); releaseErr != nil && retErr == nil {
				retErr = fmt.Errorf("daemon: ExecuteVerdict: lock Release: %w", releaseErr)
			}
		}
	}()

	// ── Step 1: Validate verdict per RC-020/RC-023 ────────────────────────────
	if !ve.Valid() {
		// Emit reconciliation_verdict_malformed; fall through to escalate-to-human.
		malformed := core.MalformedVerdictPayload{
			InvestigatorRunID:  ve.InvestigatorRunID,
			TargetRunID:        ve.TargetRunID,
			MalformationReason: core.MalformationReasonUnknownVerdictValue,
			RawVerdictExcerpt:  verdictExcerpt(ve),
		}
		if emitErr := emitMarshal(ctx, cfg.Emitter, core.EventTypeReconciliationVerdictMalformed, malformed); emitErr != nil {
			return result, fmt.Errorf("daemon: ExecuteVerdict: emit malformed: %w", emitErr)
		}
		// RC-023: fallback verdict is escalate-to-human.
		ve.Verdict = core.VerdictEscalateToHuman
		ve.Context = nil
		ve.CheckpointRef = nil
		result.Malformed = true
		// Continue to execute the fallback escalate-to-human verdict.
	}

	// ── Step 2: RC-024 staleness check ───────────────────────────────────────
	currentGitHead, gitErr := captureGitHead(ctx, cfg.ProjectDir)
	if gitErr != nil {
		return result, fmt.Errorf("daemon: ExecuteVerdict: re-capture git HEAD: %w", gitErr)
	}
	currentBeadsAuditID, auditErr := captureBeadsAuditID(ctx, cfg.BrAdapter, cfg.TargetBeadID)
	if auditErr != nil || (cfg.BrAdapter == nil || cfg.TargetBeadID == "") {
		// When the adapter is unavailable or no bead is associated, treat the
		// beads-audit dimension as matching the snapshot to avoid spurious
		// staleness. The Cat 3b idempotency guard (RC-026) handles any
		// double-execution on a subsequent re-run.
		currentBeadsAuditID = ve.SnapshotToken.BeadsAuditEntryID
	}

	stalenessResult := core.CheckVerdictStaleness(ve.SnapshotToken, currentGitHead, currentBeadsAuditID)
	if stalenessResult.Stale && stalenessResult.Payload != nil {
		if emitErr := emitMarshal(ctx, cfg.Emitter, core.EventTypeReconciliationVerdictStale, stalenessResult.Payload); emitErr != nil {
			return result, fmt.Errorf("daemon: ExecuteVerdict: emit stale: %w", emitErr)
		}
		result.Stale = true
		return result, nil // caller re-dispatches fresh reconciliation per §8.5
	}

	// ── Step 3: Commit reconciliation_verdict_emitted on investigator branch ──
	worktreePath := workspace.WorktreePath(cfg.ProjectDir, ve.InvestigatorRunID.String(), workspace.NoWorktreeRootOverride())
	verdictJSON, marshalErr := json.MarshalIndent(ve, "", "  ")
	if marshalErr != nil {
		return result, fmt.Errorf("daemon: ExecuteVerdict: marshal VerdictEvent: %w", marshalErr)
	}
	if commitErr := commitVerdictEmitted(ctx, worktreePath, ve, verdictJSON); commitErr != nil {
		return result, fmt.Errorf("daemon: ExecuteVerdict: commit verdict-emitted: %w", commitErr)
	}

	// ── Step 4: Apply mechanical action per schemas.md §6.2 ─────────────────
	plan := core.PlanForVerdict(ve.Verdict)
	if actionErr := applyVerdictAction(ctx, ve, plan, cfg); actionErr != nil {
		return result, fmt.Errorf("daemon: ExecuteVerdict: apply action %q: %w", plan.ActionKind, actionErr)
	}

	// ── Step 5: Commit reconciliation_verdict_executed on investigator branch ─
	//
	// RC-002b: this write and the lock WriteVerdictExecuted call are NOT atomic.
	// Cat 3b re-execution on next startup handles the window where step 3 landed
	// but step 5 did not.
	if commitErr := commitVerdictExecuted(ctx, worktreePath, ve); commitErr != nil {
		return result, fmt.Errorf("daemon: ExecuteVerdict: commit verdict-executed: %w", commitErr)
	}
	// Write verdict-executed marker to the lock file (RC-002b discrimination).
	if lock != nil {
		if writeErr := lock.WriteVerdictExecuted(); writeErr != nil {
			return result, fmt.Errorf("daemon: ExecuteVerdict: lock WriteVerdictExecuted: %w", writeErr)
		}
	}

	// ── Step 6: Emit reconciliation_verdict_emitted + reconciliation_verdict_executed ──
	execTS := time.Now().UTC().Format(time.RFC3339)

	emittedPayload := core.ReconciliationVerdictEmittedPayload{
		InvestigatorRunID: core.RunID(ve.InvestigatorRunID),
		TargetRunID:       core.RunID(ve.TargetRunID),
		Verdict:           ve.Verdict,
	}
	if emitErr := emitMarshal(ctx, cfg.Emitter, core.EventTypeReconciliationVerdictEmitted, emittedPayload); emitErr != nil {
		return result, fmt.Errorf("daemon: ExecuteVerdict: emit verdict-emitted: %w", emitErr)
	}

	executedPayload := core.VerdictExecutedPayload{
		InvestigatorRunID:   ve.InvestigatorRunID,
		TargetRunID:         ve.TargetRunID,
		Verdict:             ve.Verdict,
		ExecutedAtTimestamp: execTS,
		ActionSummary:       plan.ActionSummary,
	}
	if emitErr := emitMarshal(ctx, cfg.Emitter, core.EventTypeReconciliationVerdictExecuted, executedPayload); emitErr != nil {
		return result, fmt.Errorf("daemon: ExecuteVerdict: emit verdict-executed: %w", emitErr)
	}

	result.Executed = true
	return result, nil
	// Step 7 (lock release) fires in the deferred function above.
}

// ── Git helpers ────────────────────────────────────────────────────────────

// commitVerdictEmitted writes the verdict JSON to the investigator's worktree
// under .harmonik/reconciliation/<investigator_run_id>/verdict.json and
// commits it with the canonical reconciliation trailers.
//
// Trailers on the verdict-emitted commit per RC-025a / schemas.md §6.4:
//   - Harmonik-Run-ID: <investigator_run_id>
//   - Harmonik-Workflow-Class: reconciliation
//   - Harmonik-Target-Run-ID: <target_run_id>
//   - Harmonik-Schema-Version: 1
//   - Harmonik-State-ID: <fresh UUIDv7>
//   - Harmonik-Transition-ID: <fresh UUIDv7>
func commitVerdictEmitted(ctx context.Context, worktreePath string, ve core.VerdictEvent, verdictJSON []byte) error {
	// Write verdict JSON file.
	reconDir := filepath.Join(worktreePath, ".harmonik", "reconciliation", ve.InvestigatorRunID.String())
	//nolint:gosec // G301: 0755 matches .harmonik subdir conventions
	if err := os.MkdirAll(reconDir, 0o755); err != nil {
		return fmt.Errorf("commitVerdictEmitted: mkdir %q: %w", reconDir, err)
	}
	verdictFilePath := filepath.Join(reconDir, "verdict.json")
	//nolint:gosec // G306: 0600 is appropriate for this daemon-written state file
	if err := os.WriteFile(verdictFilePath, verdictJSON, 0o600); err != nil {
		return fmt.Errorf("commitVerdictEmitted: write verdict.json: %w", err)
	}

	// git add .harmonik/reconciliation/<investigator_run_id>/
	relDir := filepath.Join(".harmonik", "reconciliation", ve.InvestigatorRunID.String())
	addCmd := exec.CommandContext(ctx, "git", "add", relDir)
	addCmd.Dir = worktreePath
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commitVerdictEmitted: git add: %w\n%s", err, out)
	}

	// Build commit message with trailers.
	stateID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("commitVerdictEmitted: generate state-id: %w", err)
	}
	transitionID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("commitVerdictEmitted: generate transition-id: %w", err)
	}

	var msg bytes.Buffer
	fmt.Fprintf(&msg, "reconciliation(verdict-emitted): %s for target run %s\n\n",
		string(ve.Verdict), ve.TargetRunID.String())
	fmt.Fprintf(&msg, "Verdict: %s\n", string(ve.Verdict))
	fmt.Fprintf(&msg, "\n")
	fmt.Fprintf(&msg, "Harmonik-Run-ID: %s\n", ve.InvestigatorRunID.String())
	fmt.Fprintf(&msg, "Harmonik-Workflow-Class: reconciliation\n")
	fmt.Fprintf(&msg, "Harmonik-Target-Run-ID: %s\n", ve.TargetRunID.String())
	fmt.Fprintf(&msg, "Harmonik-Schema-Version: %s\n", strconv.Itoa(ve.SchemaVersion))
	fmt.Fprintf(&msg, "Harmonik-State-ID: %s\n", stateID.String())
	fmt.Fprintf(&msg, "Harmonik-Transition-ID: %s\n", transitionID.String())

	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", msg.String())
	commitCmd.Dir = worktreePath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commitVerdictEmitted: git commit: %w\n%s", err, out)
	}
	return nil
}

// commitVerdictExecuted appends the verdict-executed commit to the investigator's
// task branch. The commit is payload-free (presence-only marker per schemas.md §6.4);
// it uses --allow-empty because no file changes are required.
//
// Trailers on the verdict-executed commit per schemas.md §6.4:
//   - Harmonik-Verdict-Executed: true
//   - Harmonik-Run-ID: <investigator_run_id>
func commitVerdictExecuted(ctx context.Context, worktreePath string, ve core.VerdictEvent) error {
	stateID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("commitVerdictExecuted: generate state-id: %w", err)
	}
	transitionID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("commitVerdictExecuted: generate transition-id: %w", err)
	}

	var msg bytes.Buffer
	fmt.Fprintf(&msg, "reconciliation(verdict-executed): executed %s for target run %s\n\n",
		string(ve.Verdict), ve.TargetRunID.String())
	fmt.Fprintf(&msg, "Harmonik-Verdict-Executed: true\n")
	fmt.Fprintf(&msg, "Harmonik-Run-ID: %s\n", ve.InvestigatorRunID.String())
	fmt.Fprintf(&msg, "Harmonik-Schema-Version: 1\n")
	fmt.Fprintf(&msg, "Harmonik-State-ID: %s\n", stateID.String())
	fmt.Fprintf(&msg, "Harmonik-Transition-ID: %s\n", transitionID.String())

	commitCmd := exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", msg.String())
	commitCmd.Dir = worktreePath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commitVerdictExecuted: git commit --allow-empty: %w\n%s", err, out)
	}
	return nil
}

// ── Staleness re-capture helpers ─────────────────────────────────────────────

// captureGitHead runs `git rev-parse HEAD` in the project root and returns
// the commit hash. Used by RC-024 staleness re-capture (step 2).
func captureGitHead(ctx context.Context, projectDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("captureGitHead: git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// captureBeadsAuditID fetches the latest Beads audit entry ID for beadID via
// AuditLog. Returns the last event's ID as a decimal string, or an empty
// string when the adapter/bead is unavailable.
func captureBeadsAuditID(ctx context.Context, adapter *brcli.Adapter, beadID core.BeadID) (string, error) {
	if adapter == nil || beadID == "" {
		return "", nil
	}
	events, err := adapter.AuditLog(ctx, beadID)
	if err != nil {
		return "", fmt.Errorf("captureBeadsAuditID: AuditLog %q: %w", beadID, err)
	}
	if len(events) == 0 {
		return "", nil
	}
	return strconv.FormatInt(events[len(events)-1].ID, 10), nil
}

// ── Mechanical action dispatch ────────────────────────────────────────────────

// applyVerdictAction executes the mechanical action for the given verdict per
// schemas.md §6.2. Each action is idempotent per the idempotency rules in the
// verdict-execution table.
func applyVerdictAction(ctx context.Context, ve core.VerdictEvent, plan core.VerdictExecutionPlan, cfg VerdictExecutorConfig) error {
	switch plan.ActionKind {
	case core.VerdictActionKindNoOp:
		// no-op-accept: no mechanical action beyond emitting verdict-executed.
		return nil

	case core.VerdictActionKindEscalateToHuman:
		// escalate-to-human: emit operator_escalation_required; deduplicated by
		// target_run_id per schemas.md §6.2.
		targetRunID := core.RunID(ve.TargetRunID)
		escalationPayload := core.OperatorEscalationRequiredPayload{
			TargetRunID: &targetRunID,
			Reason:      core.OperatorEscalationReasonOtherVerdictDriven,
		}
		return emitMarshal(ctx, cfg.Emitter, core.EventTypeOperatorEscalationRequired, escalationPayload)

	case core.VerdictActionKindReopenBead:
		// reopen-bead: invoke the BI-CLI adapter reopen path per BI-010 / BI-010a.
		// Idempotency via BI-031b status-check-before-reissue.
		if cfg.BrAdapter == nil || cfg.TargetBeadID == "" {
			return fmt.Errorf("applyVerdictAction: reopen-bead requires BrAdapter and TargetBeadID")
		}
		transitionID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("applyVerdictAction: reopen-bead: generate transition-id: %w", err)
		}
		reason := fmt.Sprintf("reconciliation reopen-bead verdict for target run %s", ve.TargetRunID.String())
		return cfg.BrAdapter.ReopenBead(
			ctx,
			cfg.IntentLogDir,
			cfg.BrTimeoutConfig,
			core.RunID(ve.TargetRunID),
			core.TransitionID(transitionID),
			cfg.TargetBeadID,
			reason,
		)

	case core.VerdictActionKindAcceptCloseWithNote:
		// accept-close-with-note: write Beads close if bead not already closed.
		// Idempotency key: <target_run_id>:close per schemas.md §6.2.
		if cfg.BrAdapter == nil || cfg.TargetBeadID == "" {
			return fmt.Errorf("applyVerdictAction: accept-close-with-note requires BrAdapter and TargetBeadID")
		}
		transitionID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("applyVerdictAction: accept-close-with-note: generate transition-id: %w", err)
		}
		return cfg.BrAdapter.CloseBead(
			ctx,
			cfg.IntentLogDir,
			cfg.BrTimeoutConfig,
			core.RunID(ve.TargetRunID),
			core.TransitionID(transitionID),
			cfg.TargetBeadID,
			false, // needsAttention: close is legitimate; no attention marker
		)

	case core.VerdictActionKindDispatchCurrentNode:
		// resume-here / resume-with-context: re-dispatch the outer run's current
		// node. This requires the daemon's dispatch infrastructure which is not
		// yet wired to the verdict executor at MVH; the verdict-executed commit
		// still lands (idempotency is at the dispatch layer). Log and return nil.
		//
		// TODO: wire VerdictNodeDispatcher per RC-025 when dispatch infra is ready.
		return nil

	case core.VerdictActionKindResetToCheckpoint:
		// reset-to-checkpoint: intra-run rollback to the named checkpoint. Requires
		// the daemon dispatch infrastructure (EM-044 / EM-045). Not yet wired at
		// MVH; verdict-executed commit lands for idempotency. Log and return nil.
		//
		// TODO: wire VerdictNodeDispatcher per RC-025 / EM-044 when ready.
		return nil

	default:
		return fmt.Errorf("applyVerdictAction: unknown ActionKind %q", plan.ActionKind)
	}
}

// ── Emit helper ───────────────────────────────────────────────────────────────

// emitMarshal JSON-encodes payload and emits it on the event bus.
func emitMarshal(ctx context.Context, emitter handlercontract.EventEmitter, eventType core.EventType, payload any) error {
	if emitter == nil {
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("emitMarshal(%s): marshal: %w", eventType, err)
	}
	return emitter.Emit(ctx, eventType, b)
}

// verdictExcerpt returns a short string representation of the verdict for
// inclusion in malformation-reason payloads.
func verdictExcerpt(ve core.VerdictEvent) string {
	b, err := json.Marshal(ve.Verdict)
	if err != nil {
		return "unparseable"
	}
	return string(b)
}
