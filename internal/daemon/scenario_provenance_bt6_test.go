//go:build scenario

package daemon

// scenario_provenance_bt6_test.go — BT6 scenario test for the own-merged
// provenance gate of the work-generates-work positive loop (flywheel-motion.md
// §6.2, AC3 / bead hk-zlwq).
//
// # What is tested (BT6 — own-merged provenance)
//
// The staged-bead generator (§5.4 B) must only enqueue a deploy+verify follow-up
// for ONE of its OWN merged commits — i.e. when the completed bead's "Refs: <id>"
// trailer is verifiably present on origin/<targetBranch>. A run that "succeeded
// but is NOT on origin/main" (the daemon thinks the run is done, but the commit
// never landed on the remote) MUST spawn NO follow-up work.
//
// This is the negative assertion the bead asks for: not-on-origin/main → no
// follow-up. A positive control (Refs: present on origin/main → exactly one
// follow-up) is included so the negative is not vacuously passing because the
// generator is wedged.
//
// # Why this is the REAL provenance path (no stubs)
//
//   - stagedBeadGeneratorEval is the production §5.4 B generator (eagerfill_em063.go).
//   - beadOnOriginMain is the production §6.2 provenance guard it calls (hk-zlwq):
//     it runs `git log origin/<targetBranch> --grep "Refs: <id>"` against a real
//     git repo, fail-closed.
//   - The scenario drives a REAL git repo (init + bare origin + push) so the
//     provenance check executes a real `git log` against a real remote tracking
//     branch — not a stub.
//   - `br create` is a fake executable script whose INVOCATION (or non-invocation)
//     is the observable: a created follow-up means br was called; a no-op means it
//     was not. This matches the BT4 / EM-063 unit-test idiom already on main.
//
// # Why //go:build scenario
//
// The test shells out to a real `git` binary and performs real filesystem I/O
// (init/commit/push to a bare origin). It is tagged scenario so the daemon's
// 30-min commit-gate skips it; only the explicit scenario run covers it.
//
// Run independently:
//
//	go test -tags=scenario -run BT6 ./internal/daemon/...
//
// Spec ref: flywheel-motion.md §6.2 (provenance gate), §5.4 B (staged-bead
// generator). Bead: hk-rsje (flywheel-BT6). AC3 bead: hk-zlwq. Epic: hk-0oca
// (codename:flywheel).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ---------------------------------------------------------------------------
// helpers (prefix "bt6" per the helper-prefix discipline)
// ---------------------------------------------------------------------------

// bt6Phase2Project lays out a project dir with a Phase-2 sentinel class so the
// staged-bead generator is rule-eligible (guardrail 1), and returns the dir.
//
//	<dir>/.harmonik/config.yaml  (sentinel.done_definition.<class>: <verifyCmd>)
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

// bt6GitWithOrigin initialises a real git repo in dir with a bare origin remote.
// When refBeadID is non-empty, a commit carrying a "Refs: <refBeadID>" trailer is
// added to main and PUSHED to origin (so beadOnOriginMain finds it on
// origin/main). When refBeadID is empty, origin/main exists but carries NO such
// trailer — modelling "the run succeeded locally but its commit is NOT on
// origin/main".
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

// bt6FakeBr writes an executable shell script at scriptPath that records every
// invocation (one "CALL" line per call) into argsFile and exits 0. The presence
// or absence of argsFile is the observable: a follow-up bead created ⇔ br called.
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

// bt6Deps builds a workLoopDeps wired for stagedBeadGeneratorEval against a real
// git project and a fake br, with targetBranch="main" so the §6.2 provenance
// guard is ACTIVE (the guard is skipped only when targetBranch is empty).
func bt6Deps(t *testing.T, projectDir, brPath string) workLoopDeps {
	t.Helper()
	return workLoopDeps{
		queueStore:       nil,
		kerfPath:         "",
		projectDir:       projectDir,
		brPath:           brPath,
		maxConcurrent:    4,
		runRegistry:      newLocalRunRegistry(),
		bus:              &noopEmitter{},
		targetBranch:     "main",
		followUpLedger:   make(map[string]struct{}),
		followUpLedgerMu: new(sync.Mutex),
	}
}

// bt6BrCallCount counts CALL lines in argsFile; returns 0 when the file is absent
// (br never invoked).
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

// ---------------------------------------------------------------------------
// BT6 — own-merged provenance
// ---------------------------------------------------------------------------

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
	// origin/main exists, but carries NO "Refs: hk-bt6-merged" trailer:
	// the run "succeeded" locally but its commit never landed on origin/main.
	bt6GitWithOrigin(t, projectDir, "")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	brPath := filepath.Join(tmp, "br")
	bt6FakeBr(t, brPath, argsFile)

	deps := bt6Deps(t, projectDir, brPath)

	// Sanity: the provenance gate itself reports "not landed" for this bead, so
	// we know the no-op is the gate firing (not some unrelated guardrail).
	if beadOnOriginMain(context.Background(), projectDir, core.BeadID("hk-bt6-merged"), "main") {
		t.Fatal("precondition failed: beadOnOriginMain reported the bead landed, " +
			"but no Refs: trailer was pushed to origin/main")
	}

	// Drive the REAL §5.4 B generator with a rule-eligible class label.
	stagedBeadGeneratorEval(context.Background(), deps,
		core.BeadID("hk-bt6-merged"), []string{class})

	// NEGATIVE assertion: no follow-up bead created.
	if n := bt6BrCallCount(t, argsFile); n != 0 {
		t.Errorf("provenance gate breached: br create called %d time(s) for a bead "+
			"absent from origin/main; want 0 (§6.2 own-merged provenance)", n)
	}

	// And the at-most-once ledger must NOT have recorded the follow-up — the gate
	// returns before the ledger write, so no key should be present.
	deps.followUpLedgerMu.Lock()
	_, recorded := deps.followUpLedger["hk-bt6-merged:"+class]
	deps.followUpLedgerMu.Unlock()
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
	// origin/main DOES carry "Refs: hk-bt6-landed".
	bt6GitWithOrigin(t, projectDir, beadID)

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	brPath := filepath.Join(tmp, "br")
	bt6FakeBr(t, brPath, argsFile)

	deps := bt6Deps(t, projectDir, brPath)

	// Sanity: provenance gate confirms the landing before we drive the generator.
	if !beadOnOriginMain(context.Background(), projectDir, core.BeadID(beadID), "main") {
		t.Fatal("precondition failed: beadOnOriginMain did not find the Refs: trailer " +
			"that was pushed to origin/main")
	}

	stagedBeadGeneratorEval(context.Background(), deps,
		core.BeadID(beadID), []string{class})

	// POSITIVE assertion: exactly one follow-up bead created.
	if n := bt6BrCallCount(t, argsFile); n != 1 {
		t.Errorf("provenance-present path: br create called %d time(s); want exactly 1 "+
			"(§5.4 B staged-bead generator)", n)
	}
}
