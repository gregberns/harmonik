package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workspace"
)

func TestPreflightDispatchReplayAllowsEmptyAuthorityWithoutReader(t *testing.T) {
	steps, err := preflightDispatchReplayWithReader(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("empty replay preflight = %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("empty replay preflight steps = %v", steps)
	}
}

func TestPreflightDispatchReplayReturnsActionForExecutor(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{
		intent.Binding.RunID.String(): replayPlanPreparedFacts(intent, dispatch.QueueOfferable),
	}}
	steps, err := preflightDispatchReplayWithReader(t.Context(), []dispatch.Intent{intent}, reader)
	if err != nil {
		t.Fatalf("replay preflight error = %v", err)
	}
	if len(steps) != 1 || steps[0].Action != dispatch.ReplayReservation {
		t.Fatalf("replay preflight steps = %+v", steps)
	}
}

func TestPreflightDispatchReplayFailsClosedBeforeExecutorOnRepair(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{
		intent.Binding.RunID.String(): replayPlanPreparedFacts(intent, dispatch.QueueConflict),
	}}
	_, err := preflightDispatchReplayWithReader(t.Context(), []dispatch.Intent{intent}, reader)
	if err == nil || !strings.Contains(err.Error(), "requires repair") {
		t.Fatalf("replay repair preflight error = %v", err)
	}
}

