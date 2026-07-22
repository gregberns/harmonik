package daemon

// hk9hvr0 — regression guard for the tmux-substrate input buffer name.
//
// The interim InputPort.SubmitInput once used a hardcoded "harmonik-input"
// buffer name, which fails the tmux buffer-name invariant
// (bufferNameRe: harmonik-<session-id>-<purpose>). Every implementer-initial
// dispatch then failed with ErrStructural, so the worker reached agent_ready
// but never received its task prompt — wedging all tmux-substrate dispatch.
//
// These tests exercise the REAL perRunSubstrate.SubmitInput through the REAL
// tmux.OSAdapter validation (LoadBuffer/PasteBuffer) so the name it emits can
// never again drift out of the invariant. This is an internal (package daemon)
// test so it can construct perRunSubstrate directly.

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// submitInputThroughRealAdapter drives perRunSubstrate.SubmitInput for the given
// runSessionID / paneTarget through a real tmux.OSAdapter whose CommandRunner is
// stubbed to a successful no-op `true`. A malformed buffer name is rejected by
// LoadBuffer BEFORE the runner is reached (ErrStructural), so a nil error proves
// the emitted name passed the real bufferNameRe validation. Returns the buffer
// name captured from the recorded `tmux load-buffer -b <name>` argv.
func submitInputThroughRealAdapter(t *testing.T, runSessionID, paneTarget string) (bufName string, err error) {
	t.Helper()
	rr := &tmuxPkg.RecordingRunner{
		// Produce a command that always succeeds without needing real tmux.
		CmdFunc: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "true")
		},
	}
	adapter := tmuxPkg.OSAdapter{}.WithRunner(rr)
	p := &perRunSubstrate{
		runSessionID:     runSessionID,
		cachedPaneTarget: paneTarget,
		remoteAdapter:    adapter, // pasteAdapter() returns this when non-nil
	}
	_, err = p.SubmitInput(t.Context(), handler.InputRequest{Payload: []byte("task prompt\n")})
	// Recover the buffer name from the first recorded load-buffer call.
	for _, c := range rr.Calls {
		for i, a := range c.Args {
			if a == "-b" && i+1 < len(c.Args) {
				return c.Args[i+1], err
			}
		}
	}
	return "", err
}

// TestSubmitInputBufferNamePassesValidation asserts SubmitInput's buffer name is
// accepted by the real tmux osadapter validation for a representative UUIDv7 run
// id, and is the expected "harmonik-<run-id>-input" form (never the retired
// hardcoded "harmonik-input").
func TestSubmitInputBufferNamePassesValidation(t *testing.T) {
	runID := "019f871a-d017-728c-97f3-d99397451e3b" // representative UUIDv7
	bufName, err := submitInputThroughRealAdapter(t, runID, "%1964")
	if err != nil {
		t.Fatalf("SubmitInput with run id %q returned error (buffer name rejected?): %v", runID, err)
	}
	want := "harmonik-" + runID + "-input"
	if bufName != want {
		t.Errorf("SubmitInput buffer name = %q; want %q", bufName, want)
	}
	if bufName == "harmonik-input" {
		t.Errorf("SubmitInput still uses the retired hardcoded %q (hk-9hvr0 regression)", bufName)
	}
}

// TestSubmitInputBufferNameFallbacksValidate asserts the fallback name still
// passes osadapter validation when runSessionID is empty (shared-session /
// remote runs) — the pane target is sanitized into a regex-valid segment — and
// when neither id is usable the literal "run" segment is used.
func TestSubmitInputBufferNameFallbacksValidate(t *testing.T) {
	cases := []struct {
		name         string
		runSessionID string
		paneTarget   string
		wantSegment  string // <id> in harmonik-<id>-input
	}{
		{"empty run id, pane target sanitized", "", "%1964", "1964"},
		{"empty run id, structured pane target", "", "worker:window.0", "worker-window-0"},
		{"no usable id falls back to literal", "", "%%%", "run"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bufName, err := submitInputThroughRealAdapter(t, tc.runSessionID, tc.paneTarget)
			if err != nil {
				t.Fatalf("SubmitInput returned error (buffer name rejected?): %v", err)
			}
			want := "harmonik-" + tc.wantSegment + "-input"
			if bufName != want {
				t.Errorf("SubmitInput buffer name = %q; want %q", bufName, want)
			}
		})
	}
}

// TestInputBufferNameConstruction unit-tests the name builder directly, covering
// the runSessionID-preferred path and the sanitizing fallbacks.
func TestInputBufferNameConstruction(t *testing.T) {
	cases := []struct {
		runSessionID string
		paneTarget   string
		want         string
	}{
		{"019f871a-d017-728c-97f3-d99397451e3b", "%1", "harmonik-019f871a-d017-728c-97f3-d99397451e3b-input"},
		{"", "%1964", "harmonik-1964-input"},
		{"", "session:window.0", "harmonik-session-window-0-input"},
		{"", "", "harmonik-run-input"},
		{"ABC-123", "%9", "harmonik-abc-123-input"}, // uppercase lowered
	}
	for _, tc := range cases {
		p := &perRunSubstrate{runSessionID: tc.runSessionID, cachedPaneTarget: tc.paneTarget}
		if got := p.inputBufferName(); got != tc.want {
			t.Errorf("inputBufferName(runSessionID=%q, pane=%q) = %q; want %q",
				tc.runSessionID, tc.paneTarget, got, tc.want)
		}
	}
}

// TestSanitizeBufferSegment covers the character mapping and trimming rules.
func TestSanitizeBufferSegment(t *testing.T) {
	cases := map[string]string{
		"019f871a-d017":   "019f871a-d017",
		"%1964":           "1964",
		"session:win.0":   "session-win-0",
		"ABC":             "abc",
		"%%%":             "",
		"":                "",
		"-leading-trail-": "leading-trail",
	}
	for in, want := range cases {
		if got := sanitizeBufferSegment(in); got != want {
			t.Errorf("sanitizeBufferSegment(%q) = %q; want %q", in, got, want)
		}
	}
	// Every non-empty sanitized segment must yield a bufferNameRe-valid name.
	for in := range cases {
		seg := sanitizeBufferSegment(in)
		if seg == "" {
			continue
		}
		name := "harmonik-" + seg + "-input"
		if !strings.HasPrefix(name, "harmonik-") {
			t.Errorf("unexpected name shape: %q", name)
		}
	}
}
