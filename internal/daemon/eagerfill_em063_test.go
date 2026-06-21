package daemon

// eagerfill_em063_test.go — unit tests for the EM-063 pre-screen and
// provenance guard in the eager-refill path.
//
// Observable behaviours covered:
//
//  1. Phase 1: a bead already present in the queue with pending/dispatched/
//     completed/failed status is excluded from survivors.
//
//  2. Phase 2: beadLandedOnOriginMain returns (false, "", nil) when the
//     git working directory does not contain a remote tracking branch —
//     the call does not crash and treats the bead as not-landed.
//
//  3. kerfNextBeads returns an error when the kerf binary path is absent.
//
//  4. eagerRefillEval returns immediately (no panic) when kerfPath is empty.
//
//  5. eagerRefillEval returns immediately when queueStore is nil.
//
// Spec ref: specs/execution-model.md §4.13 EM-063.
// Bead ref: hk-9321v.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// em063FixtureStreamQueueWithBeads builds an active stream queue that has
// the given bead IDs as dispatched or pending items.  The returned queue has
// one group (index 0) in active state.
func em063FixtureStreamQueueWithBeads(beadIDs ...string) *queue.Queue {
	now := time.Now().UTC()
	items := make([]queue.Item, 0, len(beadIDs))
	for i, id := range beadIDs {
		status := queue.ItemStatusPending
		if i%2 == 0 {
			status = queue.ItemStatusDispatched
		}
		items = append(items, queue.Item{
			BeadID: core.BeadID(id),
			Status: status,
		})
	}
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "em063-test-queue",
		Status:        queue.QueueStatusActive,
		SubmittedAt:   now,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items:      items,
			},
		},
	}
}

// em063FixtureDeps builds a minimal workLoopDeps with only the fields
// required by preScreenCandidates and eagerRefillEval.  kerfPath is left
// empty (no eager-refill) unless overridden by the caller.
func em063FixtureDeps(t *testing.T, qs *QueueStore) workLoopDeps {
	t.Helper()
	return workLoopDeps{
		queueStore:    qs,
		kerfPath:      "",
		projectDir:    t.TempDir(),
		maxConcurrent: 4,
		runRegistry:   newLocalRunRegistry(),
		bus:           &noopEmitter{},
		queueLedger:   nil,
	}
}

// noopEmitter satisfies handlercontract.EventEmitter for test stubs that do
// not need event inspection.
type noopEmitter struct{}

func (n *noopEmitter) Emit(_ context.Context, _ core.EventType, _ []byte) error { return nil }
func (n *noopEmitter) EmitWithRunID(_ context.Context, _ core.RunID, _ core.EventType, _ []byte) error {
	return nil
}

// ---------------------------------------------------------------------------
// Phase 1: already-in-queue guard
// ---------------------------------------------------------------------------

// TestEM063_Phase1_AlreadyInQueue_PendingExcluded verifies that a bead present
// in the active queue with ItemStatusPending is excluded from pre-screen
// survivors (EM-063 Phase 1).
func TestEM063_Phase1_AlreadyInQueue_PendingExcluded(t *testing.T) {
	t.Parallel()

	qs := newQueueStore()
	q := em063FixtureStreamQueueWithBeads("hk-inqueue-01", "hk-inqueue-02")
	qs.SetQueue(q)

	deps := em063FixtureDeps(t, qs)

	candidates := []core.BeadID{"hk-inqueue-01", "hk-inqueue-02", "hk-new-bead"}
	survivors := preScreenCandidates(context.Background(), deps, candidates, "em063-test-queue")

	// Only the bead NOT already in the queue should survive Phase 1.
	if len(survivors) != 1 {
		t.Fatalf("Phase 1: survivors = %v, want [hk-new-bead]", survivors)
	}
	if survivors[0] != "hk-new-bead" {
		t.Errorf("Phase 1: survivors[0] = %q, want 'hk-new-bead'", survivors[0])
	}
}

