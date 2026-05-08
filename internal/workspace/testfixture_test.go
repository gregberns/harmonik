package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// runIDRegex is the canonical filesystem-safety regex for run_id values per
// workspace-model.md §4.2 WM-002: "run_id MUST match the filesystem-safe regex
// [A-Za-z0-9-]+ (UUIDv7 satisfies this by construction)".
var runIDRegex = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// runIDValid reports whether s matches the canonical filesystem-safety regex for
// run_id values per workspace-model.md §4.2 WM-002.
//
// The regex [A-Za-z0-9-]+ is the normative constraint; UUIDv7 satisfies it by
// construction. Post-MVH ID-scheme extensions must preserve this invariant or
// declare an escape rule before adoption (WM-002).
func runIDValid(s string) bool {
	if len(s) == 0 {
		return false
	}
	return runIDRegex.MatchString(s)
}

// canonicalWorktreePath returns the canonical worktree path for a given repo root
// and run_id per workspace-model.md §4.2 WM-002:
//
//	<repo>/.harmonik/worktrees/<run_id>/
func canonicalWorktreePath(repo, runID string) string {
	return filepath.Join(repo, ".harmonik", "worktrees", runID) + string(filepath.Separator)
}

// tempRepo initialises a git repository in t.TempDir() with a single initial commit
// and returns the repo path and the initial commit SHA.
//
// The repository is a plain working-tree clone (not bare) as required by WM-002:
// "The daemon operates on a local clone only; workspaces MUST NOT be materialized
// against a bare remote URL."
func tempRepo(t *testing.T) (repoPath, initialSHA string) {
	t.Helper()

	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")

	// Create an initial commit so that HEAD is resolvable and worktree add can
	// use it as a <parent_commit> start-point.
	initFile := filepath.Join(dir, "README")
	if err := os.WriteFile(initFile, []byte("harmonik test repo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")

	// Capture the initial commit SHA for use as a deterministic parent_commit.
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	sha := string(out)
	if len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}

	return dir, sha
}

// classifyCrashEvidence classifies a worktree directory under <repo>/.harmonik/worktrees/<runID>/
// into one of the orphan evidence types defined by workspace-model.md §4.1 WM-003a:
//
//   - "bare-worktree-no-lease" — registered worktree, no lease-lock file, no sessions dir
//   - "sidecar-without-lease" — registered worktree, sidecar present, no lease-lock
//
// Both evidence types arise from a SIGKILL / power loss between `git worktree add`
// and the lease-lock fsync gate of WM-016 (workspace_leased emission). Neither the
// `leased` nor any post-`ready` event has been durably emitted.
//
// THIS IS A PLACEHOLDER. It implements the classification logic via direct filesystem
// inspection and is intended to capture the shape of the evidence classifier that the
// full reconciliation Cat 3 detector will replace.
//
// TODO: replace placeholder classifier when reconciliation Cat 3 detector lands
// (hk-8mwo.67 owns the lease-lock format; the Cat 3 detector will own the reconciliation
// routing logic).
func classifyCrashEvidence(repo, runID string) (string, error) {
	// Strip any trailing separator that canonicalWorktreePath appends.
	workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	// Confirm the worktree directory exists on disk.
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		return "", fmt.Errorf("classifyCrashEvidence: worktree path %q does not exist", workspacePath)
	}

	// Check for the lease-lock file per WM-013a canonical path:
	// ${workspace_path}/.harmonik/lease.lock
	leaseLock := filepath.Join(workspacePath, ".harmonik", "lease.lock")
	_, leaseLockErr := os.Stat(leaseLock)
	hasLeaseLock := (leaseLockErr == nil)

	if hasLeaseLock {
		// Not a crash evidence state — the lease lock is present.
		return "", fmt.Errorf("classifyCrashEvidence: lease-lock present at %q; not an orphan evidence state", leaseLock)
	}

	// No lease-lock. Now check for session sidecars:
	// ${workspace_path}/.harmonik/sessions/<session_id>/harmonik.meta.json
	// We look for any harmonik.meta.json under sessions/.
	sessionsDir := filepath.Join(workspacePath, ".harmonik", "sessions")
	sidecarFound := false

	entries, err := os.ReadDir(sessionsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				sidecar := filepath.Join(sessionsDir, entry.Name(), "harmonik.meta.json")
				if _, err := os.Stat(sidecar); err == nil {
					sidecarFound = true
					break
				}
			}
		}
	}

	// WM-003a classification rules:
	// - sidecar present, no lease-lock → "sidecar-without-lease"
	// - no lease-lock, no sessions dir → "bare-worktree-no-lease"
	if sidecarFound {
		return "sidecar-without-lease", nil
	}
	return "bare-worktree-no-lease", nil
}
