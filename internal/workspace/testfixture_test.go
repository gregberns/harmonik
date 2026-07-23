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
	if s == "" {
		return false
	}
	return runIDRegex.MatchString(s)
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

	// Create an initial commit so that HEAD is resolvable and worktree add can
	// use it as a <parent_commit> start-point.
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
	// The canonical worktree path per WM-002.
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

// mustReadFile reads path or fails the test.
//
// It replaces the `data, _ := os.ReadFile(path)` idiom that used to appear
// throughout this package's tests. Discarding the error there meant an
// unreadable or missing file surfaced as a confusing downstream assertion
// failure ("missing section X") instead of naming the real problem, and it
// hid the case where the production code under test never created the file
// at all — the assertion then ran against an empty string.
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	//nolint:gosec // G304: path is supplied by this package's temporary test fixtures
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("mustReadFile %q: %v", path, err)
	}
	return data
}

// isLowerHexDigit reports whether r is one of 0-9 or a-f.
//
// Shared by the git-SHA and diff-hash assertions, which both check that a hash
// string is lowercase hex. Naming the predicate keeps the intent readable at
// the call site; the inline form it replaces read as a negated disjunction that
// staticcheck kept asking to invert into something harder to follow.
func isLowerHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

// jsonObject returns parent[key] as a nested JSON object.
//
// The tests that walk ~/.claude.json used to spell this `x, _ := parent[key].(T)`
// and then nil-check x, which discarded the type-assertion result and made a
// wrong-typed value indistinguishable from an absent one.
func jsonObject(parent map[string]any, key string) (map[string]any, bool) {
	obj, ok := parent[key].(map[string]any)
	return obj, ok
}

// mustJSONObject is jsonObject, failing the test when key is absent or is not
// a JSON object. context names what was being looked up, for the failure message.
func mustJSONObject(t *testing.T, parent map[string]any, key, context string) map[string]any {
	t.Helper()

	obj, ok := jsonObject(parent, key)
	if !ok {
		t.Fatalf("%s: %q is absent or is not a JSON object; got %#v", context, key, parent[key])
	}
	return obj
}

// trustDialogAccepted reports whether a ~/.claude.json project entry carries
// hasTrustDialogAccepted: true.
//
// An absent key, and a key holding any non-bool, both read as false — the same
// semantics as the blank-discard type assertions this replaces, and the same
// reading Claude Code itself applies (anything but an explicit true re-prompts).
func trustDialogAccepted(entry map[string]any) bool {
	accepted, ok := entry["hasTrustDialogAccepted"].(bool)
	return ok && accepted
}
