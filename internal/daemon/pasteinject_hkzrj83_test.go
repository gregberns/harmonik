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
//     implementer-resume → "task" (combined task+feedback, hk-poy7k).
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
func (a *pasteInjectFixtureAdapter) SendKeysEnter(_ context.Context, _ string) error { return nil }
func (a *pasteInjectFixtureAdapter) SendKeysQuit(_ context.Context, _ string) error  { return nil }
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

// pasteInjectFixtureSubstrate creates a perRunSubstrate backed by the fake
// adapter. SpawnWindow is called once to capture the pane target so
// WriteLastPane has a valid pane target.
//
// hk-jfh59: WriteLastPane/SendEnterToLastPane/SendQuitToLastPane are now on
// perRunSubstrate, not tmuxSubstrate. The fixture wraps the shared substrate
// in a perRunSubstrate (the production path) and calls SpawnWindow on it.
func pasteInjectFixtureSubstrate(t *testing.T, adapter *pasteInjectFixtureAdapter) handler.Substrate {
	t.Helper()
	// Prime the adapter with a successful NewWindowIn outcome.
	adapter.newWindowInOutcome = tmux.Outcome{
		Handle: tmux.WindowHandle("harmonik-proj:task-window"),
		Err:    nil,
	}
	sharedSub := daemon.NewTmuxSubstrate(adapter, "harmonik-proj")
	// Wrap in perRunSubstrate so pasteInjectOnLaunch finds the pasteInjecter interface.
	prs := daemon.ExportedNewPerRunSubstrate(sharedSub)
	// Call SpawnWindow to capture the pane target.
	_, err := prs.SpawnWindow(t.Context(), handler.SubstrateSpawn{
		WindowName: "task-window",
		Cwd:        "/tmp",
		Env:        nil,
		Argv:       []string{"/usr/bin/claude"},
	})
	if err != nil {
		t.Fatalf("pasteInjectFixtureSubstrate: SpawnWindow: %v", err)
	}
	return prs
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
	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhase(""), // empty = single-mode / implementer-initial
		1, wtPath,
	)
	<-briefDelivered

	calls := adapter.calls()
	if len(calls) != 1 {
		t.Fatalf("implementer-initial: expected 1 WriteToPane call, got %d", len(calls))
	}
	c := calls[0]

	// T8: the daemon-run delivery routes through the AIS InputPort.SubmitInput,
	// whose interim tmux driver uses the single AIS input buffer (not the former
	// per-phase "harmonik-<sessionID>-task" name — that discipline now applies
	// only to the keeper/CLI paste paths per the PL-021d carve-out).
	wantBuf := daemon.ExportedInputBufferName(sub)
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
	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhaseReviewer,
		1, wtPath,
	)
	<-briefDelivered

	calls := adapter.calls()
	if len(calls) != 1 {
		t.Fatalf("reviewer: expected 1 WriteToPane call, got %d", len(calls))
	}
	c := calls[0]

	// T8: daemon-run delivery via AIS InputPort.SubmitInput → single AIS input
	// buffer (see implementer-initial note above).
	wantBuf := daemon.ExportedInputBufferName(sub)
	if c.bufferName != wantBuf {
		t.Errorf("reviewer: bufferName = %q, want %q", c.bufferName, wantBuf)
	}
	if !strings.Contains(c.payload, "review-target.md") {
		t.Errorf("reviewer: payload = %q, want mention of review-target.md", c.payload)
	}
}

// TestPasteInjectOnLaunch_ImplementerResume verifies that:
//   - Exactly ONE WriteToPane call fires (task + feedback combined, hk-poy7k).
//   - The buffer name uses purpose "task".
//   - The single payload contains both "agent-task.md" and "reviewer-feedback.iter-1.md"
//     as distinct readable content.
func TestPasteInjectOnLaunch_ImplementerResume(t *testing.T) {
	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\nDo something.\n")
	// iterCount=2 → prior iter=1 → "reviewer-feedback.iter-1.md"
	pasteInjectFixtureTaskFile(t, wtPath, "reviewer-feedback.iter-1.md", "# Feedback\nFix the thing.\n")

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	const sessionID = "01hwxyz-resume789"
	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhaseImplementerResume,
		2, wtPath,
	)
	<-briefDelivered

	calls := adapter.calls()
	// hk-poy7k: task + feedback are combined into ONE paste to eliminate the
	// inter-message race where a second Enter was sent before Claude returned
	// to the REPL prompt, causing the feedback to be dropped.
	if len(calls) != 1 {
		t.Fatalf("implementer-resume: expected 1 WriteToPane call (combined task+feedback), got %d", len(calls))
	}

	// T8: daemon-run delivery via AIS InputPort.SubmitInput → single AIS input
	// buffer (see implementer-initial note above).
	wantBuf := daemon.ExportedInputBufferName(sub)
	if calls[0].bufferName != wantBuf {
		t.Errorf("implementer-resume: bufferName = %q, want %q", calls[0].bufferName, wantBuf)
	}
	// Both messages must be readable as distinct content within the single payload.
	if !strings.Contains(calls[0].payload, "agent-task.md") {
		t.Errorf("implementer-resume: payload = %q, want mention of agent-task.md", calls[0].payload)
	}
	if !strings.Contains(calls[0].payload, "reviewer-feedback.iter-1.md") {
		t.Errorf("implementer-resume: payload = %q, want mention of reviewer-feedback.iter-1.md", calls[0].payload)
	}
}

// TestPasteInjectOnLaunch_TaskFileMissing verifies that when the task file is
// absent, no WriteToPane call is made (non-fatal stat-check failure).
func TestPasteInjectOnLaunch_TaskFileMissing(t *testing.T) {
	wtPath := t.TempDir()
	// Intentionally do NOT create agent-task.md.

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, "session-missing",
		handlercontract.ReviewLoopPhase(""), // implementer-initial
		1, wtPath,
	)
	<-briefDelivered

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
	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), nil, "session-nil",
		handlercontract.ReviewLoopPhase(""),
		1, wtPath,
	)
	<-briefDelivered

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
	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, "session-err",
		handlercontract.ReviewLoopPhase(""),
		1, wtPath,
	)
	<-briefDelivered

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
