package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runloop"
)

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
		QueueID:       newTestQueueID(),
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

func em063FixtureDeps(t *testing.T, qs *queuewiring.QueueStore) testRuntime {
	t.Helper()
	return testRuntime{
		queueStore:   qs,
		env:          runloop.RunEnv{ProjectDir: t.TempDir()},
		ports:        runloop.RunPorts{Emitter: &noopEmitter{}},
		capacity:     newCapacityPort(4, nil),
		queueSurface: newQueueSurfacePort(nil, nil),
		runRegistry:  newLocalRunRegistry(),
	}
}

func TestEagerRefillPort_LoadsDurableLedgerAtBoot(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	port := newEagerRefillPort(Config{ProjectDir: projectDir, KerfPath: "/tools/kerf"})
	wantPath := filepath.Join(projectDir, ".harmonik", followUpLedgerFileName)
	if port.kerfPath != "/tools/kerf" {
		t.Fatalf("kerfPath = %q, want config value", port.kerfPath)
	}
	if port.followUpLedgerPath != wantPath {
		t.Fatalf("followUpLedgerPath = %q, want %q", port.followUpLedgerPath, wantPath)
	}
	if port.followUpLedger == nil || port.followUpLedgerMu == nil {
		t.Fatal("new eager-refill port did not initialise the in-memory ledger")
	}
	if err := os.MkdirAll(filepath.Dir(port.followUpLedgerPath), 0o755); err != nil { //nolint:gosec // Test creates its own temporary ledger directory.
		t.Fatalf("MkdirAll ledger dir: %v", err)
	}
	if err := appendFollowUpLedger(port.followUpLedgerPath, "hk-restart:deploy"); err != nil {
		t.Fatalf("appendFollowUpLedger: %v", err)
	}

	resumed := newEagerRefillPort(Config{ProjectDir: projectDir, KerfPath: "/tools/kerf"})
	loadEagerRefillLedger(&resumed)
	if _, ok := resumed.followUpLedger["hk-restart:deploy"]; !ok {
		t.Fatal("boot ledger load did not restore the staged follow-up key")
	}
}

func TestRunCompletionPort_CarriesEagerRefillValues(t *testing.T) {
	t.Parallel()

	ledger := make(map[string]struct{})
	eager := eagerRefillPort{
		kerfPath:           "/tools/kerf",
		followUpLedger:     ledger,
		followUpLedgerMu:   new(sync.Mutex),
		followUpLedgerPath: "/project/.harmonik/follow-up-ledger.jsonl",
	}
	completion := newRunCompletionPort("/tools/br", newReapSeamPort(nil, "", "", nil, nil, loopLifecyclePort{}, capacityPort{}, queueSurfacePort{}, eager))
	if completion.brPath != "/tools/br" || completion.eagerRefill.kerfPath != eager.kerfPath {
		t.Fatal("completion port did not retain its command and eager-refill values")
	}
	completion.eagerRefill.followUpLedger["hk-complete:deploy"] = struct{}{}
	if _, ok := ledger["hk-complete:deploy"]; !ok {
		t.Fatal("completion port did not retain the eager-refill ledger by value")
	}
}

type noopEmitter struct{}

func (n *noopEmitter) Emit(_ context.Context, _ core.EventType, _ []byte) error { return nil }
func (n *noopEmitter) EmitWithRunID(_ context.Context, _ core.RunID, _ core.EventType, _ []byte) error {
	return nil
}

// TestEM063_Phase1_AlreadyInQueue_PendingExcluded verifies that a bead present
// in the active queue with ItemStatusPending is excluded from pre-screen
// survivors (EM-063 Phase 1).
func TestEM063_Phase1_AlreadyInQueue_PendingExcluded(t *testing.T) {
	t.Parallel()

	qs := queuewiring.NewQueueStore()
	q := em063FixtureStreamQueueWithBeads("hk-inqueue-01", "hk-inqueue-02")
	qs.SetQueue(q)

	deps := em063FixtureDeps(t, qs)

	candidates := []core.BeadID{"hk-inqueue-01", "hk-inqueue-02", "hk-new-bead"}
	survivors := preScreenCandidates(context.Background(), deps.reap(eagerRefillPort{}), candidates)

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

	qs := queuewiring.NewQueueStore()
	now := time.Now().UTC()
	runID := "019e0000-0000-7000-0000-000000000001"
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
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
	survivors := preScreenCandidates(context.Background(), deps.reap(eagerRefillPort{}), candidates)

	if len(survivors) != 1 || survivors[0] != "hk-fresh" {
		t.Errorf("Phase 1: survivors = %v, want [hk-fresh]", survivors)
	}
}

