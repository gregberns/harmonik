//go:build scenario

package runloop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
