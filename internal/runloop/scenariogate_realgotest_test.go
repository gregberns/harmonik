//go:build scenario

package runloop

// scenariogate_realgotest_test.go — the scenario gate against a REAL
// `go test` run (hk-v5dyg, hk-7btdk).
//
// classifyScenarioGateError is thoroughly unit-tested in scenariogate_test.go,
// but every one of those cases hands it a HAND-BUILT error. Nothing checked that
// a real `go test` produces the exit codes and the output markers the classifier
// keys on. That is the gap these tests close: each one commits a real
// scenario-tagged probe into a real git worktree and runs the real gate over it.
//
// FAIL-OPEN is the contract (hk-ur428). A compile failure is gate-infrastructure
// noise, not a verdict on the work, so it ALLOWS. Only a test that RAN and
// FAILED blocks.
//
// WHY THIS LIVES HERE AND NOT IN internal/daemon. It used to be an end-to-end
// test that drove the whole work loop and asserted on the bead transition and on
// whether main advanced. That test could not have measured what it claimed:
// there is exactly one WireSpine call site in the daemon (workloop.go) and it
// sets SpineArgs.SkipGate unconditionally, because a DOT run gates inside the
// graph at its commit_gate node. Every run is a DOT run — dot is both the daemon
// default and the tier-4 fallback (moderesolve.go), and workflow:single selects
// a different GRAPH rather than a different code path. So runScenarioGateIfNeededVia
// never ran, in any mode, and both sub-tests were failing on an unrelated
// upstream error that read as a gate defect.
//
// The gate cannot be reached from internal/daemon, and it must not be: the
// runloop freeze gate (scripts/runloop-freeze-gate.sh) names both
// runScenarioGateIfNeededVia and RunScenarioGateIfNeededVia as forbidden
// declarations there. In-package here is the only honest home.
//
// Refs: hk-v5dyg, hk-7btdk (Refs hk-n7fw3, hk-i2ie5, hk-ur428).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gateEfficacyWorktree builds a git repo holding a minimal self-contained Go
// module whose //go:build scenario probe carries the supplied test body, and
// returns the worktree path plus the SHA the gate diffs against.
//
// The committed module makes the package buildable, so the gate's
// `go test -tags=scenario ./scenariopkg/...` produces a REAL verdict — a genuine
// FAIL on a t.Fatal body, or a real compile error on a malformed one — and not a
// no-Go-files build-constraint skip.
//
// The trivial non-test scenariopkg/doc.go gives the module a package that exists
// without the scenario tag. Without it `go test` reports "build constraints
// exclude all Go files", which the classifier reads as a compile failure and
// which would make the genuine-RED case pass for the wrong reason.
func gateEfficacyWorktree(t *testing.T, scenarioTestBody string) (wtPath, headSHA string) {
	t.Helper()

	wtPath = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals.
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = wtPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gateEfficacyWorktree: git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	git("init", "--initial-branch=main")
	git("config", "user.email", "test@harmonik.local")
	git("config", "user.name", "Harmonik Test")

	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(wtPath, rel)
		//nolint:gosec // G301: test-only worktree dir.
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("gateEfficacyWorktree: mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("gateEfficacyWorktree: write %s: %v", rel, err)
		}
	}

	write("README", "gate efficacy fixture\n")
	git("add", "README")
	git("commit", "-m", "Initial commit")
	headSHA = git("rev-parse", "HEAD")

	write("go.mod", "module scenariopkg.test\n\ngo 1.21\n")
	write("scenariopkg/doc.go", "package scenariopkg\n")
	write("scenariopkg/probe_test.go",
		"//go:build scenario\n\npackage scenariopkg\n\nimport \"testing\"\n\n"+
			"func TestScenarioGateProbe(t *testing.T) {\n"+scenarioTestBody+"\n}\n")
	git("add", "go.mod", "scenariopkg/doc.go", "scenariopkg/probe_test.go")
	git("commit", "-m", "test(scenario): gate efficacy probe")

	return wtPath, headSHA
}

