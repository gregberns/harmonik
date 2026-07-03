package workspace

// reviewverdict_via_hkf3u6o_test.go — tests for ReadReviewVerdictVia, the
// remote-aware verdict reader (hk-f3u6o).
//
// Scenario: on a REMOTE run the reviewer writes review.json on the WORKER, so a
// box-A os.ReadFile (ReadReviewVerdict) never finds it → the run false-failed as
// "verdict absent". ReadReviewVerdictVia routes the read through the run's
// CommandRunner (cat over the transport) so the worker-side file is read and
// validated identically.
//
// Mirrors the 0f1c98a8 / ComputeDiffHashVia idiom: nil runner → byte-identical
// local read; a non-local runner → routed read.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// catFromFileRunner is a non-local CommandRunner stub: every Command() invocation
// is rewritten to `cat <srcPath>`, so .Output()/.CombinedOutput() yields the
// bytes of srcPath regardless of the requested argv. It simulates a remote worker
// whose review.json lives at srcPath. Being a distinct (non-LocalRunner) type, it
// is classified as non-local by runnerIsLocalFS, so ReadReviewVerdictVia routes
// through it rather than falling back to os.ReadFile.
type catFromFileRunner struct {
	srcPath string // file whose bytes Command() returns; "" → a path that does not exist (cat fails)
}

func (r catFromFileRunner) Command(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	src := r.srcPath
	if src == "" {
		src = filepath.Join(os.TempDir(), "hkf3u6o-nonexistent-verdict-file")
	}
	//nolint:gosec // G204: src is a test-controlled temp path, not user input
	return exec.CommandContext(ctx, "cat", src)
}

// TestReadReviewVerdictVia_NilRunner_ReadsLocally verifies the nil-runner path is
// byte-identical to ReadReviewVerdict (NFR7): it reads from box-A local disk.
func TestReadReviewVerdictVia_NilRunner_ReadsLocally(t *testing.T) {
	t.Parallel()

	data := reviewVerdictFixtureValidJSON(t)
	workspacePath := reviewVerdictFixtureWrite(t, data)

	v, err := ReadReviewVerdictVia(context.Background(), nil, workspacePath)
	if err != nil {
		t.Fatalf("ReadReviewVerdictVia(nil): %v", err)
	}
	if v == nil {
		t.Fatal("ReadReviewVerdictVia(nil) returned nil; want the locally-written verdict")
	}
	if v.Verdict != ReviewVerdictApprove {
		t.Errorf("Verdict = %q; want %q", v.Verdict, ReviewVerdictApprove)
	}
}

// TestReadReviewVerdictVia_LocalRunner_ReadsLocally verifies that a tmux.LocalRunner
// (local-FS) also takes the local read path, not the cat-routed path.
func TestReadReviewVerdictVia_LocalRunner_ReadsLocally(t *testing.T) {
	t.Parallel()

	data := reviewVerdictFixtureValidJSON(t)
	workspacePath := reviewVerdictFixtureWrite(t, data)

	v, err := ReadReviewVerdictVia(context.Background(), tmux.LocalRunner{}, workspacePath)
	if err != nil {
		t.Fatalf("ReadReviewVerdictVia(LocalRunner): %v", err)
	}
	if v == nil || v.Verdict != ReviewVerdictApprove {
		t.Fatalf("ReadReviewVerdictVia(LocalRunner) = %+v; want APPROVE verdict", v)
	}
}

// TestReadReviewVerdictVia_RemoteRunner_ReadsViaRunner is the core regression
// test: box-A workspace has NO review.json (a bare os.ReadFile would return
// (nil,nil) = "absent" → the false-fail), but the non-local runner's cat returns
// a valid worker-side verdict. The verdict MUST be read and validated via the
// runner.
func TestReadReviewVerdictVia_RemoteRunner_ReadsViaRunner(t *testing.T) {
	t.Parallel()

	// "Worker-side" verdict file (lives at an arbitrary box-A path the stub cats).
	workerVerdict := filepath.Join(t.TempDir(), "worker-review.json")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(workerVerdict, reviewVerdictFixtureValidJSON(t), 0o644); err != nil {
		t.Fatalf("write worker verdict: %v", err)
	}

	// box-A workspace path has NO review.json — proves the read is NOT local.
	boxAWorkspace := t.TempDir()
	if _, statErr := os.Stat(ReviewVerdictPath(boxAWorkspace)); statErr == nil {
		t.Fatal("precondition: box-A workspace must NOT contain review.json")
	}

	runner := catFromFileRunner{srcPath: workerVerdict}

	v, err := ReadReviewVerdictVia(context.Background(), runner, boxAWorkspace)
	if err != nil {
		t.Fatalf("ReadReviewVerdictVia(remote): %v", err)
	}
	if v == nil {
		t.Fatal("ReadReviewVerdictVia(remote) returned nil; want the worker-side verdict routed via the runner")
	}
	if v.Verdict != ReviewVerdictApprove {
		t.Errorf("Verdict = %q; want %q", v.Verdict, ReviewVerdictApprove)
	}
}

// emptyExit0Runner is a non-local CommandRunner stub whose Command() ALWAYS
// exits 0 with EMPTY stdout (it runs `true`). It reproduces the gb-mbp remote
// worker whose `-zsh` login rc resets $?, so `ssh cat <absent-file>` returns
// err==nil with empty stdout instead of a non-zero exit — the exit-code-masking
// root cause. Being a distinct (non-LocalRunner) type, it is classified as
// non-local, so ReadReviewVerdictVia routes through it.
type emptyExit0Runner struct{}

func (emptyExit0Runner) Command(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "true") // exit 0, empty stdout
}

// TestReadReviewVerdictVia_RemoteRunner_EmptyExit0ReturnsNil is the regression
// test for the ssh-exit-0-masking false-fail: the runner returns (empty stdout,
// nil err) for an absent verdict. The correct interpretation is absent
// (nil,nil = inconclusive), NOT ErrMalformed from feeding "" to the parser.
func TestReadReviewVerdictVia_RemoteRunner_EmptyExit0ReturnsNil(t *testing.T) {
	t.Parallel()

	boxAWorkspace := t.TempDir()
	runner := emptyExit0Runner{}

	v, err := ReadReviewVerdictVia(context.Background(), runner, boxAWorkspace)
	if err != nil {
		t.Fatalf("ReadReviewVerdictVia(empty-exit0) error = %v; want nil (absent, not ErrMalformed)", err)
	}
	if v != nil {
		t.Errorf("ReadReviewVerdictVia(empty-exit0) = %+v; want nil (absent)", v)
	}
}

// TestReadReviewVerdictVia_RemoteRunner_AbsentReturnsNil verifies that when the
// runner's cat fails (worker file absent), the result is (nil,nil) — the
// inconclusive condition per WM-027a §(e), matching the local not-exist branch.
func TestReadReviewVerdictVia_RemoteRunner_AbsentReturnsNil(t *testing.T) {
	t.Parallel()

	boxAWorkspace := t.TempDir() // also has no local file
	runner := catFromFileRunner{srcPath: ""}

	v, err := ReadReviewVerdictVia(context.Background(), runner, boxAWorkspace)
	if err != nil {
		t.Fatalf("ReadReviewVerdictVia(remote-absent) error = %v; want nil", err)
	}
	if v != nil {
		t.Errorf("ReadReviewVerdictVia(remote-absent) = %+v; want nil (absent)", v)
	}
}
