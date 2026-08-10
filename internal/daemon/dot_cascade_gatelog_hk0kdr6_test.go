package daemon

// dot_cascade_gatelog_hk0kdr6_test.go — the failed commit gate's log must outlive
// the worktree it ran in (hk-0kdr6).
//
// Pre-fix: dispatchDotToolNode wrote the gate output to
// <worktree>/.harmonik/commit-gate.log and named that path in the daemon log. The
// worktree is removed on the run's terminal transition, so by the time a cell
// reported RED the named path did not exist and nothing said WHY the merge
// decision failed. One live run made four gate attempts and left zero readable
// logs.
//
// Post-fix: every failed attempt ALSO appends to
// <projectDir>/.harmonik/gate-logs/<run_id>/<node_id>.log, which nothing removes,
// and the reported path is that one.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func gateLogNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("gateLogNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

// gateLogFailingNode returns a shell tool node whose command prints marker on
// stdout and exits non-zero — the deterministic-FAIL shape of a red commit gate.
func gateLogFailingNode(marker string) *dot.Node {
	return &dot.Node{
		ID:          "commit_gate",
		Type:        core.NodeTypeNonAgentic,
		HandlerRef:  "shell",
		ToolCommand: "echo " + marker + "; exit 2",
		Timeout:     "30",
	}
}

func gateLogArchiveFile(projectDir string, runID core.RunID, nodeID string) string {
	return filepath.Join(projectDir, ".harmonik", gateLogArchiveDir, runID.String(), nodeID+".log")
}

// TestGateLogArchive_SurvivesWorktreeRemoval is the bug itself: read the gate log
// AFTER the worktree is gone, which is the only moment anybody ever wants it.
func TestGateLogArchive_SurvivesWorktreeRemoval(t *testing.T) {
	projectDir := t.TempDir()
	wtPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o700); err != nil {
		t.Fatalf("mkdir worktree .harmonik: %v", err)
	}
	runID := gateLogNewRunID(t)
	node := gateLogFailingNode("GATE_DIAGNOSTIC_MARKER")

	outcome, err := dispatchDotToolNode(context.Background(), nil, runID, nil, projectDir, wtPath, node, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Fatalf("expected FAIL from `exit 2`, got %q", outcome.Status)
	}

	// The run ends: the worktree, and the worktree copy of the log with it, go away.
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}

	archive := gateLogArchiveFile(projectDir, runID, "commit_gate")
	data, err := os.ReadFile(archive) //nolint:gosec // G304: archive is a test-local path under t.TempDir()
	if err != nil {
		t.Fatalf("gate log archive %s unreadable after the worktree was removed: %v", archive, err)
	}
	if !strings.Contains(string(data), "GATE_DIAGNOSTIC_MARKER") {
		t.Fatalf("archive does not carry the gate output; got %q", string(data))
	}
	if !strings.Contains(string(data), "node=commit_gate") || !strings.Contains(string(data), runID.String()) {
		t.Fatalf("archive header does not identify the attempt; got %q", string(data))
	}
}

// TestGateLogArchive_KeepsEveryAttempt — one run makes several attempts at the
// same gate node (the FAIL back-edge to implement re-enters it), and diagnosis
// means comparing them. A truncating write would leave only the last.
func TestGateLogArchive_KeepsEveryAttempt(t *testing.T) {
	projectDir := t.TempDir()
	wtPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o700); err != nil {
		t.Fatalf("mkdir worktree .harmonik: %v", err)
	}
	runID := gateLogNewRunID(t)

	for _, marker := range []string{"ATTEMPT_ONE", "ATTEMPT_TWO", "ATTEMPT_THREE"} {
		if _, err := dispatchDotToolNode(context.Background(), nil, runID, nil, projectDir, wtPath, gateLogFailingNode(marker), nil); err != nil {
			t.Fatalf("dispatch %s: %v", marker, err)
		}
	}

	archive := gateLogArchiveFile(projectDir, runID, "commit_gate")
	data, err := os.ReadFile(archive) //nolint:gosec // G304: archive is a test-local path under t.TempDir()
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	for _, marker := range []string{"ATTEMPT_ONE", "ATTEMPT_TWO", "ATTEMPT_THREE"} {
		if !strings.Contains(string(data), marker) {
			t.Fatalf("attempt %s missing from the archive; got %q", marker, string(data))
		}
	}
	if n := strings.Count(string(data), "===== gate attempt:"); n != 3 {
		t.Fatalf("expected 3 attempt headers, got %d", n)
	}
}

// TestGateLogArchive_RemoteRunIsArchived — for a REMOTE run the worktree is on
// the worker and the worktree copy is skipped entirely, so the archive is the
// ONLY copy. The bytes came back over the runner, and the archive is on box A.
func TestGateLogArchive_RemoteRunIsArchived(t *testing.T) {
	projectDir := t.TempDir()
	runID := gateLogNewRunID(t)
	rr := &tmux.RecordingRunner{} // nil CmdFunc → execs locally, standing in for the worker

	outcome, err := dispatchDotToolNode(context.Background(), nil, runID, rr, projectDir, t.TempDir(), gateLogFailingNode("REMOTE_MARKER"), nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Fatalf("expected FAIL, got %q", outcome.Status)
	}

	data, err := os.ReadFile(gateLogArchiveFile(projectDir, runID, "commit_gate"))
	if err != nil {
		t.Fatalf("remote gate produced no archive: %v", err)
	}
	if !strings.Contains(string(data), "REMOTE_MARKER") {
		t.Fatalf("archive does not carry the remote gate output; got %q", string(data))
	}
}

// TestGateLogArchive_GreenGateWritesNothing — a passing gate returns before the
// log paths are touched. Archiving green output would bury the red ones.
func TestGateLogArchive_GreenGateWritesNothing(t *testing.T) {
	projectDir := t.TempDir()
	runID := gateLogNewRunID(t)
	node := &dot.Node{
		ID:          "commit_gate",
		Type:        core.NodeTypeNonAgentic,
		HandlerRef:  "shell",
		ToolCommand: "echo green",
		Timeout:     "30",
	}

	outcome, err := dispatchDotToolNode(context.Background(), nil, runID, nil, projectDir, t.TempDir(), node, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS, got %q", outcome.Status)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, ".harmonik", gateLogArchiveDir)); !os.IsNotExist(statErr) {
		t.Fatalf("green gate created a gate-log archive dir (stat err = %v)", statErr)
	}
}

// TestSanitizeGateLogName_NoTraversal — node IDs are author-supplied, so the name
// must not be able to decide where the daemon writes.
func TestSanitizeGateLogName_NoTraversal(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"commit_gate", "commit_gate"},
		// Separators are what makes a name decide a directory; dots that cannot
		// separate anything are harmless in a single component.
		{"../../etc/passwd", ".._.._etc_passwd"},
		{"a/b", "a_b"},
		{"", "node"},
		{"..", "node"},
	} {
		if got := sanitizeGateLogName(tc.in); got != tc.want {
			t.Errorf("sanitizeGateLogName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