// TestScenarioGateEfficacy_CompileFailDoesNotBlock pins the fail-open half of the
// contract (hk-ur428): a scenario commit that fails to COMPILE is gate
// infrastructure noise, not a test verdict, and must ALLOW. If a future change
// made the gate fail-CLOSED on build errors, this flips RED.
func TestScenarioGateEfficacy_CompileFailDoesNotBlock(t *testing.T) {
	t.Parallel()

	// References an undefined identifier: `go test` returns exit code 2 with a
	// `[build failed]` marker → isCompileFailure → fail-open ALLOW.
	wtPath, headSHA := gateEfficacyWorktree(t, "\tthisIdentifierIsUndefined()")

	res := runScenarioGateIfNeededVia(t.Context(), nil, wtPath, headSHA)
	if res.blocked {
		t.Errorf("the gate BLOCKED on a compile failure; it must fail-OPEN on build noise (hk-ur428).\nreason: %s", res.reason)
	}
}

// TestScenarioGateEfficacy_GenuineRedBlocksMerge is the other half: a
// deterministically-RED scenario test must BLOCK, and the reason it blocks with
// must name the gate.
//
// The reason string is not decoration. It is what runbridge.go's gateHook puts
// on the EvGateFailed event and what the run's failure summary carries, so an
// operator reading a blocked run learns WHY. A gate that blocks with an
// unattributable reason is a gate nobody can act on.
func TestScenarioGateEfficacy_GenuineRedBlocksMerge(t *testing.T) {
	t.Parallel()

	// A genuine, deterministic FAIL: exit 1 with a `--- FAIL` verdict →
	// isGenuineTestFailure → BLOCK. The gate re-runs a genuine RED once
	// (scenarioGateWithRetry); a t.Fatal fails identically both times.
	wtPath, headSHA := gateEfficacyWorktree(t,
		"\tt.Fatal(\"deterministic RED for gate-efficacy probe (hk-v5dyg)\")")

	res := runScenarioGateIfNeededVia(t.Context(), nil, wtPath, headSHA)
	if !res.blocked {
		t.Fatal("the gate ALLOWED a deterministically-RED scenario test — silent gate erosion")
	}
	if !strings.Contains(res.reason, "scenario_gate_failed") {
		t.Errorf("the block reason does not name the gate; an operator cannot attribute the failure.\nreason: %s", res.reason)
	}
}

// TestScenarioGateEfficacy_GreenPasses is the control. Same fixture, same real
// `go test`, one difference: the probe passes. Without it both tests above are
// satisfied by a gate that can only ever reach one answer.
func TestScenarioGateEfficacy_GreenPasses(t *testing.T) {
	t.Parallel()

	wtPath, headSHA := gateEfficacyWorktree(t, "\tt.Log(\"green\")")

	res := runScenarioGateIfNeededVia(t.Context(), nil, wtPath, headSHA)
	if res.blocked {
		t.Errorf("the gate BLOCKED a PASSING scenario test; the gate can only ever block.\nreason: %s", res.reason)
	}
}

// TestScenarioGateEfficacy_NoScenarioFilesIsANoOp pins the entry condition: when
// the commit touches nothing scenario-tagged the gate does not run `go test` at
// all. It is what keeps the gate off the critical path of the ordinary commit.
func TestScenarioGateEfficacy_NoScenarioFilesIsANoOp(t *testing.T) {
	t.Parallel()

	wtPath := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals.
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = wtPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--initial-branch=main")
	git("config", "user.email", "test@harmonik.local")
	git("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(wtPath, "README"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	git("add", "README")
	git("commit", "-m", "Initial commit")
	headSHA := git("rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(wtPath, "README"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatalf("rewrite README: %v", err)
	}
	git("add", "README")
	git("commit", "-m", "docs: touch nothing scenario-tagged")

	res := runScenarioGateIfNeededVia(t.Context(), nil, wtPath, headSHA)
	if res.blocked {
		t.Errorf("the gate blocked a commit that touches no scenario-tagged file.\nreason: %s", res.reason)
	}
}
