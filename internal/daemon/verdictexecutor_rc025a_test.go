package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

func ve025aGitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@harmonik"},
		{"config", "user.name", "harmonik-test"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // G204: fixed args; not user input
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("ve025aGitInit: git %v: %v\n%s", args, err, out)
		}
	}
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("test repo\n"), 0o644); err != nil {
		t.Fatalf("ve025aGitInit: WriteFile README: %v", err)
	}
	for _, args := range [][]string{
		{"add", "README"},
		{"commit", "-m", "initial"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // G204: fixed args; not user input
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("ve025aGitInit: git %v: %v\n%s", args, err, out)
		}
	}
}

func ve025aValidVerdictEvent(verdict core.Verdict) core.VerdictEvent {
	ve := core.VerdictEvent{
		Verdict:           verdict,
		InvestigatorRunID: uuid.Must(uuid.NewV7()),
		TargetRunID:       uuid.Must(uuid.NewV7()),
		SnapshotToken: core.SnapshotToken{
			GitHeadHash:         "placeholder-replaced-by-test",
			BeadsAuditEntryID:   "1",
			CapturedAtTimestamp: time.Now().UTC().Format(time.RFC3339),
		},
		SchemaVersion: 1,
	}
	if verdict == core.VerdictResumeWithContext {
		ctx := "investigator context"
		ve.Context = &ctx
	}
	if verdict == core.VerdictResetToCheckpoint {
		tid := core.TransitionID(uuid.Must(uuid.NewV7()))
		ve.CheckpointRef = &tid
	}
	return ve
}

type ve025aRecordingEmitter struct {
	Types []core.EventType
}

func (e *ve025aRecordingEmitter) Emit(_ context.Context, eventType core.EventType, _ []byte) error {
	e.Types = append(e.Types, eventType)
	return nil
}

func (e *ve025aRecordingEmitter) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, _ []byte) error {
	e.Types = append(e.Types, eventType)
	return nil
}

func ve025aAcquireLock(t *testing.T, projectDir, targetRunID string) *lifecycle.ReconciliationLock {
	t.Helper()
	lock, err := lifecycle.AcquireReconciliationLock(projectDir, targetRunID)
	if err != nil {
		t.Fatalf("ve025aAcquireLock: %v", err)
	}
	return lock
}

func ve025aWorktreePath(projectDir, runID string) string {
	return filepath.Join(projectDir, ".harmonik", "worktrees", runID)
}

func ve025aSetupWorktree(t *testing.T, projectDir string, investigatorRunID uuid.UUID) {
	t.Helper()
	wtPath := ve025aWorktreePath(projectDir, investigatorRunID.String())
	//nolint:gosec // G301: test-only directory
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("ve025aSetupWorktree: mkdir: %v", err)
	}
	ve025aGitInit(t, wtPath)
}

func ve025aCurrentGitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ve025aCurrentGitHead: git rev-parse HEAD: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func ve025aGitLog(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "log", "--format=fuller", "--no-abbrev-commit")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ve025aGitLog: git log: %v\n%s", err, out)
	}
	return string(out)
}

func ve025aAssertEventEmitted(t *testing.T, types []core.EventType, want core.EventType, desc string) {
	t.Helper()
	for _, et := range types {
		if et == want {
			return
		}
	}
	t.Errorf("%s: event %q not emitted; emitted types: %v", desc, want, types)
}

func ve025aCfg(projectDir string, emitter *ve025aRecordingEmitter) daemon.VerdictExecutorConfig {
	return daemon.VerdictExecutorConfig{
		ProjectDir: projectDir,
		Emitter:    emitter,
	}
}

// TestExecuteVerdict_MalformedVerdict_RoutesToFallback verifies that a
// VerdictEvent that fails Valid() causes ExecuteVerdict to route to the
// escalate-to-human fallback per RC-023 and emit the malformed event.
//
// RC-025a step 1: "Validates the verdict per RC-020/RC-023; on validation
// failure, routes through fallback per RC-023."
func TestExecuteVerdict_MalformedVerdict_RoutesToFallback(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ve025aGitInit(t, projectDir)

	ve := core.VerdictEvent{
		Verdict:           core.VerdictNoOpAccept,
		InvestigatorRunID: uuid.Nil,
		TargetRunID:       uuid.Must(uuid.NewV7()),
		SnapshotToken: core.SnapshotToken{
			GitHeadHash:         "deadbeef",
			BeadsAuditEntryID:   "1",
			CapturedAtTimestamp: time.Now().UTC().Format(time.RFC3339),
		},
		SchemaVersion: 1,
	}
	if ve.Valid() {
		t.Fatal("fixture VerdictEvent must be invalid; test setup error")
	}

	emitter := &ve025aRecordingEmitter{}
	lock := ve025aAcquireLock(t, projectDir, ve.TargetRunID.String())

	result, _ := daemon.ExecuteVerdict(context.Background(), ve, lock, ve025aCfg(projectDir, emitter))

	if !result.Malformed {
		t.Error("Expected result.Malformed=true for invalid VerdictEvent, got false")
	}

	ve025aAssertEventEmitted(t, emitter.Types, core.EventTypeReconciliationVerdictMalformed,
		"RC-023: reconciliation_verdict_malformed must be emitted on validation failure")
}