// TestEM063_Phase1_AlreadyInQueue_DispatchedExcluded verifies that a bead
// present with ItemStatusDispatched is also excluded (EM-063 Phase 1
// covers pending, dispatched, completed, and failed).
func TestEM063_Phase1_AlreadyInQueue_DispatchedExcluded(t *testing.T) {
	t.Parallel()

	qs := newQueueStore()
	now := time.Now().UTC()
	runID := "019e0000-0000-7000-0000-000000000001"
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "em063-dispatched-queue",
		Status:        queue.QueueStatusActive,
		SubmittedAt:   now,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{
				{BeadID: "hk-dispatched", Status: queue.ItemStatusDispatched, RunID: &runID},
			},
		}},
	}
	qs.SetQueue(q)

	deps := em063FixtureDeps(t, qs)

	candidates := []core.BeadID{"hk-dispatched", "hk-fresh"}
	survivors := preScreenCandidates(context.Background(), deps, candidates, "em063-dispatched-queue")

	if len(survivors) != 1 || survivors[0] != "hk-fresh" {
		t.Errorf("Phase 1: survivors = %v, want [hk-fresh]", survivors)
	}
}

// TestEM063_Phase1_EmptyQueueAllSurvive verifies that when no queue is loaded
// all candidates pass Phase 1 (no in-queue entries to exclude).
func TestEM063_Phase1_EmptyQueueAllSurvive(t *testing.T) {
	t.Parallel()

	deps := em063FixtureDeps(t, newQueueStore())

	candidates := []core.BeadID{"hk-a", "hk-b", "hk-c"}
	// Phase 2 git check will not find anything (temp dir has no git history).
	survivors := preScreenCandidates(context.Background(), deps, candidates, "no-queue")

	if len(survivors) != 3 {
		t.Errorf("Phase 1 with empty queue: survivors = %v, want all 3 candidates", survivors)
	}
}

// ---------------------------------------------------------------------------
// Phase 2: already-landed git guard
// ---------------------------------------------------------------------------

// TestEM063_Phase2_BeadLandedOnOriginMain_MissingRemote verifies that
// beadLandedOnOriginMain returns (false, "", nil) when the project directory
// is a git repo with no origin/main remote tracking branch.  This models the
// most common CI/test environment where origin/main doesn't exist yet.
func TestEM063_Phase2_BeadLandedOnOriginMain_MissingRemote(t *testing.T) {
	t.Parallel()

	// Use a temp dir with an empty git repo.
	dir := t.TempDir()
	// initialise a bare git repo so `git log` has something to work with.
	if out, err := runSimpleCmd("git", "-C", dir, "init"); err != nil {
		t.Skipf("git init failed: %v (%s)", err, out)
	}

	found, sha, err := beadLandedOnOriginMain(context.Background(), dir, "hk-test-bead")
	if err != nil {
		t.Fatalf("beadLandedOnOriginMain: unexpected error: %v", err)
	}
	if found {
		t.Errorf("beadLandedOnOriginMain: found = true, want false (no origin/main)")
	}
	if sha != "" {
		t.Errorf("beadLandedOnOriginMain: sha = %q, want empty", sha)
	}
}

// runSimpleCmd is a test helper that runs a command and returns (output, error).
func runSimpleCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ---------------------------------------------------------------------------
// kerfNextBeads — binary-absent error path
// ---------------------------------------------------------------------------

