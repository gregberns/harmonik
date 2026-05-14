package daemon_test

// pasteinject_hkzrj83_test.go — unit tests for the paste-inject step (hk-zrj83).
//
// Helper prefix: pasteInjectFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-zrj83).
//
// Tests verify:
//  1. Call order: file-stat check → WriteLastPane.
//  2. Buffer-name format: "harmonik-<session-id>-<purpose>".
//  3. Phase mapping: implementer-initial → "task"; reviewer → "review";
//     implementer-resume → "task" then "feedback".
//  4. Stat-check failure → inject skipped (non-fatal); WriteLastPane NOT called.
//  5. Nil substrate → no-op (no calls).
//  6. WriteToPane error → logged, not fatal to workloop.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fake tmux.Adapter for paste-inject tests
// ─────────────────────────────────────────────────────────────────────────────

// pasteInjectFixtureAdapter is a recording fake for tmux.Adapter.
// It records WriteToPane calls and can inject controlled errors.
type pasteInjectFixtureAdapter struct {
	mu sync.Mutex

	// writeToPaneCalls records each (bufferName, paneTarget, payload) triplet.
	writeToPaneCalls []pasteInjectFixtureWriteToPane

	// writeToPaneErr, when non-nil, is returned by WriteToPane.
	writeToPaneErr error

	// newWindowInOutcome is returned by NewWindowIn.
	newWindowInOutcome tmux.Outcome

	// paneIDResult is returned by WindowPaneID. When empty, WindowPaneID
	// returns "" (triggering the handle+".0" fallback in WriteLastPane).
	// Set to a "%NNNN" value to exercise the pane-ID fast path (hk-yngq2).
	paneIDResult string
}

type pasteInjectFixtureWriteToPane struct {
	bufferName string
	paneTarget string
	payload    string
}

func (a *pasteInjectFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *pasteInjectFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *pasteInjectFixtureAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *pasteInjectFixtureAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	return a.newWindowInOutcome
}
func (a *pasteInjectFixtureAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	return nil
}
func (a *pasteInjectFixtureAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 1234, nil
}
func (a *pasteInjectFixtureAdapter) KillSession(_ context.Context, _ string) error { return nil }

// WindowPaneID returns paneIDResult when set, or "" to trigger the
// handle+".0" fallback in WriteLastPane.
func (a *pasteInjectFixtureAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return a.paneIDResult, nil
}

func (a *pasteInjectFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *pasteInjectFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error { return nil }
func (a *pasteInjectFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}
func (a *pasteInjectFixtureAdapter) WriteToPane(_ context.Context, bufferName, paneTarget string, payload []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writeToPaneCalls = append(a.writeToPaneCalls, pasteInjectFixtureWriteToPane{
		bufferName: bufferName,
		paneTarget: paneTarget,
		payload:    string(payload),
	})
	return a.writeToPaneErr
}

// calls returns the recorded WriteToPane calls (copy, safe for assertions).
func (a *pasteInjectFixtureAdapter) calls() []pasteInjectFixtureWriteToPane {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]pasteInjectFixtureWriteToPane, len(a.writeToPaneCalls))
	copy(out, a.writeToPaneCalls)
	return out
}

