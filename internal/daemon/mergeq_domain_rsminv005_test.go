package daemon

// mergeq_domain_rsminv005_test.go — RSM-INV-005 / RSM-017 mechanical DoD for the
// merge exclusion domain, plus the RSM-021 shutdown-drain (bgCtx) proof.
//
// The merge split (RSM-012..016) runs the rebase, the go build/vet gate, and the
// gofumpt/gci auto-fix in a speculative prepare phase OUTSIDE the exclusion
// domain, and only the FF re-validation → update-ref → push → working-tree reset
// INSIDE it (via mergeq.Queue.Submit). RSM-017 / RSM-INV-005 require that no
// build-class command (go build/vet, gofumpt, gci, git rebase) runs inside the
// domain, and that the commit phase's command inventory is the enumerated
// allowlist. These tests prove both against the real daemon-side prepare/commit
// closures — the daemon layer of the harness scaffolding RT2 stood up in
// internal/mergeq (mergeq_test.go §RSM-INV-005).
//
// Mechanism: a PATH shim for `git` and `go` logs every invocation's subcommand
// to a file; the injected mergeSubmit brackets the critical section with
// ENTER/EXIT markers in the SAME file. Commands recorded between the markers are
// the inside-domain inventory.
//
// Design: 04-design/merge-queue-design.md §2, §4, §5.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/workspace"
)

// rsmInvBuildClass are the commands that MUST NOT run inside the exclusion
// domain (merge-queue-design §5) — the critical-section-creep change detector.
var rsmInvBuildClass = map[string]bool{
	"go build":   true,
	"go vet":     true,
	"gofumpt":    true,
	"gci":        true,
	"git rebase": true,
}

// rsmInvCommitAllowlist is the exact command inventory the commit phase may run
// (merge-queue-design §5).
var rsmInvCommitAllowlist = map[string]bool{
	"git rev-parse":  true,
	"git merge-base": true,
	"git update-ref": true,
	"git push":       true,
	"git fetch":      true,
	"git restore":    true,
	"git reset":      true,
	"git diff":       true,
	"br sync":        true,
}

// rsmInvGit runs a git command in dir, failing the test on error.
func rsmInvGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rsmInvGit: git %v: %v\n%s", args, err, out)
	}
}

// rsmInvWriteFile writes content to path, failing the test on error.
func rsmInvWriteFile(t *testing.T, path, content string) {
	t.Helper()
	//nolint:gosec // G306: test fixture file.
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("rsmInvWriteFile %s: %v", path, err)
	}
}

// rsmInvAppend appends line (plus a newline) to path, failing the test on error.
func rsmInvAppend(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // G304: test path.
	if err != nil {
		t.Fatalf("rsmInvAppend open %s: %v", path, err)
	}
	if _, werr := f.WriteString(line + "\n"); werr != nil {
		if cerr := f.Close(); cerr != nil {
			t.Logf("rsmInvAppend close-after-write-error %s: %v", path, cerr)
		}
		t.Fatalf("rsmInvAppend write %s: %v", path, werr)
	}
	if cerr := f.Close(); cerr != nil {
		t.Fatalf("rsmInvAppend close %s: %v", path, cerr)
	}
}

// rsmInvWriteShim writes an executable PATH shim at dir/name whose body is body.
func rsmInvWriteShim(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	//nolint:gosec // G306: 0755 — a test shim must be executable.
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("rsmInvWriteShim %s: %v", name, err)
	}
}