// TestEM063_KerfNextBeads_BinaryAbsent verifies that kerfNextBeads returns an
// error when the kerf binary path does not exist (EM-062 relies on this to
// detect a non-installed kerf).
func TestEM063_KerfNextBeads_BinaryAbsent(t *testing.T) {
	t.Parallel()

	_, err := kerfNextBeads(context.Background(), "/nonexistent/kerf-binary", 4)
	if err == nil {
		t.Fatal("kerfNextBeads with absent binary: expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// eagerRefillEval — guard gates
// ---------------------------------------------------------------------------

// TestEM063_EagerRefillEval_NoopWhenKerfPathEmpty verifies that
// eagerRefillEval returns immediately (no panic, no queue mutation) when
// kerfPath is empty — the "kerf not installed" fast-path.
func TestEM063_EagerRefillEval_NoopWhenKerfPathEmpty(t *testing.T) {
	t.Parallel()

	qs := newQueueStore()
	q := em063FixtureStreamQueueWithBeads("hk-existing")
	qs.SetQueue(q)

	deps := em063FixtureDeps(t, qs)
	deps.kerfPath = "" // kerf not installed

	// Must not panic, must not mutate queue.
	eagerRefillEval(context.Background(), deps)

	// Queue should be unchanged.
	got := qs.Queue()
	if got == nil || len(got.Groups[0].Items) != 1 {
		t.Error("eagerRefillEval with empty kerfPath mutated the queue; expected no-op")
	}
}

// TestEM063_EagerRefillEval_NoopWhenQueueStoreNil verifies that
// eagerRefillEval returns immediately when queueStore is nil.
func TestEM063_EagerRefillEval_NoopWhenQueueStoreNil(t *testing.T) {
	t.Parallel()

	deps := em063FixtureDeps(t, nil)
	deps.kerfPath = "/some/kerf" // set a kerf path to get past the first guard
	deps.queueStore = nil

	// Must not panic.
	eagerRefillEval(context.Background(), deps)
}

// ---------------------------------------------------------------------------
// stagedBeadGeneratorEval (flywheel V9 §5.4 B, hk-f722)
// ---------------------------------------------------------------------------

// writeTestFile writes content to path, creating parent directories as needed.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("writeTestFile: mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTestFile: write: %v", err)
	}
}

// writePhase2Config writes a minimal .harmonik/config.yaml with one Phase-2
// class entry: class → verifyCmd.
func writePhase2Config(t *testing.T, projectDir, class, verifyCmd string) {
	t.Helper()
	content := "sentinel:\n  done_definition:\n    " + class + ": \"" + verifyCmd + "\"\n"
	writeTestFile(t, filepath.Join(projectDir, ".harmonik", "config.yaml"), content)
}

// writeFakeBrScript creates an executable shell script at scriptPath that
// appends its first two arguments (subcommand + title) to argsFile and exits 0.
// The full description is NOT written to avoid newline-splitting in counts.
func writeFakeBrScript(t *testing.T, scriptPath, argsFile string) {
	t.Helper()
	// Write only the first arg ($1) and the second arg ($2) so that
	// multi-line descriptions in later args do not create spurious "lines".
	script := "#!/bin/sh\nprintf 'CALL %s %s\\n' \"$1\" \"$2\" >> " + argsFile + "\n"
	writeTestFile(t, scriptPath, script)
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("writeFakeBrScript: chmod: %v", err)
	}
}

// writeFakeBrArgScript creates an executable shell script at scriptPath that
// appends ALL arguments (joined by space) to argsFile.  Use this variant only
// when the test needs to inspect specific flags; note that newlines embedded in
// arguments will appear verbatim in the file.
func writeFakeBrArgScript(t *testing.T, scriptPath, argsFile string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + argsFile + "\n"
	writeTestFile(t, scriptPath, script)
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("writeFakeBrArgScript: chmod: %v", err)
	}
}

// stagedBeadFixtureDeps builds a workLoopDeps for stagedBeadGeneratorEval
// tests with the given brPath wired in.
func stagedBeadFixtureDeps(t *testing.T, projectDir, brPath string) workLoopDeps {
	t.Helper()
	deps := em063FixtureDeps(t, nil)
	deps.projectDir = projectDir
	deps.brPath = brPath
	deps.followUpLedger = make(map[string]struct{})
	deps.followUpLedgerMu = new(sync.Mutex)
	return deps
}

// TestStagedBeadGenerator_NoopWhenBrPathEmpty verifies guardrail: empty brPath
// makes stagedBeadGeneratorEval a no-op (generator disabled).
func TestStagedBeadGenerator_NoopWhenBrPathEmpty(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy")

	deps := stagedBeadFixtureDeps(t, projectDir, "")
	// Must not panic and must not call br (no file to write to since brPath is empty).
	stagedBeadGeneratorEval(context.Background(), deps, "hk-abc", []string{"deploy"})
}

// TestStagedBeadGenerator_NoopWhenNoPhase2Classes verifies guardrail 1:
// if sentinel has no Phase-2 classes, nothing is created.
func TestStagedBeadGenerator_NoopWhenNoPhase2Classes(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	// Config with only "merged" (default): no Phase-2 classes.
	writeTestFile(t, filepath.Join(projectDir, ".harmonik", "config.yaml"),
		"sentinel:\n  done_definition:\n    myclass: merged\n")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEval(context.Background(), deps, "hk-abc", []string{"myclass"})

	// argsFile must not exist (br was never called).
	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("br was called despite no Phase-2 classes; expected no-op")
	}
}