func TestStartupReconcileReplaysReservationBeforeOrphanSweep(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	writeReplayReaderIntent(t, projectDir, intent)
	writeReplayReaderQueue(t, projectDir, intent, false)
	brPath := filepath.Join(t.TempDir(), "br")
	callsPath := filepath.Join(t.TempDir(), "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> '` + callsPath + `'
if [ "$1" = "--version" ]; then
  echo "br test"
  exit 0
fi
printf '%s\n' '[{"id":"hk-replay-owner","title":"replay","issue_type":"task","status":"open"}]'
`
	if err := os.WriteFile(brPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(brPath, 0o700); err != nil { //nolint:gosec // The test fixture must be executable.
		t.Fatal(err)
	}
	bs := &bootState{cfg: Config{ProjectDir: projectDir, BrPath: brPath}}
	err := bs.runStartupReconcile(t.Context(), time.Now(), "main")
	if err == nil || !strings.Contains(err.Error(), "durable progress") {
		t.Fatalf("runStartupReconcile() error = %v", err)
	}
	assertReplayReservationQueue(t, projectDir, intent, 1)
	calls, readErr := os.ReadFile(callsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(calls), " list ") || strings.Contains(string(calls), "list --") {
		t.Fatalf("orphan sweep reached Beads list before replay refusal:\n%s", calls)
	}
}

func TestStartupReconcileInstallsLocationOwnedWorktreeObserver(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhaseRunDurable)
	writeReplayReaderIntent(t, projectDir, intent)
	writeReplayReaderQueue(t, projectDir, intent, true)
	record, err := runpkg.NewDispatchRecord(intent.Binding, time.Date(2026, 8, 15, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.CreateDispatchRecord(projectDir, record); err != nil {
		t.Fatal(err)
	}
	location := runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionLocalIndependent, RepositoryPath: intent.Binding.RepositoryPath,
	}
	located, err := record.BindLocation(location)
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.AdvanceDispatchRecord(projectDir, record, located); err != nil {
		t.Fatal(err)
	}
	brPath := filepath.Join(t.TempDir(), "br")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "br test"; exit 0; fi
if [ "$1" = "show" ]; then printf '%s\n' '[{"id":"hk-replay-owner","title":"replay","issue_type":"task","status":"in_progress"}]'; fi
exit 0
`
	if err := os.WriteFile(brPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(brPath, 0o700); err != nil { //nolint:gosec // The test fixture must be executable.
		t.Fatal(err)
	}
	bs := &bootState{cfg: Config{ProjectDir: projectDir, BrPath: brPath}}
	err = bs.runStartupReconcile(t.Context(), time.Now(), "main")
	if err == nil || !strings.Contains(err.Error(), "observe dispatch worktree") {
		t.Fatalf("runStartupReconcile() error = %v", err)
	}
	if strings.Contains(err.Error(), "observer is not configured") {
		t.Fatalf("production worktree observer was not installed: %v", err)
	}
}

func TestPreflightDispatchReplayInstallsProductionWorktreeProvisioner(t *testing.T) {
	projectDir := t.TempDir()
	parent := initReplayProvisionRepository(t, projectDir)
	intent := replayLocationIntent(t, projectDir)
	intent.Binding.ParentCommit = parent
	prepared, err := dispatch.NewPrepared(intent.Binding)
	var claimed dispatch.Intent
	if err == nil {
		claimed, err = prepared.WithClaimDurable()
	}
	if err == nil {
		intent, err = claimed.WithRunDurable()
	}
	if err != nil {
		t.Fatal(err)
	}
	store := dispatchstore.New(projectDir)
	if err := store.Create(prepared); err != nil {
		t.Fatal(err)
	}
	if err := store.Advance(prepared, claimed); err != nil {
		t.Fatal(err)
	}
	if err := store.Advance(claimed, intent); err != nil {
		t.Fatal(err)
	}
	writeReplayReaderQueue(t, projectDir, intent, true)
	record, err := runpkg.NewDispatchRecord(intent.Binding, time.Date(2026, 8, 15, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.CreateDispatchRecord(projectDir, record); err != nil {
		t.Fatal(err)
	}
	located, err := record.BindLocation(runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionLocalShared, RepositoryPath: projectDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runpkg.AdvanceDispatchRecord(projectDir, record, located); err != nil {
		t.Fatal(err)
	}
	bs := &bootState{cfg: Config{ProjectDir: projectDir}}
	st := &reconcileState{orphanStatusReader: &replayReaderBeads{
		record: replayFactBead(intent.Binding.BeadID, "in_progress"),
	}}
	err = bs.preflightDispatchReplay(t.Context(), st, []dispatch.Intent{intent})
	if err == nil || !strings.Contains(err.Error(), "durable progress") || strings.Contains(err.Error(), "provisioner is not configured") {
		t.Fatalf("preflightDispatchReplay() error = %v", err)
	}
	observed, err := workspace.ObserveDispatchWorktree(
		t.Context(), projectDir, intent.Binding.RunID.String(), workspace.NoWorktreeRootOverride(),
	)
	if err != nil || len(observed) != 1 || observed[0].HeadCommit != parent {
		t.Fatalf("created worktree = %+v, error %v", observed, err)
	}
}

func initReplayProvisionRepository(t *testing.T, projectDir string) string {
	t.Helper()
	commands := [][]string{
		{"init", "-q"},
		{"config", "user.email", "replay@example.invalid"},
		{"config", "user.name", "Replay Test"},
	}
	for _, args := range commands {
		if output, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", projectDir}, args...)...).CombinedOutput(); err != nil { //nolint:gosec // Test-owned path and fixed git arguments.
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(projectDir, "README.md"), []byte("replay\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-q", "-m", "base"}} {
		if output, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", projectDir}, args...)...).CombinedOutput(); err != nil { //nolint:gosec // Test-owned path and fixed git arguments.
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	output, err := exec.CommandContext(t.Context(), "git", "-C", projectDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func TestExecuteDispatchReplayPlanRejectsUnsupportedWaveBeforeWrite(t *testing.T) {
	projectDir := t.TempDir()
	first := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second.Binding.RunID[15]++
	writeReplayReaderQueue(t, projectDir, first, false)
	steps := []dispatchReplayStep{
		{Intent: first, Action: dispatch.ReplayReservation},
		{Intent: second, Action: dispatch.PrepareHandoff},
	}
	err := executeDispatchReplayPlan(t.Context(), steps, dispatchReplayExecutor{projectDir: projectDir})
	if err == nil || !strings.Contains(err.Error(), "does not support action") {
		t.Fatalf("execute replay plan error = %v", err)
	}
	durable, loadErr := queue.Load(t.Context(), projectDir, first.Binding.QueueName)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if item := durable.Groups[0].Items[0]; item.Status != queue.ItemStatusPending || item.RunID != nil || item.Attempts != 0 {
		t.Fatalf("unsupported wave changed queue item = %+v", item)
	}
}

func TestExecuteDispatchReplayPlanStopsAfterFirstDurableChange(t *testing.T) {
	projectDir := t.TempDir()
	first := replayOwnershipIntent(t, dispatch.PhaseClaimDurable)
	second := replayOwnershipIntent(t, dispatch.PhaseClaimDurable)
	second.Binding.RunID = mustReplayRunID(t, "0197d100-0000-7000-8000-000000000041")
	second.Binding.ClaimTransitionID = mustReplayTransitionID(t, "0197d100-0000-7000-8000-000000000042")
	second.Binding.ItemIndex = 1
	second.Binding.BeadID = "hk-replay-second"
	for _, intent := range []dispatch.Intent{first, second} {
		prepared, err := dispatch.NewPrepared(intent.Binding)
		if err != nil {
			t.Fatal(err)
		}
		if err := dispatchstore.New(projectDir).Create(prepared); err != nil {
			t.Fatal(err)
		}
		if err := dispatchstore.New(projectDir).Advance(prepared, intent); err != nil {
			t.Fatal(err)
		}
	}
	steps := []dispatchReplayStep{
		{Intent: first, Action: dispatch.AdvanceRunPhase},
		{Intent: second, Action: dispatch.AdvanceRunPhase},
	}
	if err := executeDispatchReplayPlan(t.Context(), steps, dispatchReplayExecutor{projectDir: projectDir}); err != nil {
		t.Fatal(err)
	}
	store := dispatchstore.New(projectDir)
	firstGot, err := store.Load(first.Binding.RunID)
	if err != nil {
		t.Fatal(err)
	}
	secondGot, err := store.Load(second.Binding.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if firstGot.Phase != dispatch.PhaseRunDurable || secondGot.Phase != dispatch.PhaseClaimDurable {
		t.Fatalf("phases after one pass = (%q, %q)", firstGot.Phase, secondGot.Phase)
	}
}

func TestStartupReconcileReplaysExactClaimBeforeOrphanSweep(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	writeReplayReaderIntent(t, projectDir, intent)
	writeReplayReaderQueue(t, projectDir, intent, true)
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "beads-intents"), 0o700); err != nil {
		t.Fatal(err)
	}
	brPath := filepath.Join(t.TempDir(), "br")
	callsPath := filepath.Join(t.TempDir(), "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> '` + callsPath + `'
if [ "$1" = "--version" ]; then
  echo "br test"
  exit 0
fi
if [ "$1" = "show" ]; then
  printf '%s\n' '[{"id":"hk-replay-owner","title":"replay","issue_type":"task","status":"open"}]'
fi
exit 0
`
	if err := os.WriteFile(brPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(brPath, 0o700); err != nil { //nolint:gosec // The test fixture must be executable.
		t.Fatal(err)
	}
	bs := &bootState{cfg: Config{ProjectDir: projectDir, BrPath: brPath}}
	err := bs.runStartupReconcile(t.Context(), time.Now(), "main")
	if err == nil || !strings.Contains(err.Error(), "durable progress") {
		t.Fatalf("runStartupReconcile() error = %v", err)
	}
	got, loadErr := dispatchstore.New(projectDir).Load(intent.Binding.RunID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if got.Phase != dispatch.PhaseClaimDurable {
		t.Fatalf("replayed claim intent = %+v", got)
	}
	calls, readErr := os.ReadFile(callsPath) //nolint:gosec // Test-owned path below t.TempDir.
	if readErr != nil {
		t.Fatal(readErr)
	}
	wantClaim := "update " + string(intent.Binding.BeadID) + " --claim"
	if !strings.Contains(string(calls), wantClaim) {
		t.Fatalf("claim call %q missing:\n%s", wantClaim, calls)
	}
	if strings.Contains(string(calls), " list ") || strings.Contains(string(calls), "list --") {
		t.Fatalf("orphan sweep reached Beads list after claim:\n%s", calls)
	}
}

func TestStartupReconcileWritesUniversalRunRecordBeforeOrphanSweep(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhaseClaimDurable)
	writeReplayReaderIntent(t, projectDir, intent)
	writeReplayReaderQueue(t, projectDir, intent, true)
	brPath := filepath.Join(t.TempDir(), "br")
	callsPath := filepath.Join(t.TempDir(), "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> '` + callsPath + `'
if [ "$1" = "--version" ]; then
  echo "br test"
  exit 0
fi
if [ "$1" = "show" ]; then
  printf '%s\n' '[{"id":"hk-replay-owner","title":"replay","issue_type":"task","status":"in_progress"}]'
fi
exit 0
`
	if err := os.WriteFile(brPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(brPath, 0o700); err != nil { //nolint:gosec // The test fixture must be executable.
		t.Fatal(err)
	}
	bs := &bootState{cfg: Config{ProjectDir: projectDir, BrPath: brPath}}
	err := bs.runStartupReconcile(t.Context(), time.Now(), "main")
	if err == nil || !strings.Contains(err.Error(), "durable progress") {
		t.Fatalf("runStartupReconcile() error = %v", err)
	}
	records, scanErr := runpkg.ScanRegistry(projectDir)
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	if len(records.Dispatch) != 1 || records.Dispatch[0].RunID != intent.Binding.RunID ||
		records.Dispatch[0].ClaimTransitionID != intent.Binding.ClaimTransitionID {
		t.Fatalf("startup dispatch records = %+v", records.Dispatch)
	}
	calls, readErr := os.ReadFile(callsPath) //nolint:gosec // Test-owned path below t.TempDir.
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(calls), " list ") || strings.Contains(string(calls), "list --") {
		t.Fatalf("orphan sweep reached Beads list after run record:\n%s", calls)
	}
}