// rsmInvReadDomainInventory parses the shim log and returns the commands recorded
// while inside the ENTER/EXIT "commit-merge" markers (insideCmds) and those
// recorded outside it (outsideCmds).
func rsmInvReadDomainInventory(t *testing.T, logPath string) (insideCmds, outsideCmds []string) {
	t.Helper()
	data, err := os.ReadFile(logPath) //nolint:gosec // G304: test-controlled path.
	if err != nil {
		t.Fatalf("rsmInvReadDomainInventory read: %v", err)
	}
	inside := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "ENTER commit-merge":
			inside = true
		case line == "EXIT commit-merge":
			inside = false
		case strings.HasPrefix(line, "ENTER "), strings.HasPrefix(line, "EXIT "):
			// other domain members (escape-check, base-sync-create) — ignore.
		case line == "":
		default:
			if inside {
				insideCmds = append(insideCmds, line)
			} else {
				outsideCmds = append(outsideCmds, line)
			}
		}
	}
	return insideCmds, outsideCmds
}

// rsmInvSetupRepo builds a project git repo with a bare origin and a run-branch
// one commit ahead of main (fast-forwardable). It returns the projectDir and the
// RunID whose run-branch is ready to merge. A go.mod is present so the prepare
// build gate actually invokes the `go` shim (a positive control that build-class
// work runs — just OUTSIDE the domain).
func rsmInvSetupRepo(t *testing.T) (projectDir string, runID core.RunID) {
	t.Helper()
	projectDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "events"), 0o750); err != nil {
		t.Fatalf("rsmInvSetupRepo mkdir: %v", err)
	}
	rsmInvGit(t, projectDir, "init", "--initial-branch=main")
	rsmInvGit(t, projectDir, "config", "user.email", "daemon@harmonik.local")
	rsmInvGit(t, projectDir, "config", "user.name", "Harmonik Test")
	rsmInvWriteFile(t, filepath.Join(projectDir, "go.mod"), "module rsminv\n\ngo 1.22\n")
	rsmInvWriteFile(t, filepath.Join(projectDir, "README"), "init\n")
	rsmInvGit(t, projectDir, "add", ".")
	rsmInvGit(t, projectDir, "commit", "-m", "init")

	// Bare origin + initial push so the commit-phase `git push origin main` works.
	originDir := t.TempDir()
	rsmInvGit(t, originDir, "init", "--bare", "--initial-branch=main")
	rsmInvGit(t, projectDir, "remote", "add", "origin", originDir)
	rsmInvGit(t, projectDir, "push", "origin", "main")

	// A run-branch one commit ahead of main on a distinct file.
	runID = core.RunID(uuid.MustParse("0190a000-0000-7000-8000-0000000a0001"))
	runBranch := workspace.TaskBranchName(runID.String())
	rsmInvGit(t, projectDir, "branch", runBranch, "main")
	// Commit onto the run-branch via a temporary worktree so the project checkout
	// stays on main (mirrors production).
	tmpWt := filepath.Join(t.TempDir(), "runwt")
	rsmInvGit(t, projectDir, "worktree", "add", tmpWt, runBranch)
	rsmInvWriteFile(t, filepath.Join(tmpWt, "work.txt"), "agent work\n")
	rsmInvGit(t, tmpWt, "add", "work.txt")
	rsmInvGit(t, tmpWt, "commit", "-m", "agent commit")
	rsmInvGit(t, projectDir, "worktree", "remove", "--force", tmpWt)
	return projectDir, runID
}

// rsmInvInstallShims prepends a PATH shim dir with logging `git` and `go`
// wrappers and returns the log path. The git shim forwards to the real git so
// the merge actually runs; the go shim logs and exits 0 so the build gate passes
// without a real toolchain build.
func rsmInvInstallShims(t *testing.T) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	shimDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "cmds.log")
	rsmInvWriteShim(t, shimDir, "git",
		"#!/bin/sh\necho \"git $1\" >> \""+logPath+"\"\nexec "+realGit+" \"$@\"\n")
	rsmInvWriteShim(t, shimDir, "go",
		"#!/bin/sh\necho \"go $1\" >> \""+logPath+"\"\nexit 0\n")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// TestMergeQDomain_RSMInv005_NoBuildClassInsideDomain drives the real daemon-side