// Compile-time assertion: pasteInjectFixtureAdapter implements tmux.Adapter.
var _ tmux.Adapter = (*pasteInjectFixtureAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// pasteInjectFixtureSubstrate creates a tmuxSubstrate backed by the fake
// adapter.  The substrate's SpawnWindow is called once to prime lastHandle so
// WriteLastPane has a valid pane target.
func pasteInjectFixtureSubstrate(t *testing.T, adapter *pasteInjectFixtureAdapter) handler.Substrate {
	t.Helper()
	// Prime the adapter with a successful NewWindowIn outcome.
	adapter.newWindowInOutcome = tmux.Outcome{
		Handle: tmux.WindowHandle("harmonik-proj:task-window"),
		Err:    nil,
	}
	sub := daemon.NewTmuxSubstrate(adapter, "harmonik-proj")
	// Call SpawnWindow to set lastHandle.
	_, err := sub.SpawnWindow(t.Context(), handler.SubstrateSpawn{
		WindowName: "task-window",
		Cwd:        "/tmp",
		Env:        nil,
		Argv:       []string{"/usr/bin/claude"},
	})
	if err != nil {
		t.Fatalf("pasteInjectFixtureSubstrate: SpawnWindow: %v", err)
	}
	return sub
}

// pasteInjectFixtureTaskFile creates a non-empty task file at
// <wtPath>/.harmonik/<name> and registers cleanup.
func pasteInjectFixtureTaskFile(t *testing.T, wtPath, name, content string) {
	t.Helper()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("pasteInjectFixtureTaskFile: mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("pasteInjectFixtureTaskFile: write: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — pasteInjectOnLaunch (via exported wrapper in export_test.go)
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectOnLaunch_ImplementerInitial verifies that:
//   - A single WriteToPane call fires.
//   - bufferName is "harmonik-<sessionID>-task".
//   - paneTarget ends with ".0".
//   - Payload mentions "agent-task.md".
func TestPasteInjectOnLaunch_ImplementerInitial(t *testing.T) {
	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\nDo something.\n")

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	const sessionID = "01hwxyz-abc123"
	daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhase(""), // empty = single-mode / implementer-initial
		1, wtPath,
	)

	calls := adapter.calls()
	if len(calls) != 1 {
		t.Fatalf("implementer-initial: expected 1 WriteToPane call, got %d", len(calls))
	}
	c := calls[0]

	wantBuf := fmt.Sprintf("harmonik-%s-task", sessionID)
	if c.bufferName != wantBuf {
		t.Errorf("implementer-initial: bufferName = %q, want %q", c.bufferName, wantBuf)
	}
	if !strings.HasSuffix(c.paneTarget, ".0") {
		t.Errorf("implementer-initial: paneTarget = %q, want suffix .0", c.paneTarget)
	}
	if !strings.Contains(c.payload, "agent-task.md") {
		t.Errorf("implementer-initial: payload = %q, want mention of agent-task.md", c.payload)
	}
}

// TestPasteInjectOnLaunch_Reviewer verifies that:
//   - A single WriteToPane call fires.
//   - bufferName uses purpose "review".
//   - Payload mentions "review-target.md".
func TestPasteInjectOnLaunch_Reviewer(t *testing.T) {
	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "review-target.md", "# Review target\n")

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	const sessionID = "01hwxyz-rev456"
	daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhaseReviewer,
		1, wtPath,
	)

	calls := adapter.calls()
	if len(calls) != 1 {
		t.Fatalf("reviewer: expected 1 WriteToPane call, got %d", len(calls))
	}
	c := calls[0]

	wantBuf := fmt.Sprintf("harmonik-%s-review", sessionID)
	if c.bufferName != wantBuf {
		t.Errorf("reviewer: bufferName = %q, want %q", c.bufferName, wantBuf)
	}
	if !strings.Contains(c.payload, "review-target.md") {
		t.Errorf("reviewer: payload = %q, want mention of review-target.md", c.payload)
	}
}

