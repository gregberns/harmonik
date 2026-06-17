package daemon

// reviewtrailers_hkdyim_test.go — unit tests for appendReviewTrailersToHEAD (hk-dyim).
//
// Verifies that appendReviewTrailersToHEAD amends the HEAD commit in a git
// worktree to carry Reviewed-By: and Review-Verdict: trailers from an APPROVE
// verdict, making the review audit trail visible in git history.
//
// Bead: hk-dyim.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/workspace"
)

// initTestRepo_dyim initialises a minimal git repository in dir with a single
// initial commit that includes a Refs: trailer (mimicking the implementer commit).
func initTestRepo_dyim(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("initTestRepo_dyim: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")

	p := filepath.Join(dir, "work.txt")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(p, []byte("agent work\n"), 0o644); err != nil {
		t.Fatalf("initTestRepo_dyim: WriteFile: %v", err)
	}
	run("add", "work.txt")
	run("commit", "-m", "feat: agent work\n\nRefs: hk-test")
}

// headCommitMsg_dyim reads the HEAD commit message from dir.
func headCommitMsg_dyim(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "log", "-1", "--format=%B", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("headCommitMsg_dyim: git log: %v", err)
	}
	return string(out)
}

// TestAppendReviewTrailersToHEAD_AddsTrailers_dyim verifies that the
// Reviewed-By: and Review-Verdict: trailers are present in the HEAD commit
// message after a successful call to appendReviewTrailersToHEAD with an
// APPROVE verdict.
func TestAppendReviewTrailersToHEAD_AddsTrailers_dyim(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	initTestRepo_dyim(t, dir)

	verdict := &workspace.ReviewVerdict{
		SchemaVersion: 1,
		Verdict:       workspace.ReviewVerdictApprove,
		Flags:         []string{},
		Notes:         "All five checks pass.",
	}

	ctx := context.Background()
	if err := appendReviewTrailersToHEAD(ctx, dir, verdict); err != nil {
		t.Fatalf("appendReviewTrailersToHEAD: %v", err)
	}

	msg := headCommitMsg_dyim(t, dir)

	// Reviewed-By: trailer must be present.
	if !strings.Contains(msg, "Reviewed-By: "+reviewedByTrailerValue) {
		t.Errorf("commit message missing Reviewed-By trailer; got:\n%s", msg)
	}

	// Review-Verdict: trailer must contain valid JSON with the APPROVE verdict.
	if !strings.Contains(msg, "Review-Verdict: ") {
		t.Errorf("commit message missing Review-Verdict trailer; got:\n%s", msg)
	}

	// Find the Review-Verdict line and parse it.
	var verdictLine string
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(line, "Review-Verdict: ") {
			verdictLine = strings.TrimPrefix(line, "Review-Verdict: ")
			break
		}
	}
	if verdictLine == "" {
		t.Fatalf("Review-Verdict line not found in commit message:\n%s", msg)
	}
	var got workspace.ReviewVerdict
	if err := json.Unmarshal([]byte(verdictLine), &got); err != nil {
		t.Fatalf("Review-Verdict trailer is not valid JSON: %v; raw: %q", err, verdictLine)
	}
	if got.Verdict != workspace.ReviewVerdictApprove {
		t.Errorf("Review-Verdict.verdict = %q; want %q", got.Verdict, workspace.ReviewVerdictApprove)
	}
	if got.SchemaVersion != 1 {
		t.Errorf("Review-Verdict.schema_version = %d; want 1", got.SchemaVersion)
	}
	if got.Notes != "All five checks pass." {
		t.Errorf("Review-Verdict.notes = %q; want %q", got.Notes, "All five checks pass.")
	}
}

// TestAppendReviewTrailersToHEAD_Idempotent_dyim verifies that calling
// appendReviewTrailersToHEAD twice does not duplicate the trailers.
func TestAppendReviewTrailersToHEAD_Idempotent_dyim(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	initTestRepo_dyim(t, dir)

	verdict := &workspace.ReviewVerdict{
		SchemaVersion: 1,
		Verdict:       workspace.ReviewVerdictApprove,
		Flags:         []string{},
		Notes:         "Idempotency check.",
	}

	ctx := context.Background()
	if err := appendReviewTrailersToHEAD(ctx, dir, verdict); err != nil {
		t.Fatalf("first appendReviewTrailersToHEAD: %v", err)
	}
	if err := appendReviewTrailersToHEAD(ctx, dir, verdict); err != nil {
		t.Fatalf("second appendReviewTrailersToHEAD: %v", err)
	}

	msg := headCommitMsg_dyim(t, dir)

	// Count occurrences of "Reviewed-By:" — must appear exactly once.
	count := strings.Count(msg, "Reviewed-By: "+reviewedByTrailerValue)
	if count != 1 {
		t.Errorf("Reviewed-By: appears %d times (want 1); msg:\n%s", count, msg)
	}
}

// TestAppendReviewTrailersToHEAD_NilVerdict_dyim verifies that a nil verdict
// is a safe no-op.
func TestAppendReviewTrailersToHEAD_NilVerdict_dyim(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	initTestRepo_dyim(t, dir)

	msgBefore := headCommitMsg_dyim(t, dir)

	ctx := context.Background()
	if err := appendReviewTrailersToHEAD(ctx, dir, nil); err != nil {
		t.Fatalf("appendReviewTrailersToHEAD(nil): %v", err)
	}

	msgAfter := headCommitMsg_dyim(t, dir)
	if msgBefore != msgAfter {
		t.Errorf("nil verdict mutated commit message; before:\n%s\nafter:\n%s", msgBefore, msgAfter)
	}
}