// TestExecuteVerdict_StaleVerdict_ReturnsStale verifies that when the git HEAD
// has advanced past the snapshot token, ExecuteVerdict sets Stale=true and
// emits reconciliation_verdict_stale without executing the verdict.
//
// RC-025a step 2: "Re-captures snapshot per RC-024 staleness check; on stale,
// routes through Cat 3b re-execution per §8.5."
func TestExecuteVerdict_StaleVerdict_ReturnsStale(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ve025aGitInit(t, projectDir)

	ve := ve025aValidVerdictEvent(core.VerdictNoOpAccept)
	ve.SnapshotToken.GitHeadHash = "0000000000000000000000000000000000000000"

	emitter := &ve025aRecordingEmitter{}
	lock := ve025aAcquireLock(t, projectDir, ve.TargetRunID.String())

	result, err := daemon.ExecuteVerdict(context.Background(), ve, lock, ve025aCfg(projectDir, emitter))
	if err != nil {
		t.Fatalf("ExecuteVerdict returned unexpected error: %v", err)
	}

	if !result.Stale {
		t.Error("Expected result.Stale=true for stale snapshot token, got false")
	}
	if result.Executed {
		t.Error("Expected result.Executed=false when verdict is stale")
	}

	ve025aAssertEventEmitted(t, emitter.Types, core.EventTypeReconciliationVerdictStale,
		"RC-024: reconciliation_verdict_stale must be emitted on staleness detection")
}

// TestExecuteVerdict_NoOpAccept_CommitsAndEmits verifies the happy-path
// execution of a no-op-accept verdict: both commits land on the investigator's
// task branch and both reconciliation events are emitted.
//
// RC-025a steps 3–6: commit verdict-emitted, apply no-op, commit
// verdict-executed, emit reconciliation_verdict_emitted +
// reconciliation_verdict_executed.
func TestExecuteVerdict_NoOpAccept_CommitsAndEmits(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ve025aGitInit(t, projectDir)

	ve := ve025aValidVerdictEvent(core.VerdictNoOpAccept)
	ve025aSetupWorktree(t, projectDir, ve.InvestigatorRunID)
	ve.SnapshotToken.GitHeadHash = ve025aCurrentGitHead(t, projectDir)

	emitter := &ve025aRecordingEmitter{}
	lock := ve025aAcquireLock(t, projectDir, ve.TargetRunID.String())

	result, err := daemon.ExecuteVerdict(context.Background(), ve, lock, ve025aCfg(projectDir, emitter))
	if err != nil {
		t.Fatalf("ExecuteVerdict returned error: %v", err)
	}

	if !result.Executed {
		t.Errorf("Expected result.Executed=true; got %+v", result)
	}
	if result.Stale || result.Malformed {
		t.Errorf("Unexpected Stale/Malformed flags; got %+v", result)
	}

	ve025aAssertEventEmitted(t, emitter.Types, core.EventTypeReconciliationVerdictEmitted,
		"RC-025a step 6: reconciliation_verdict_emitted must be emitted")
	ve025aAssertEventEmitted(t, emitter.Types, core.EventTypeReconciliationVerdictExecuted,
		"RC-025a step 6: reconciliation_verdict_executed must be emitted")

	wtPath := ve025aWorktreePath(projectDir, ve.InvestigatorRunID.String())
	logOut := ve025aGitLog(t, wtPath)
	if !strings.Contains(logOut, "verdict-emitted") {
		t.Errorf("verdict-emitted commit missing from investigator worktree log:\n%s", logOut)
	}
	if !strings.Contains(logOut, "verdict-executed") {
		t.Errorf("verdict-executed commit missing from investigator worktree log:\n%s", logOut)
	}

	verdictFile := filepath.Join(wtPath, ".harmonik", "reconciliation", ve.InvestigatorRunID.String(), "verdict.json")
	data, readErr := os.ReadFile(verdictFile) //nolint:gosec // G304: path from test fixture
	if readErr != nil {
		t.Fatalf("verdict.json not found at %q: %v", verdictFile, readErr)
	}
	if !json.Valid(data) {
		t.Errorf("verdict.json is not valid JSON: %q", data)
	}
}