// TestEM063_Phase1_EmptyQueueAllSurvive verifies that when no queue is loaded
// all candidates pass Phase 1 (no in-queue entries to exclude).
func TestEM063_Phase1_EmptyQueueAllSurvive(t *testing.T) {
	t.Parallel()

	deps := em063FixtureDeps(t, queuewiring.NewQueueStore())

	candidates := []core.BeadID{"hk-a", "hk-b", "hk-c"}
	survivors := preScreenCandidates(context.Background(), deps.reap(eagerRefillPort{}), candidates)

	if len(survivors) != 3 {
		t.Errorf("Phase 1 with empty queue: survivors = %v, want all 3 candidates", survivors)
	}
}

// TestEM063_Phase2_BeadLandedOnOriginMain_MissingRemote verifies that
// beadLandedOnOriginMain returns (false, "", nil) when the project directory
// is a git repo with no origin/main remote tracking branch.  This models the
// most common CI/test environment where origin/main doesn't exist yet.
func TestEM063_Phase2_BeadLandedOnOriginMain_MissingRemote(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if out, err := runSimpleCmd("git", "-C", dir, "init"); err != nil {
		t.Skipf("git init failed: %v (%s)", err, out)
	}

	found, sha, err := beadLandedOnOriginMain(context.Background(), dir, "main", "hk-test-bead")
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

func runSimpleCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec
	out, err := cmd.CombinedOutput()
	return string(out), err
}

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

// TestEM063_EagerRefillEval_NoopWhenKerfPathEmpty verifies that
// eagerRefillEval returns immediately (no panic, no queue mutation) when
// kerfPath is empty — the "kerf not installed" fast-path.
func TestEM063_EagerRefillEval_NoopWhenKerfPathEmpty(t *testing.T) {
	t.Parallel()

	qs := queuewiring.NewQueueStore()
	q := em063FixtureStreamQueueWithBeads("hk-existing")
	qs.SetQueue(q)

	deps := em063FixtureDeps(t, qs)

	eagerRefillEval(context.Background(), deps.reap(eagerRefillPort{}))

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
	deps.queueStore = nil

	eagerRefillEval(context.Background(), deps.reap(eagerRefillPort{kerfPath: "/some/kerf"}))
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("writeTestFile: mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTestFile: write: %v", err)
	}
}

func writePhase2Config(t *testing.T, projectDir, class, verifyCmd string) {
	t.Helper()
	content := "sentinel:\n  done_definition:\n    " + class + ": \"" + verifyCmd + "\"\n"
	writeTestFile(t, filepath.Join(projectDir, ".harmonik", "config.yaml"), content)
}

func writeFakeBrScript(t *testing.T, scriptPath, argsFile string) {
	t.Helper()
	script := "#!/bin/sh\nprintf 'CALL %s %s\\n' \"$1\" \"$2\" >> " + argsFile + "\n"
	writeTestFile(t, scriptPath, script)
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("writeFakeBrScript: chmod: %v", err)
	}
}

func writeFakeBrArgScript(t *testing.T, scriptPath, argsFile string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + argsFile + "\n"
	writeTestFile(t, scriptPath, script)
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("writeFakeBrArgScript: chmod: %v", err)
	}
}

func stagedBeadFixtureDeps(t *testing.T, projectDir, brPath string) (testRuntime, eagerRefillPort) {
	t.Helper()
	deps := em063FixtureDeps(t, nil)
	deps.env.ProjectDir = projectDir
	deps.env.BrPath = brPath
	return deps, eagerRefillPort{
		followUpLedger:   make(map[string]struct{}),
		followUpLedgerMu: new(sync.Mutex),
	}
}

func stagedBeadGeneratorEvalForTest(ctx context.Context, runtime testRuntime, eager eagerRefillPort, beadID core.BeadID, labels []string) {
	stagedBeadGeneratorEval(ctx, runtime.env.BrPath, runtime.reap(eager), beadID, labels)
}

// TestStagedBeadGenerator_NoopWhenBrPathEmpty verifies guardrail: empty brPath
// makes stagedBeadGeneratorEval a no-op (generator disabled).
func TestStagedBeadGenerator_NoopWhenBrPathEmpty(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy")

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, "")
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-abc", []string{"deploy"})
}

