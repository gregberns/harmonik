package daemon_test

// tmuxsubstrate_hkrpr6_test.go — regression tests for the codex argv shattering
// and PTY stdin-EOF block (hk-rpr6).
//
// # The bugs
//
// BUG-1 (argv shattering): tmuxsubstrate.go joined argv with strings.Join and
// passed the result to `tmux new-window -- <command>`. tmux runs that via
// `sh -c`, which re-word-splits on whitespace. The codex seed prompt is a
// multi-word string, so sh -c shattered it: codex received ARGC=15 individual
// word fragments instead of one arg → `error: unexpected argument` → exit 2 ~3.5s.
//
// BUG-2 (PTY stdin-EOF block): once BUG-1 is fixed, codex 0.139.0 would block
// on "Reading additional input from stdin..." because the tmux pane PTY never
// sends EOF on stdin. Redirecting stdin from /dev/null unblocks it.
//
// # What is tested
//
//   - TestShellQuoteArg_*: unit tests for the shellQuoteArg helper
//     (simple string, string with spaces, string with single-quotes).
//
//   - TestSpawnWindow_ArgvWithSpaces_ShellQuoted: SpawnWindow with an Argv
//     containing spaces produces a shell-command string where the spaced
//     element is enclosed in single-quotes, surviving sh -c as one token.
//     (AC1 of hk-rpr6 done-means.)
//
//   - TestSpawnWindow_ProcessExit_StdinDevNull: when SubstrateSpawn.StdinDevNull
//     is true the command string ends with " < /dev/null". (AC2.)
//
//   - TestSpawnWindow_Claude_NoStdinDevNull: when StdinDevNull is false (the
//     claude/paste-inject path) the command string does NOT contain "/dev/null".
//     (AC3 — claude path unchanged.)
//
// # Helper prefix
//
// Helpers use the prefix "hkrpr6" to avoid redeclaration collisions with
// parallel daemon test beads (implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// hkrpr6CommandCapturingAdapter — captures the NewWindowIn params for inspection
// ─────────────────────────────────────────────────────────────────────────────

// hkrpr6CommandCapturingAdapter is a deterministic test double for tmux.Adapter
// that records the most recent NewWindowIn command string.
type hkrpr6CommandCapturingAdapter struct {
	capturedCommand string
	capturedParams  tmux.NewWindowIn
	panePIDResult   int
}

func (a *hkrpr6CommandCapturingAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *hkrpr6CommandCapturingAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}

func (a *hkrpr6CommandCapturingAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *hkrpr6CommandCapturingAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.capturedCommand = params.Command
	a.capturedParams = params
	return tmux.Outcome{Handle: tmux.WindowHandle("test-session:hkrpr6-window")}
}

func (a *hkrpr6CommandCapturingAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	return nil
}

func (a *hkrpr6CommandCapturingAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return a.panePIDResult, nil
}