// TestExecuteVerdict_EscalateToHuman_EmitsOperatorEscalation verifies that
// the escalate-to-human verdict emits operator_escalation_required per
// schemas.md §6.2 in addition to the standard verdict events.
func TestExecuteVerdict_EscalateToHuman_EmitsOperatorEscalation(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ve025aGitInit(t, projectDir)

	ve := ve025aValidVerdictEvent(core.VerdictEscalateToHuman)
	ve025aSetupWorktree(t, projectDir, ve.InvestigatorRunID)
	ve.SnapshotToken.GitHeadHash = ve025aCurrentGitHead(t, projectDir)

	emitter := &ve025aRecordingEmitter{}
	lock := ve025aAcquireLock(t, projectDir, ve.TargetRunID.String())

	result, err := daemon.ExecuteVerdict(context.Background(), ve, lock, ve025aCfg(projectDir, emitter))
	if err != nil {
		t.Fatalf("ExecuteVerdict returned error: %v", err)
	}
	if !result.Executed {
		t.Errorf("Expected result.Executed=true; got %+v", result)
	}

	ve025aAssertEventEmitted(t, emitter.Types, core.EventTypeOperatorEscalationRequired,
		"schemas.md §6.2 escalate-to-human: operator_escalation_required must be emitted")
	ve025aAssertEventEmitted(t, emitter.Types, core.EventTypeReconciliationVerdictEmitted, "step 6 verdict-emitted")
	ve025aAssertEventEmitted(t, emitter.Types, core.EventTypeReconciliationVerdictExecuted, "step 6 verdict-executed")
}