// TestStagedBeadGenerator_NoopWhenLabelsMismatch verifies guardrail 1:
// if the completed bead has no labels matching any Phase-2 class, nothing is created.
func TestStagedBeadGenerator_NoopWhenLabelsMismatch(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	// Labels: "bugfix", "chore" — neither matches "deploy".
	stagedBeadGeneratorEval(context.Background(), deps, "hk-abc", []string{"bugfix", "chore"})

	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("br was called despite no matching Phase-2 label; expected no-op")
	}
}

// TestStagedBeadGenerator_CreatesBead verifies that a matching bead causes
// br create to be called with correct arguments (guardrail 2: --status open).
func TestStagedBeadGenerator_CreatesBead(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy-prod")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrArgScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEval(context.Background(), deps, "hk-xyz", []string{"deploy", "other"})

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("br was not called; expected a follow-up bead to be created: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if !strings.Contains(line, "create") {
		t.Errorf("br args missing 'create': %q", line)
	}
	if !strings.Contains(line, "hk-xyz") {
		t.Errorf("br args missing completed bead ID 'hk-xyz': %q", line)
	}
	if !strings.Contains(line, "--status") || !strings.Contains(line, "open") {
		t.Errorf("br args missing '--status open' (guardrail 2 land-open): %q", line)
	}
}

// TestStagedBeadGenerator_AppliesNeedsGreenlightLabel verifies that the created
// bead carries the "needs-greenlight" label (AC2 greenlight gate, hk-lacr).
// The label is the structural block that prevents daemon auto-dispatch until a
// captain explicitly clears it via `harmonik greenlight <bead-id>`.
func TestStagedBeadGenerator_AppliesNeedsGreenlightLabel(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy-prod")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrArgScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEval(context.Background(), deps, "hk-gltest", []string{"deploy"})

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("br was not called: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if !strings.Contains(line, labelNeedsGreenlight) {
		t.Errorf("br args missing %q (AC2 greenlight gate): %q", labelNeedsGreenlight, line)
	}
}

// TestStagedBeadGenerator_AtMostOnce verifies guardrail 4: a second call with
// the same (beadID, class) is a no-op; br is only invoked once.
func TestStagedBeadGenerator_AtMostOnce(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy-prod")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	// First call: should create the bead.
	stagedBeadGeneratorEval(context.Background(), deps, "hk-xyz", []string{"deploy"})
	// Second call with the same bead + class: must be a no-op.
	stagedBeadGeneratorEval(context.Background(), deps, "hk-xyz", []string{"deploy"})

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("br was not called on first invocation: %v", err)
	}
	// writeFakeBrScript writes "CALL <subcmd> <title>\n" per invocation.
	var callCount int
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CALL ") {
			callCount++
		}
	}
	if callCount != 1 {
		t.Errorf("br was called %d times; want exactly 1 (at-most-once guardrail)", callCount)
	}
}

// TestStagedBeadGenerator_DurableLedger_SkipsOnPreseededKey verifies that
// when the in-memory ledger is pre-seeded (simulating a daemon restart that
// loaded a durable ledger from disk), a subsequent call with the same
// (beadID, class) key is a no-op — br create is NOT called.
func TestStagedBeadGenerator_DurableLedger_SkipsOnPreseededKey(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy-prod")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	// Simulate a restart: pre-seed the in-memory ledger as the boot-seed does.
	deps.followUpLedger["hk-xyz:deploy"] = struct{}{}

	// This call must be a no-op because the key is already in the ledger.
	stagedBeadGeneratorEval(context.Background(), deps, "hk-xyz", []string{"deploy"})

	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("br was called despite key being pre-seeded in ledger (durable restart guard)")
	}
}