// TestStagedBeadGenerator_NoopWhenNoPhase2Classes verifies guardrail 1:
// if sentinel has no Phase-2 classes, nothing is created.
func TestStagedBeadGenerator_NoopWhenNoPhase2Classes(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writeTestFile(t, filepath.Join(projectDir, ".harmonik", "config.yaml"),
		"sentinel:\n  done_definition:\n    myclass: merged\n")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-abc", []string{"myclass"})

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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-abc", []string{"bugfix", "chore"})

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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-xyz", []string{"deploy", "other"})

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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-gltest", []string{"deploy"})

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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-xyz", []string{"deploy"})
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-xyz", []string{"deploy"})

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("br was not called on first invocation: %v", err)
	}
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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	eagerRefill.followUpLedger["hk-xyz:deploy"] = struct{}{}

	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-xyz", []string{"deploy"})

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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	ledgerPath := filepath.Join(tmp, followUpLedgerFileName)
	eagerRefill.followUpLedgerPath = ledgerPath

	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-persist", []string{"deploy"})

	if _, statErr := os.Stat(argsFile); statErr != nil {
		t.Fatalf("br was not called: %v", statErr)
	}

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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	deps.capacity.maxConcurrent = 1

	deps.runRegistry.Register(core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000099")), &runregistry.RunHandle{
		BeadID:    core.BeadID("hk-other"),
		StartedAt: time.Now(),
	})

	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-xyz", []string{"deploy"})

	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("br was called at WIP==max_concurrent; expected no-op (guardrail 3)")
	}
}

func stagedBeadGitFixture(t *testing.T, dir, refBeadID string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
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
// commit, the generator is a no-op even if the run terminal was a success.
func TestStagedBeadGenerator_NoopWhenProvenanceAbsent(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "make deploy")
	stagedBeadGitFixture(t, projectDir, "") // no Refs: hk-xyz on origin/main

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	deps.env.TargetBranch = "main"
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-xyz", []string{"deploy"})

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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	deps.env.TargetBranch = "main"
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-xyz", []string{"deploy"})

	if _, statErr := os.Stat(argsFile); statErr != nil {
		t.Errorf("br was not called despite Refs: hk-xyz present on origin/main: %v", statErr)
	}
}

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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	deps.capacity.maxConcurrent = 3

	deps.runRegistry.Register(core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000001")), &runregistry.RunHandle{
		BeadID:    core.BeadID("hk-inflight-1"),
		StartedAt: time.Now(),
	})
	deps.runRegistry.Register(core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000002")), &runregistry.RunHandle{
		BeadID:    core.BeadID("hk-inflight-2"),
		StartedAt: time.Now(),
	})

	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-xyz", []string{"deploy"})

	if _, statErr := os.Stat(argsFile); statErr != nil {
		t.Errorf("br was NOT called at WIP==maxConcurrent-1; expected creation (one slot free): %v", statErr)
	}
}

// TestStagedBeadGenerator_MultiClassSameBead_DifferentLedgerKeys verifies that
// two calls with the same completed bead but different Phase-2 class labels
// produce two separate ledger keys (beadID:classA and beadID:classB) and
// trigger two br create calls.
func TestStagedBeadGenerator_MultiClassSameBead_DifferentLedgerKeys(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	writeTestFile(t, filepath.Join(projectDir, ".harmonik", "config.yaml"),
		"sentinel:\n  done_definition:\n    deploy: make deploy\n    smoke: make smoke\n")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrScript(t, scriptPath, argsFile)

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-abc", []string{"deploy"})
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-abc", []string{"smoke"})

	eagerRefill.followUpLedgerMu.Lock()
	_, hasDeployKey := eagerRefill.followUpLedger["hk-abc:deploy"]
	_, hasSmokeKey := eagerRefill.followUpLedger["hk-abc:smoke"]
	eagerRefill.followUpLedgerMu.Unlock()

	if !hasDeployKey {
		t.Error("ledger missing key 'hk-abc:deploy'")
	}
	if !hasSmokeKey {
		t.Error("ledger missing key 'hk-abc:smoke'")
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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-alpha", []string{"deploy"})
	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-beta", []string{"deploy"})

	eagerRefill.followUpLedgerMu.Lock()
	_, hasAlpha := eagerRefill.followUpLedger["hk-alpha:deploy"]
	_, hasBeta := eagerRefill.followUpLedger["hk-beta:deploy"]
	eagerRefill.followUpLedgerMu.Unlock()

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