// TestExecuteVerdict_LockReleasedOnError verifies that the RC-002a lock is
// always released (step 7) even when execution fails mid-way.
//
// RC-025a step 7: lock release is unconditional.
func TestExecuteVerdict_LockReleasedOnError(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ve025aGitInit(t, projectDir)

	ve := ve025aValidVerdictEvent(core.VerdictNoOpAccept)
	ve.SnapshotToken.GitHeadHash = ve025aCurrentGitHead(t, projectDir)

	lock := ve025aAcquireLock(t, projectDir, ve.TargetRunID.String())

	_, _ = daemon.ExecuteVerdict(context.Background(), ve, lock, ve025aCfg(projectDir, &ve025aRecordingEmitter{}))

	const releaseWindow = 2 * time.Second
	deadline := time.Now().Add(releaseWindow)
	var lastErr error
	for {
		lock2, err := lifecycle.AcquireReconciliationLock(projectDir, ve.TargetRunID.String())
		if err == nil {
			if relErr := lock2.Release(); relErr != nil {
				t.Errorf("release probe reconciliation lock: %v", relErr)
			}
			return
		}
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Errorf("RC-002a lock not released after ExecuteVerdict error: %v; "+
		"RC-025a step 7 must release the lock unconditionally "+
		"(still held after retrying for %v — not the hk-fei89 fork/exec window)",
		lastErr, releaseWindow)
}

// TestExecuteVerdict_VerdictEmittedCommit_HasReconciliationTrailer verifies that
// the verdict-emitted commit carries the Harmonik-Workflow-Class: reconciliation
// trailer used by the RC-026 startup detector to identify verdict commits.
//
// BranchVerdictEvidence.HasVerdictCommit checks for this trailer.
func TestExecuteVerdict_VerdictEmittedCommit_HasReconciliationTrailer(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ve025aGitInit(t, projectDir)

	ve := ve025aValidVerdictEvent(core.VerdictNoOpAccept)
	ve025aSetupWorktree(t, projectDir, ve.InvestigatorRunID)
	ve.SnapshotToken.GitHeadHash = ve025aCurrentGitHead(t, projectDir)

	lock := ve025aAcquireLock(t, projectDir, ve.TargetRunID.String())
	result, err := daemon.ExecuteVerdict(context.Background(), ve, lock, ve025aCfg(projectDir, &ve025aRecordingEmitter{}))
	if err != nil {
		t.Fatalf("ExecuteVerdict: %v", err)
	}
	if !result.Executed {
		t.Fatalf("Expected Executed=true; got %+v", result)
	}

	wtPath := ve025aWorktreePath(projectDir, ve.InvestigatorRunID.String())
	logOut := ve025aGitLog(t, wtPath)
	if !strings.Contains(logOut, "Harmonik-Workflow-Class: reconciliation") {
		t.Errorf("Harmonik-Workflow-Class: reconciliation trailer missing from verdict-emitted commit;\ngit log:\n%s", logOut)
	}
}

// TestExecuteVerdict_ReopenBead_WIPCaptureWrittenToVerdictEmittedCommit
// verifies that a reopen-bead verdict causes commitVerdictEmitted (step 3) to
// write WIP capture files from the target run's worktree into the
// .harmonik/reconciliation/<investigatorRunID>/wip-capture/ directory, which
// are then included in the verdict-emitted commit (RC-019).
//
// The test deliberately passes a nil BrAdapter so ExecuteVerdict fails at
// step 4 (reopen-bead requires BrAdapter). This is intentional: we assert
// step 3 (verdict-emitted commit + WIP capture) succeeds without needing a
// real Beads binary.
func TestExecuteVerdict_ReopenBead_WIPCaptureWrittenToVerdictEmittedCommit(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ve025aGitInit(t, projectDir)

	ve := ve025aValidVerdictEvent(core.VerdictReopenBead)
	ve025aSetupWorktree(t, projectDir, ve.InvestigatorRunID)
	ve.SnapshotToken.GitHeadHash = ve025aCurrentGitHead(t, projectDir)

	targetWTPath := ve025aWorktreePath(projectDir, ve.TargetRunID.String())
	if err := os.MkdirAll(targetWTPath, 0o755); err != nil {
		t.Fatalf("TestExecuteVerdict_ReopenBead: setup target worktree mkdir: %v", err)
	}
	ve025aGitInit(t, targetWTPath)
	readmePath := filepath.Join(targetWTPath, "README")
	if err := os.WriteFile(readmePath, []byte("test repo with WIP changes\n"), 0o644); err != nil {
		t.Fatalf("TestExecuteVerdict_ReopenBead: write WIP: %v", err)
	}

	lock := ve025aAcquireLock(t, projectDir, ve.TargetRunID.String())
	_, _ = daemon.ExecuteVerdict(context.Background(), ve, lock, ve025aCfg(projectDir, &ve025aRecordingEmitter{}))

	wtPath := ve025aWorktreePath(projectDir, ve.InvestigatorRunID.String())
	statusFile := filepath.Join(wtPath, ".harmonik", "reconciliation",
		ve.InvestigatorRunID.String(), "wip-capture", "git-status.txt")
	if _, err := os.Stat(statusFile); err != nil {
		t.Errorf("RC-019: %s not found in investigator worktree after reopen-bead "+
			"verdict-emitted commit: %v (reopen-bead verdicts must capture WIP)",
			statusFile, err)
	}
}

// TestExecuteVerdict_VerdictExecutedCommit_HasExecutedTrailer verifies that the
// verdict-executed commit carries the Harmonik-Verdict-Executed: true trailer
// per schemas.md §6.4.
//
// This trailer is the durable marker the Cat 3b detector (RC-026) reads to
// distinguish a fully-executed verdict from an unexecuted one.
func TestExecuteVerdict_VerdictExecutedCommit_HasExecutedTrailer(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ve025aGitInit(t, projectDir)

	ve := ve025aValidVerdictEvent(core.VerdictNoOpAccept)
	ve025aSetupWorktree(t, projectDir, ve.InvestigatorRunID)
	ve.SnapshotToken.GitHeadHash = ve025aCurrentGitHead(t, projectDir)

	lock := ve025aAcquireLock(t, projectDir, ve.TargetRunID.String())
	result, err := daemon.ExecuteVerdict(context.Background(), ve, lock, ve025aCfg(projectDir, &ve025aRecordingEmitter{}))
	if err != nil {
		t.Fatalf("ExecuteVerdict: %v", err)
	}
	if !result.Executed {
		t.Fatalf("Expected Executed=true; got %+v", result)
	}

	wtPath := ve025aWorktreePath(projectDir, ve.InvestigatorRunID.String())
	logOut := ve025aGitLog(t, wtPath)
	if !strings.Contains(logOut, "Harmonik-Verdict-Executed: true") {
		t.Errorf("Harmonik-Verdict-Executed: true trailer missing from verdict-executed commit;\ngit log:\n%s", logOut)
	}
}