// TestStagedBeadGenerator_DurableLedger_PersistsToDisk verifies that a
// successful br create causes the ledger key to be appended to the disk file,
// and that re-loading the file restores the key (AC1 durability contract).
func TestStagedBeadGenerator_DurableLedger_PersistsToDisk(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy-prod")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	ledgerPath := filepath.Join(tmp, followUpLedgerFileName)
	deps.followUpLedgerPath = ledgerPath

	stagedBeadGeneratorEval(context.Background(), deps, "hk-persist", []string{"deploy"})

	// br must have been called.
	if _, statErr := os.Stat(argsFile); statErr != nil {
		t.Fatalf("br was not called: %v", statErr)
	}

	// The key must be on disk.
	ledger, err := loadFollowUpLedger(ledgerPath)
	if err != nil {
		t.Fatalf("loadFollowUpLedger: %v", err)
	}
	if _, ok := ledger["hk-persist:deploy"]; !ok {
		t.Errorf("key 'hk-persist:deploy' missing from disk ledger after successful create; got %v", ledger)
	}
}

// TestStagedBeadGenerator_DurableLedger_NoopWhenAtCeiling verifies guardrail 3: when
// in-flight run count == maxConcurrent the generator skips bead creation.
func TestStagedBeadGenerator_NoopWhenAtCeiling(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy-prod")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	deps.maxConcurrent = 1

	// Register a fake in-flight run to saturate the ceiling.
	deps.runRegistry.Register(core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000099")), &RunHandle{
		BeadID:    core.BeadID("hk-other"),
		StartedAt: time.Now(),
	})

	stagedBeadGeneratorEval(context.Background(), deps, "hk-xyz", []string{"deploy"})

	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("br was called at WIP==max_concurrent; expected no-op (guardrail 3)")
	}
}

// ---------------------------------------------------------------------------
// stagedBeadGeneratorEval — §6.2 provenance guard (hk-zlwq)
// ---------------------------------------------------------------------------

// stagedBeadGitFixture initialises a git repository with an origin remote in dir.
// When refBeadID is non-empty, a commit with "Refs: <refBeadID>" is added to main
// and pushed to origin so that beadOnOriginMain returns true for that bead.
func stagedBeadGitFixture(t *testing.T, dir, refBeadID string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("stagedBeadGitFixture: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("test\n"), 0o644); err != nil {
		t.Fatalf("stagedBeadGitFixture: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	if refBeadID != "" {
		if err := os.WriteFile(filepath.Join(dir, "marker"), []byte(refBeadID+"\n"), 0o644); err != nil {
			t.Fatalf("stagedBeadGitFixture: WriteFile marker: %v", err)
		}
		run("add", "marker")
		run("commit", "-m", "work\n\nRefs: "+refBeadID)
	}
	originDir := t.TempDir()
	//nolint:gosec // G204: git args are test-internal literals
	initCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("stagedBeadGitFixture: git init --bare: %v\n%s", err, out)
	}
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")
}

// TestStagedBeadGenerator_NoopWhenProvenanceAbsent verifies the §6.2 provenance
// guard: when targetBranch is set but origin/<targetBranch> has no "Refs: <beadID>"
// commit, the generator is a no-op even if runSucceeded was true.
func TestStagedBeadGenerator_NoopWhenProvenanceAbsent(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy")
	stagedBeadGitFixture(t, projectDir, "") // no Refs: hk-xyz on origin/main

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	deps.targetBranch = "main"
	stagedBeadGeneratorEval(context.Background(), deps, "hk-xyz", []string{"deploy"})

	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("br was called despite Refs: hk-xyz absent from origin/main; want no-op (§6.2 provenance guard)")
	}
}