// TestPasteInjectOnLaunch_ImplementerResume verifies that:
//   - Two WriteToPane calls fire: first "task", then "feedback".
//   - The feedback buffer name uses purpose "feedback".
//   - The feedback payload mentions the prior iteration file.
func TestPasteInjectOnLaunch_ImplementerResume(t *testing.T) {
	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\nDo something.\n")
	// iterCount=2 → prior iter=1 → "reviewer-feedback.iter-1.md"
	pasteInjectFixtureTaskFile(t, wtPath, "reviewer-feedback.iter-1.md", "# Feedback\nFix the thing.\n")

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	const sessionID = "01hwxyz-resume789"
	daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhaseImplementerResume,
		2, wtPath,
	)

	calls := adapter.calls()
	if len(calls) != 2 {
		t.Fatalf("implementer-resume: expected 2 WriteToPane calls, got %d", len(calls))
	}

	// Call 0: task.
	wantTaskBuf := fmt.Sprintf("harmonik-%s-task", sessionID)
	if calls[0].bufferName != wantTaskBuf {
		t.Errorf("implementer-resume call[0]: bufferName = %q, want %q", calls[0].bufferName, wantTaskBuf)
	}
	if !strings.Contains(calls[0].payload, "agent-task.md") {
		t.Errorf("implementer-resume call[0]: payload = %q, want mention of agent-task.md", calls[0].payload)
	}

	// Call 1: feedback.
	wantFeedbackBuf := fmt.Sprintf("harmonik-%s-feedback", sessionID)
	if calls[1].bufferName != wantFeedbackBuf {
		t.Errorf("implementer-resume call[1]: bufferName = %q, want %q", calls[1].bufferName, wantFeedbackBuf)
	}
	if !strings.Contains(calls[1].payload, "reviewer-feedback.iter-1.md") {
		t.Errorf("implementer-resume call[1]: payload = %q, want mention of reviewer-feedback.iter-1.md", calls[1].payload)
	}
}

// TestPasteInjectOnLaunch_TaskFileMissing verifies that when the task file is
// absent, no WriteToPane call is made (non-fatal stat-check failure).
func TestPasteInjectOnLaunch_TaskFileMissing(t *testing.T) {
	wtPath := t.TempDir()
	// Intentionally do NOT create agent-task.md.

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, "session-missing",
		handlercontract.ReviewLoopPhase(""), // implementer-initial
		1, wtPath,
	)

	if calls := adapter.calls(); len(calls) != 0 {
		t.Errorf("task file missing: expected 0 WriteToPane calls, got %d", len(calls))
	}
}

// TestPasteInjectOnLaunch_NilSubstrate verifies that a nil substrate is a no-op.
func TestPasteInjectOnLaunch_NilSubstrate(t *testing.T) {
	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\n")

	adapter := &pasteInjectFixtureAdapter{}

	// nil substrate — no panic, no calls.
	daemon.ExportedPasteInjectOnLaunch(
		t.Context(), nil, "session-nil",
		handlercontract.ReviewLoopPhase(""),
		1, wtPath,
	)
	if calls := adapter.calls(); len(calls) != 0 {
		t.Errorf("nil substrate: expected 0 WriteToPane calls, got %d", len(calls))
	}
}

// TestPasteInjectOnLaunch_WriteToPaneError verifies that a WriteToPane error
// does not panic or return an error — it is non-fatal (logged only).
func TestPasteInjectOnLaunch_WriteToPaneError(t *testing.T) {
	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\n")

	adapter := &pasteInjectFixtureAdapter{
		writeToPaneErr: fmt.Errorf("simulated tmux failure"),
	}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	// Must not panic — error is logged to stderr and swallowed.
	daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, "session-err",
		handlercontract.ReviewLoopPhase(""),
		1, wtPath,
	)
	// WriteToPane WAS called (it just returned an error).
	if calls := adapter.calls(); len(calls) != 1 {
		t.Errorf("write error: expected 1 WriteToPane call (error is non-fatal), got %d", len(calls))
	}
}

// TestPasteInjectBufferNameFormat verifies the buffer-name format independently
// of the substrate machinery.
func TestPasteInjectBufferNameFormat(t *testing.T) {
	cases := []struct {
		sessionID string
		purpose   string
		want      string
	}{
		{"01hwxyz-abc123", "task", "harmonik-01hwxyz-abc123-task"},
		{"01hwxyz-abc123", "feedback", "harmonik-01hwxyz-abc123-feedback"},
		{"01hwxyz-abc123", "review", "harmonik-01hwxyz-abc123-review"},
	}
	for _, tc := range cases {
		got := daemon.ExportedBufferName(tc.sessionID, tc.purpose)
		if got != tc.want {
			t.Errorf("bufferName(%q, %q) = %q, want %q", tc.sessionID, tc.purpose, got, tc.want)
		}
	}
}