func (a *hkrpr6CommandCapturingAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *hkrpr6CommandCapturingAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *hkrpr6CommandCapturingAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (a *hkrpr6CommandCapturingAdapter) PasteBuffer(_ context.Context, _, _ string) error {
	return nil
}

func (a *hkrpr6CommandCapturingAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}

func (a *hkrpr6CommandCapturingAdapter) SendKeysEnter(_ context.Context, _ string) error {
	return nil
}

func (a *hkrpr6CommandCapturingAdapter) SendKeysQuit(_ context.Context, _ string) error {
	return nil
}

func (a *hkrpr6CommandCapturingAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*hkrpr6CommandCapturingAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// shellQuoteArg unit tests (AC1 partial — function-level)
// ─────────────────────────────────────────────────────────────────────────────

// TestShellQuoteArg_SimpleString verifies a simple word is wrapped in single-quotes.
func TestShellQuoteArg_SimpleString(t *testing.T) {
	t.Parallel()
	got := daemon.ExportedShellQuoteArg("codex")
	if got != "'codex'" {
		t.Errorf("shellQuoteArg(%q) = %q; want %q", "codex", got, "'codex'")
	}
}

// TestShellQuoteArg_StringWithSpaces verifies a multi-word string is kept intact
// as a single token after sh -c splitting.
func TestShellQuoteArg_StringWithSpaces(t *testing.T) {
	t.Parallel()
	input := "Read .harmonik/agent-task.md and implement the task"
	got := daemon.ExportedShellQuoteArg(input)
	// Must start with ' and end with '
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("shellQuoteArg(%q) = %q; want single-quoted output", input, got)
	}
	// When passed to sh -c, the output must decode back to the original string.
	// Verify by checking the interior matches the input (after stripping outer quotes).
	inner := got[1 : len(got)-1]
	if inner != input {
		t.Errorf("shellQuoteArg(%q) interior = %q; want %q", input, inner, input)
	}
}

// TestShellQuoteArg_StringWithSingleQuote verifies embedded single-quotes are
// escaped correctly using the '\” sequence.
func TestShellQuoteArg_StringWithSingleQuote(t *testing.T) {
	t.Parallel()
	input := "it's done"
	got := daemon.ExportedShellQuoteArg(input)
	want := `'it'\''s done'`
	if got != want {
		t.Errorf("shellQuoteArg(%q) = %q; want %q", input, got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SpawnWindow integration tests (AC1–AC3 of hk-rpr6 done-means)
// ─────────────────────────────────────────────────────────────────────────────

// TestSpawnWindow_ArgvWithSpaces_ShellQuoted verifies AC1: an argv element
// containing spaces is enclosed in single-quotes in the command passed to
// NewWindowIn, preventing sh -c from splitting it across multiple tokens.
func TestSpawnWindow_ArgvWithSpaces_ShellQuoted(t *testing.T) {
	t.Parallel()

	adapter := &hkrpr6CommandCapturingAdapter{panePIDResult: 999}
	substrate := daemon.NewTmuxSubstrate(adapter, "hkrpr6-session")

	seedPrompt := "Read .harmonik/agent-task.md and implement the task. Refs: hk-rpr6"
	spawn := handler.SubstrateSpawn{
		WindowName: "hkrpr6-window",
		Cwd:        t.TempDir(),
		Argv:       []string{"codex", "exec", "--json", seedPrompt},
	}

	_, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	cmd := adapter.capturedCommand
	if cmd == "" {
		t.Fatal("AC1: no command captured from NewWindowIn")
	}

	// The seed prompt (which contains spaces) must appear as a single shell
	// token enclosed in single-quotes, NOT split across whitespace.
	wantToken := "'" + seedPrompt + "'"
	if !strings.Contains(cmd, wantToken) {
		t.Errorf("AC1 FAIL: command %q does not contain shell-quoted seed %q\n"+
			"The codex seed prompt was NOT protected against sh -c word-split (hk-rpr6).",
			cmd, wantToken)
	}

	// The seed prompt must NOT appear as bare words: the very first word "Read"
	// must not appear unquoted (preceded by a space or at position 0).
	// Checking for " Read " as a bare-word indicator is sufficient.
	if strings.Contains(cmd, " Read ") {
		t.Errorf("AC1 FAIL: command %q contains bare word 'Read' — seed prompt was split by sh -c (hk-rpr6).",
			cmd)
	}
}

// TestSpawnWindow_ProcessExit_StdinDevNull verifies AC2: when StdinDevNull is
// true (ProcessExit harness / codex), the command passed to NewWindowIn ends
// with "< /dev/null" so the codex process does not block on pane PTY stdin.
func TestSpawnWindow_ProcessExit_StdinDevNull(t *testing.T) {
	t.Parallel()

	adapter := &hkrpr6CommandCapturingAdapter{panePIDResult: 1000}
	substrate := daemon.NewTmuxSubstrate(adapter, "hkrpr6-session")

	spawn := handler.SubstrateSpawn{
		WindowName:   "hkrpr6-codex",
		Cwd:          t.TempDir(),
		Argv:         []string{"codex", "exec", "--json", "seed prompt here"},
		StdinDevNull: true, // ProcessExit / codex harness
	}

	_, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	cmd := adapter.capturedCommand
	if !strings.HasSuffix(cmd, "< /dev/null") {
		t.Errorf("AC2 FAIL: command %q does not end with '< /dev/null'\n"+
			"ProcessExit harnesses need stdin redirected to unblock codex PTY stdin wait (hk-rpr6).",
			cmd)
	}
}

// TestSpawnWindow_Claude_NoStdinDevNull verifies AC3: when StdinDevNull is false
// (the claude / paste-inject path), the command does NOT contain "/dev/null".
// The claude path must be unchanged by the hk-rpr6 fix.
func TestSpawnWindow_Claude_NoStdinDevNull(t *testing.T) {
	t.Parallel()

	adapter := &hkrpr6CommandCapturingAdapter{panePIDResult: 1001}
	substrate := daemon.NewTmuxSubstrate(adapter, "hkrpr6-session")

	spawn := handler.SubstrateSpawn{
		WindowName:   "hkrpr6-claude",
		Cwd:          t.TempDir(),
		Argv:         []string{"claude", "--session-id", "abc123", "--dangerously-skip-permissions"},
		StdinDevNull: false, // claude / paste-inject harness — must NOT redirect stdin
	}

	_, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	cmd := adapter.capturedCommand
	if strings.Contains(cmd, "/dev/null") {
		t.Errorf("AC3 FAIL: claude command %q contains '/dev/null'\n"+
			"The hk-rpr6 fix must NOT affect the claude paste-inject path (StdinDevNull=false).",
			cmd)
	}
}
