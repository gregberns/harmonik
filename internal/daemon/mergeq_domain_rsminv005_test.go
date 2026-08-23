package daemon

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
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/workspace"
)

var rsmInvBuildClass = map[string]bool{
	"go build":   true,
	"go vet":     true,
	"gofumpt":    true,
	"gci":        true,
	"git rebase": true,
}

var rsmInvCommitAllowlist = map[string]bool{
	"git rev-parse":  true,
	"git merge-base": true,
	"git update-ref": true,
	"git fetch":      true,
	"git restore":    true,
	"git reset":      true,
	"git diff":       true,
	"br sync":        true,
}

func rsmInvGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rsmInvGit: git %v: %v\n%s", args, err, out)
	}
}

func rsmInvWriteFile(t *testing.T, path, content string) {
	t.Helper()
	//nolint:gosec // G306: test fixture file.
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("rsmInvWriteFile %s: %v", path, err)
	}
}

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

func rsmInvWriteShim(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	//nolint:gosec // G306: 0755 — a test shim must be executable.
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("rsmInvWriteShim %s: %v", name, err)
	}
}

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

	originDir := t.TempDir()
	rsmInvGit(t, originDir, "init", "--bare", "--initial-branch=main")
	rsmInvGit(t, projectDir, "remote", "add", "origin", originDir)
	rsmInvGit(t, projectDir, "push", "origin", "main")

	runID = core.RunID(uuid.MustParse("0190a000-0000-7000-8000-0000000a0001"))
	runBranch := workspace.TaskBranchName(runID.String())
	rsmInvGit(t, projectDir, "branch", runBranch, "main")
	tmpWt := filepath.Join(t.TempDir(), "runwt")
	rsmInvGit(t, projectDir, "worktree", "add", tmpWt, runBranch)
	rsmInvWriteFile(t, filepath.Join(tmpWt, "work.txt"), "agent work\n")
	rsmInvGit(t, tmpWt, "add", "work.txt")
	rsmInvGit(t, tmpWt, "commit", "-m", "agent commit")
	rsmInvGit(t, projectDir, "worktree", "remove", "--force", tmpWt)
	return projectDir, runID
}

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

	recSubmit := func(ctx context.Context, label string, critical func(context.Context) error) error {
		rsmInvAppend(t, logPath, "ENTER "+label)
		cerr := critical(ctx)
		rsmInvAppend(t, logPath, "EXIT "+label)
		return cerr
	}

	out := runmerge.RunBranchToTarget(t.Context(), recSubmit, projectDir, runID, &noopEmitter{},
		core.BeadID("hk-rsminv005"), "", "main", nil, "")
	if !out.Success {
		t.Fatalf("merge did not succeed: reason=%q noChange=%v", out.Reason, out.NoChange)
	}

	inside, outside := rsmInvReadDomainInventory(t, logPath)
	if len(inside) == 0 {
		t.Fatal("no commands recorded inside the exclusion domain — harness did not observe the commit phase")
	}

	for _, cmd := range inside {
		if rsmInvBuildClass[cmd] {
			t.Errorf("RSM-017 violation: build-class command %q ran INSIDE the exclusion domain", cmd)
		}
		if cmd == "git push" {
			t.Errorf("RSM-019 (F4) violation: `git push` ran INSIDE the exclusion domain; the push must be relocated OUTSIDE (Phase B)")
		}
		if !rsmInvCommitAllowlist[cmd] {
			t.Errorf("RSM-INV-005 violation: command %q inside the domain is not in the commit allowlist", cmd)
		}
	}

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

	if !outsideSet["git push"] {
		t.Errorf("expected `git push` OUTSIDE the domain (Phase B); outside=%v", outside)
	}
	updateRefInside := false
	for _, c := range inside {
		if c == "git update-ref" {
			updateRefInside = true
			break
		}
	}
	if !updateRefInside {
		t.Errorf("expected `git update-ref` INSIDE the domain (Phase A ref-advance); inside=%v", inside)
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

	bgCtx := context.Background()
	out := runmerge.RunBranchToTarget(bgCtx, q.Submit, projectDir, runID, &noopEmitter{},
		core.BeadID("hk-drain"), "", "main", nil, "")
	if !out.Success {
		t.Fatalf("shutdown-drain merge did not succeed: reason=%q noChange=%v", out.Reason, out.NoChange)
	}
}
