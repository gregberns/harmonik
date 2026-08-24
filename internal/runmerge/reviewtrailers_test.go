package runmerge

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

func initTestRepoDyim(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("initTestRepoDyim: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")

	p := filepath.Join(dir, "work.txt")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(p, []byte("agent work\n"), 0o644); err != nil {
		t.Fatalf("initTestRepoDyim: WriteFile: %v", err)
	}
	run("add", "work.txt")
	run("commit", "-m", "feat: agent work\n\nKeep this body in this exact order.\nIt has two lines.\n\nRefs: hk-test\nReviewed-By: none\nReview-Verdict: {\"schema_version\":1,\"verdict\":\"NOT_REVIEWED\",\"flags\":[\"no-reviewer-reached\"],\"notes\":\"No reviewer was reached.\"}")
}

func headCommitMsgDyim(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "log", "-1", "--format=%B", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("headCommitMsgDyim: git log: %v", err)
	}
	return string(out)
}

// TestReplaceReviewTrailersOnHEAD_AddsTrailers_dyim verifies that the
// Reviewed-By: and Review-Verdict: trailers are present in the HEAD commit
// message after a successful call to ReplaceReviewTrailersOnHEAD with an
// APPROVE verdict.
func TestReplaceReviewTrailersOnHEAD_AddsTrailers_dyim(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	initTestRepoDyim(t, dir)

	verdict := &workspace.ReviewVerdict{
		SchemaVersion: 1,
		Verdict:       workspace.ReviewVerdictApprove,
		Flags:         []string{},
		Notes:         "All five checks pass.",
	}

	ctx := context.Background()
	if err := ReplaceReviewTrailersOnHEAD(ctx, dir, verdict); err != nil {
		t.Fatalf("ReplaceReviewTrailersOnHEAD: %v", err)
	}

	msg := headCommitMsgDyim(t, dir)
	wantPreserved := "feat: agent work\n\nKeep this body in this exact order.\nIt has two lines.\n\nRefs: hk-test\n"
	if !strings.HasPrefix(msg, wantPreserved) {
		t.Errorf("subject or body changed during trailer replacement; got:\n%s", msg)
	}
	if got := strings.Count(msg, "Reviewed-By:"); got != 1 {
		t.Errorf("commit message carries %d Reviewed-By lines, want 1; got:\n%s", got, msg)
	}

	if !strings.Contains(msg, "Reviewed-By: "+reviewedByTrailerValue) {
		t.Errorf("commit message missing Reviewed-By trailer; got:\n%s", msg)
	}

	if !strings.Contains(msg, "Review-Verdict: ") {
		t.Errorf("commit message missing Review-Verdict trailer; got:\n%s", msg)
	}

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

// TestReplaceReviewTrailersOnHEAD_Idempotent_dyim verifies that calling
// ReplaceReviewTrailersOnHEAD twice does not duplicate the trailers.
func TestReplaceReviewTrailersOnHEAD_Idempotent_dyim(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	initTestRepoDyim(t, dir)

	verdict := &workspace.ReviewVerdict{
		SchemaVersion: 1,
		Verdict:       workspace.ReviewVerdictApprove,
		Flags:         []string{},
		Notes:         "Idempotency check.",
	}

	ctx := context.Background()
	if err := ReplaceReviewTrailersOnHEAD(ctx, dir, verdict); err != nil {
		t.Fatalf("first ReplaceReviewTrailersOnHEAD: %v", err)
	}
	if err := ReplaceReviewTrailersOnHEAD(ctx, dir, verdict); err != nil {
		t.Fatalf("second ReplaceReviewTrailersOnHEAD: %v", err)
	}

	msg := headCommitMsgDyim(t, dir)

	count := strings.Count(msg, "Reviewed-By: "+reviewedByTrailerValue)
	if count != 1 {
		t.Errorf("Reviewed-By: appears %d times (want 1); msg:\n%s", count, msg)
	}
}

// TestReplaceReviewTrailersOnHEAD_NilVerdict_dyim verifies that a nil verdict
// is a safe no-op.
func TestReplaceReviewTrailersOnHEAD_NilVerdict_dyim(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	initTestRepoDyim(t, dir)

	msgBefore := headCommitMsgDyim(t, dir)

	ctx := context.Background()
	if err := ReplaceReviewTrailersOnHEAD(ctx, dir, nil); err != nil {
		t.Fatalf("ReplaceReviewTrailersOnHEAD(nil): %v", err)
	}

	msgAfter := headCommitMsgDyim(t, dir)
	if msgBefore != msgAfter {
		t.Errorf("nil verdict mutated commit message; before:\n%s\nafter:\n%s", msgBefore, msgAfter)
	}
}
