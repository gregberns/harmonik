package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// diffhashFixtureRepo creates a git repository with an initial commit and
// returns the repo path and the initial commit SHA.
//
// Prefixed diffhashFixture per implementer-protocol helper-prefix discipline
// (bead hk-7om2q.17).
func diffhashFixtureRepo(t *testing.T) (repoPath, initialSHA string) {
	t.Helper()

	dir := t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("diffhashFixtureRepo: git %v: %v\n%s", args, err, out)
		}
		return strings.TrimRight(string(out), "\n")
	}

	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")

	initFile := filepath.Join(dir, "README")
	if err := os.WriteFile(initFile, []byte("harmonik test repo\n"), 0o644); err != nil {
		t.Fatalf("diffhashFixtureRepo: WriteFile README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")

	sha := run("rev-parse", "HEAD")
	return dir, sha
}

// diffhashFixtureCommit adds a file with the given name and content to the
// repo and creates a commit, returning the new commit SHA.
//
// Prefixed diffhashFixture per implementer-protocol helper-prefix discipline
// (bead hk-7om2q.17).
func diffhashFixtureCommit(t *testing.T, repoPath, filename, content string) string {
	t.Helper()

	filePath := filepath.Join(repoPath, filename)
	//nolint:gosec // G306: test file; content is test-controlled
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("diffhashFixtureCommit: WriteFile %q: %v", filePath, err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("diffhashFixtureCommit: git %v: %v\n%s", args, err, out)
		}
		return strings.TrimRight(string(out), "\n")
	}

	run("add", filename)
	run("commit", "-m", "add "+filename)
	return run("rev-parse", "HEAD")
}

// TestEM015e_ComputeDiffHash_EmptyDiffIsNonError verifies that ComputeDiffHash
// returns a non-error result when parentSHA == headSHA (empty diff, no changes).
//
// Spec ref: execution-model.md §4.3.EM-015e — empty diff must not be an error.
func TestEM015e_ComputeDiffHash_EmptyDiffIsNonError(t *testing.T) {
	t.Parallel()

	repoPath, initialSHA := diffhashFixtureRepo(t)

	hash, err := ComputeDiffHash(t.Context(), repoPath, initialSHA, initialSHA)
	if err != nil {
		t.Fatalf("ComputeDiffHash (empty diff): unexpected error: %v", err)
	}
	if hash == "" {
		t.Error("ComputeDiffHash (empty diff): returned empty hash; want non-empty SHA-256 hex string")
	}
	// SHA-256 hex is always 64 characters.
	if len(hash) != 64 {
		t.Errorf("ComputeDiffHash (empty diff): hash length = %d; want 64", len(hash))
	}
}

// TestEM015e_ComputeDiffHash_IdenticalDiffProducesIdenticalHash verifies that
// two calls with the same parentSHA..headSHA produce the same hash.
//
// This is the core invariant of the no-progress detector: same diff → same
// hash → no progress made between iterations.
//
// Spec ref: execution-model.md §4.3.EM-015e — "If the computed hash equals the
// prior iteration's Run.context.last_diff_hash value … the daemon MUST emit
// no_progress_detected".
func TestEM015e_ComputeDiffHash_IdenticalDiffProducesIdenticalHash(t *testing.T) {
	t.Parallel()

	repoPath, parentSHA := diffhashFixtureRepo(t)
	headSHA := diffhashFixtureCommit(t, repoPath, "feature.txt", "hello world\n")

	hash1, err := ComputeDiffHash(t.Context(), repoPath, parentSHA, headSHA)
	if err != nil {
		t.Fatalf("ComputeDiffHash (first call): unexpected error: %v", err)
	}

	hash2, err := ComputeDiffHash(t.Context(), repoPath, parentSHA, headSHA)
	if err != nil {
		t.Fatalf("ComputeDiffHash (second call): unexpected error: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("ComputeDiffHash: identical diff produced different hashes: %q vs %q", hash1, hash2)
	}
}

// TestEM015e_ComputeDiffHash_DifferentDiffProducesDifferentHash verifies that
// a one-line change produces a different hash than an empty diff (or a prior
// diff of different content).
//
// Spec ref: execution-model.md §4.3.EM-015e — acceptance criterion "one-line
// diff produces different hash".
func TestEM015e_ComputeDiffHash_DifferentDiffProducesDifferentHash(t *testing.T) {
	t.Parallel()

	repoPath, parentSHA := diffhashFixtureRepo(t)

	// First iteration: add one file.
	headSHA1 := diffhashFixtureCommit(t, repoPath, "feature.txt", "hello world\n")

	// Second iteration: add a different file (different diff vs parent).
	headSHA2 := diffhashFixtureCommit(t, repoPath, "extra.txt", "extra content\n")

	hashBase, err := ComputeDiffHash(t.Context(), repoPath, parentSHA, headSHA1)
	if err != nil {
		t.Fatalf("ComputeDiffHash (base diff): unexpected error: %v", err)
	}

	hashChanged, err := ComputeDiffHash(t.Context(), repoPath, parentSHA, headSHA2)
	if err != nil {
		t.Fatalf("ComputeDiffHash (changed diff): unexpected error: %v", err)
	}

	if hashBase == hashChanged {
		t.Errorf("ComputeDiffHash: different diffs produced identical hash %q; want distinct hashes", hashBase)
	}
}

// TestEM015e_ComputeDiffHash_HashFormat verifies that the returned hash is a
// lowercase hex-encoded SHA-256 (64 characters, hex alphabet only).
//
// Spec ref: execution-model.md §4.3.EM-015e — "SHA-256 hash of git diff output".
func TestEM015e_ComputeDiffHash_HashFormat(t *testing.T) {
	t.Parallel()

	repoPath, parentSHA := diffhashFixtureRepo(t)
	headSHA := diffhashFixtureCommit(t, repoPath, "thing.txt", "data\n")

	hash, err := ComputeDiffHash(t.Context(), repoPath, parentSHA, headSHA)
	if err != nil {
		t.Fatalf("ComputeDiffHash: unexpected error: %v", err)
	}

	if len(hash) != 64 {
		t.Errorf("ComputeDiffHash: hash length = %d; want 64 (SHA-256 hex)", len(hash))
	}

	for i, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("ComputeDiffHash: hash[%d] = %q; want lowercase hex character", i, c)
			break
		}
	}
}
