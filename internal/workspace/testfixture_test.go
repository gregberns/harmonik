package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

var runIDRegex = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func runIDValid(s string) bool {
	if s == "" {
		return false
	}
	return runIDRegex.MatchString(s)
}

func tempRepo(t *testing.T) (repoPath, initialSHA string) {
	t.Helper()

	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")

	initFile := filepath.Join(dir, "README")
	if err := os.WriteFile(initFile, []byte("harmonik test repo\n"), 0o600); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")

	// Capture the initial commit SHA for use as a deterministic parent_commit.
	//nolint:gosec // G204: test invokes git with arguments derived from its temporary repository fixture
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	sha := string(out)
	if sha != "" && sha[len(sha)-1] == '\n' {
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
	workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		return "", fmt.Errorf("classifyCrashEvidence: worktree path %q does not exist", workspacePath)
	}

	leaseLock := filepath.Join(workspacePath, ".harmonik", "lease.lock")
	_, leaseLockErr := os.Stat(leaseLock)
	hasLeaseLock := (leaseLockErr == nil)

	if hasLeaseLock {
		return "", fmt.Errorf("classifyCrashEvidence: lease-lock present at %q; not an orphan evidence state", leaseLock)
	}

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

	if sidecarFound {
		return "sidecar-without-lease", nil
	}
	return "bare-worktree-no-lease", nil
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	//nolint:gosec // G304: path is supplied by this package's temporary test fixtures
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("mustReadFile %q: %v", path, err)
	}
	return data
}

func isLowerHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

func jsonObject(parent map[string]any, key string) (map[string]any, bool) {
	obj, ok := parent[key].(map[string]any)
	return obj, ok
}

func mustJSONObject(t *testing.T, parent map[string]any, key, context string) map[string]any {
	t.Helper()

	obj, ok := jsonObject(parent, key)
	if !ok {
		t.Fatalf("%s: %q is absent or is not a JSON object; got %#v", context, key, parent[key])
	}
	return obj
}

func trustDialogAccepted(entry map[string]any) bool {
	accepted, ok := entry["hasTrustDialogAccepted"].(bool)
	return ok && accepted
}
