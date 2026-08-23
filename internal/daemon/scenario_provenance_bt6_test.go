//go:build scenario

package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runloop"
)

func bt6Phase2Project(t *testing.T, class, verifyCmd string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := "sentinel:\n  done_definition:\n    " + class + ": \"" + verifyCmd + "\"\n"
	cfgPath := filepath.Join(dir, ".harmonik", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("bt6Phase2Project: mkdir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("bt6Phase2Project: write config: %v", err)
	}
	return dir
}

func bt6GitWithOrigin(t *testing.T, dir, refBeadID string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals.
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bt6GitWithOrigin: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("bt6GitWithOrigin: WriteFile README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")

	if refBeadID != "" {
		if err := os.WriteFile(filepath.Join(dir, "marker"), []byte(refBeadID+"\n"), 0o644); err != nil {
			t.Fatalf("bt6GitWithOrigin: WriteFile marker: %v", err)
		}
		run("add", "marker")
		run("commit", "-m", "work\n\nRefs: "+refBeadID)
	}

	originDir := t.TempDir()
	//nolint:gosec // G204: git args are test-internal literals.
	bare := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := bare.CombinedOutput(); err != nil {
		t.Fatalf("bt6GitWithOrigin: git init --bare: %v\n%s", err, out)
	}
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")
}

func bt6FakeBr(t *testing.T, scriptPath, argsFile string) {
	t.Helper()
	script := "#!/bin/sh\nprintf 'CALL %s %s\\n' \"$1\" \"$2\" >> " + argsFile + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("bt6FakeBr: write: %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("bt6FakeBr: chmod: %v", err)
	}
}

func bt6Deps(t *testing.T, projectDir, brPath string) (testRuntime, eagerRefillPort) {
	t.Helper()
	return testRuntime{
			queueStore:  nil,
			env:         runloop.RunEnv{ProjectDir: projectDir, BrPath: brPath, TargetBranch: "main"},
			ports:       runloop.RunPorts{Emitter: &noopEmitter{}},
			capacity:    newCapacityPort(4, nil),
			runRegistry: newLocalRunRegistry(),
		}, eagerRefillPort{
			followUpLedger:   make(map[string]struct{}),
			followUpLedgerMu: new(sync.Mutex),
		}
}

func bt6BrCallCount(t *testing.T, argsFile string) int {
	t.Helper()
	data, err := os.ReadFile(argsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("bt6BrCallCount: read %s: %v", argsFile, err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CALL ") {
			n++
		}
	}
	return n
}

// TestScenario_BT6_OwnMergedProvenance_NotOnOriginMain_NoFollowUp is the core
// negative assertion (flywheel-motion.md §6.2, hk-zlwq): a run whose completed
// bead is a rule-eligible Phase-2 class, is below the WIP ceiling, and would
// otherwise generate a deploy+verify follow-up — but whose "Refs: <id>" trailer
// is NOT present on origin/main — generates NO follow-up work.
//
// The git repo HAS an origin/main (so this is genuinely "succeeded-but-not-landed",
// not merely "no remote"): the negative is caused by the provenance gate failing
// to find the run's commit on the remote, not by an absent remote.
func TestScenario_BT6_OwnMergedProvenance_NotOnOriginMain_NoFollowUp(t *testing.T) {
	t.Parallel()

	const class = "deploy"
	projectDir := bt6Phase2Project(t, class, "make deploy-verify")
	bt6GitWithOrigin(t, projectDir, "")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	brPath := filepath.Join(tmp, "br")
	bt6FakeBr(t, brPath, argsFile)

	deps, eagerRefill := bt6Deps(t, projectDir, brPath)

	if beadOnOriginMain(context.Background(), projectDir, core.BeadID("hk-bt6-merged"), "main") {
		t.Fatal("precondition failed: beadOnOriginMain reported the bead landed, " +
			"but no Refs: trailer was pushed to origin/main")
	}

	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill,
		core.BeadID("hk-bt6-merged"), []string{class})

	if n := bt6BrCallCount(t, argsFile); n != 0 {
		t.Errorf("provenance gate breached: br create called %d time(s) for a bead "+
			"absent from origin/main; want 0 (§6.2 own-merged provenance)", n)
	}

	eagerRefill.followUpLedgerMu.Lock()
	_, recorded := eagerRefill.followUpLedger["hk-bt6-merged:"+class]
	eagerRefill.followUpLedgerMu.Unlock()
	if recorded {
		t.Error("ledger recorded a follow-up for an un-merged bead; the provenance " +
			"gate must short-circuit before the at-most-once ledger write")
	}
}

// TestScenario_BT6_OwnMergedProvenance_OnOriginMain_FollowUpFires is the positive
// control: the SAME generator, SAME config, SAME WIP headroom — but now the run's
// "Refs: <id>" trailer IS on origin/main — DOES generate exactly one follow-up.
// Without this control, the negative test could pass for the wrong reason (e.g. a
// wedged generator that never fires).
func TestScenario_BT6_OwnMergedProvenance_OnOriginMain_FollowUpFires(t *testing.T) {
	t.Parallel()

	const class = "deploy"
	const beadID = "hk-bt6-landed"
	projectDir := bt6Phase2Project(t, class, "make deploy-verify")
	bt6GitWithOrigin(t, projectDir, beadID)

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	brPath := filepath.Join(tmp, "br")
	bt6FakeBr(t, brPath, argsFile)

	deps, eagerRefill := bt6Deps(t, projectDir, brPath)

	if !beadOnOriginMain(context.Background(), projectDir, core.BeadID(beadID), "main") {
		t.Fatal("precondition failed: beadOnOriginMain did not find the Refs: trailer " +
			"that was pushed to origin/main")
	}

	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill,
		core.BeadID(beadID), []string{class})

	if n := bt6BrCallCount(t, argsFile); n != 1 {
		t.Errorf("provenance-present path: br create called %d time(s); want exactly 1 "+
			"(§5.4 B staged-bead generator)", n)
	}
}