// prepare/commit split through a recording mergeSubmit and asserts (RSM-017 /
// RSM-INV-005): no build-class command runs inside the exclusion domain, the
// inside-domain inventory is a subset of the commit allowlist, and — as a
// positive control — build-class work (git rebase, go build/vet) DID run, just
// OUTSIDE the domain.
func TestMergeQDomain_RSMInv005_NoBuildClassInsideDomain(t *testing.T) {
	logPath := rsmInvInstallShims(t)
	projectDir, runID := rsmInvSetupRepo(t)

	// Recording submit: bracket the critical section with markers in the SAME log
	// the shims write to, then run the critical inline (single-owner semantics).
	recSubmit := func(ctx context.Context, label string, critical func(context.Context) error) error {
		rsmInvAppend(t, logPath, "ENTER "+label)
		cerr := critical(ctx)
		rsmInvAppend(t, logPath, "EXIT "+label)
		return cerr
	}

	out := mergeRunBranchToMain(t.Context(), recSubmit, projectDir, runID, &noopEmitter{},
		core.BeadID("hk-rsminv005"), "", "main", nil, "")
	if !out.success {
		t.Fatalf("merge did not succeed: reason=%q noChange=%v", out.reason, out.noChange)
	}

	inside, outside := rsmInvReadDomainInventory(t, logPath)
	if len(inside) == 0 {
		t.Fatal("no commands recorded inside the exclusion domain — harness did not observe the commit phase")
	}

	// (1) No build-class command inside the domain; inside ⊆ commit allowlist.
	for _, cmd := range inside {
		if rsmInvBuildClass[cmd] {
			t.Errorf("RSM-017 violation: build-class command %q ran INSIDE the exclusion domain", cmd)
		}
		if !rsmInvCommitAllowlist[cmd] {
			t.Errorf("RSM-INV-005 violation: command %q inside the domain is not in the commit allowlist", cmd)
		}
	}

	// (2) Positive control: build-class work actually ran — just outside.
	outsideSet := map[string]bool{}
	for _, c := range outside {
		outsideSet[c] = true
	}
	if !outsideSet["git rebase"] {
		t.Errorf("expected the prepare phase to run `git rebase` OUTSIDE the domain; outside=%v", outside)
	}
	if !outsideSet["go build"] && !outsideSet["go vet"] {
		t.Errorf("expected the prepare build gate to run `go build`/`go vet` OUTSIDE the domain; outside=%v", outside)
	}

	// (3) The commit phase actually pushed (the ref advanced) — inside the domain.
	pushedInside := false
	for _, c := range inside {
		if c == "git push" {
			pushedInside = true
			break
		}
	}
	if !pushedInside {
		t.Errorf("expected `git push` INSIDE the domain (commit phase); inside=%v", inside)
	}
}

// TestMergeQDomain_ShutdownDrain_BgCtxSubmission proves the RSM-021 shutdown-drain
// path: a merge submitted on a background context (as beadRunOne does when the
// per-run ctx is already cancelled during shutdown) still drains through a live
// queue owner. It drives the real split with a started mergeq.Queue and a
// context.Background() submission context and asserts the merge completes.
func TestMergeQDomain_ShutdownDrain_BgCtxSubmission(t *testing.T) {
	_ = rsmInvInstallShims(t)
	projectDir, runID := rsmInvSetupRepo(t)

	q := mergeq.New(nil)
	qctx, qcancel := context.WithCancel(context.Background())
	q.Start(qctx)
	t.Cleanup(qcancel)

	// bgCtx mirrors the shutdown-drain submission context (outlives the per-run
	// ctx). The queue owner is alive, so the critical section drains.
	bgCtx := context.Background()
	out := mergeRunBranchToMain(bgCtx, q.Submit, projectDir, runID, &noopEmitter{},
		core.BeadID("hk-drain"), "", "main", nil, "")
	if !out.success {
		t.Fatalf("shutdown-drain merge did not succeed: reason=%q noChange=%v", out.reason, out.noChange)
	}
}