// TestStagedBeadGenerator_FiresWhenProvenancePresent verifies the §6.2 provenance
// guard positive path: when origin/<targetBranch> contains "Refs: <beadID>", the
// generator creates the follow-up bead normally.
func TestStagedBeadGenerator_FiresWhenProvenancePresent(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy")
	stagedBeadGitFixture(t, projectDir, "hk-xyz") // Refs: hk-xyz is on origin/main

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	deps.targetBranch = "main"
	stagedBeadGeneratorEval(context.Background(), deps, "hk-xyz", []string{"deploy"})

	if _, statErr := os.Stat(argsFile); statErr != nil {
		t.Errorf("br was not called despite Refs: hk-xyz present on origin/main: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// B5-7: WIP == maxConcurrent-1 off-by-one (one slot free → DOES fire)
// ---------------------------------------------------------------------------

// TestStagedBeadGenerator_FiresAtMaxMinusOne verifies the WIP ceiling off-by-one:
// when in-flight == maxConcurrent-1 (one slot free), the generator DOES fire.
// This is the boundary complement of TestStagedBeadGenerator_NoopWhenAtCeiling.
func TestStagedBeadGenerator_FiresAtMaxMinusOne(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy-prod")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	deps.maxConcurrent = 3

	// Register 2 in-flight runs: Len() == 2 == maxConcurrent-1 → one slot free.
	deps.runRegistry.Register(core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000001")), &RunHandle{
		BeadID:    core.BeadID("hk-inflight-1"),
		StartedAt: time.Now(),
	})
	deps.runRegistry.Register(core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000002")), &RunHandle{
		BeadID:    core.BeadID("hk-inflight-2"),
		StartedAt: time.Now(),
	})

	stagedBeadGeneratorEval(context.Background(), deps, "hk-xyz", []string{"deploy"})

	if _, statErr := os.Stat(argsFile); statErr != nil {
		t.Errorf("br was NOT called at WIP==maxConcurrent-1; expected creation (one slot free): %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// B5-8: multi-class / multi-bead ledger keys
// ---------------------------------------------------------------------------

// TestStagedBeadGenerator_MultiClassSameBead_DifferentLedgerKeys verifies that
// two calls with the same completed bead but different Phase-2 class labels
// produce two separate ledger keys (beadID:classA and beadID:classB) and
// trigger two br create calls.
func TestStagedBeadGenerator_MultiClassSameBead_DifferentLedgerKeys(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	// Config with two Phase-2 classes.
	writeTestFile(t, filepath.Join(projectDir, ".harmonik", "config.yaml"),
		"sentinel:\n  done_definition:\n    deploy: make deploy\n    smoke: make smoke\n")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	// First call: matches "deploy" (first label in the slice that is a Phase-2 class).
	stagedBeadGeneratorEval(context.Background(), deps, "hk-abc", []string{"deploy"})
	// Second call: matches "smoke".
	stagedBeadGeneratorEval(context.Background(), deps, "hk-abc", []string{"smoke"})

	// Both ledger keys must be present.
	deps.followUpLedgerMu.Lock()
	_, hasDeployKey := deps.followUpLedger["hk-abc:deploy"]
	_, hasSmokeKey := deps.followUpLedger["hk-abc:smoke"]
	deps.followUpLedgerMu.Unlock()

	if !hasDeployKey {
		t.Error("ledger missing key 'hk-abc:deploy'")
	}
	if !hasSmokeKey {
		t.Error("ledger missing key 'hk-abc:smoke'")
	}

	// br should have been called twice (once per class).
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("br-args file missing: %v", err)
	}
	var callCount int
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CALL ") {
			callCount++
		}
	}
	if callCount != 2 {
		t.Errorf("expected 2 br create calls (one per class), got %d", callCount)
	}
}

// TestStagedBeadGenerator_MultiBeadSameClass_DifferentLedgerKeys verifies that
// two different completed beads with the same Phase-2 class label produce two
// separate ledger keys (beadA:class and beadB:class) and trigger two br create calls.
func TestStagedBeadGenerator_MultiBeadSameClass_DifferentLedgerKeys(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy-prod")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEval(context.Background(), deps, "hk-alpha", []string{"deploy"})
	stagedBeadGeneratorEval(context.Background(), deps, "hk-beta", []string{"deploy"})

	deps.followUpLedgerMu.Lock()
	_, hasAlpha := deps.followUpLedger["hk-alpha:deploy"]
	_, hasBeta := deps.followUpLedger["hk-beta:deploy"]
	deps.followUpLedgerMu.Unlock()

	if !hasAlpha {
		t.Error("ledger missing key 'hk-alpha:deploy'")
	}
	if !hasBeta {
		t.Error("ledger missing key 'hk-beta:deploy'")
	}

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("br-args file missing: %v", err)
	}
	var callCount int
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CALL ") {
			callCount++
		}
	}
	if callCount != 2 {
		t.Errorf("expected 2 br create calls (one per bead), got %d", callCount)
	}
}
