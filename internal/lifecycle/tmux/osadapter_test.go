package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// Fake tmux binary helpers (osAdapter prefix per bead hk-gql20.7)
// ──────────────────────────────────────────────────────────────────────────────

// osAdapterFixtureWriteFakeTmux writes a shell script at binDir/tmux that,
// when invoked, prints outputLines to stdout and exits with exitCode. The
// caller must prepend binDir to PATH before running tests.
//
// The script is used to test OSAdapter methods without a real tmux server.
func osAdapterFixtureWriteFakeTmux(t *testing.T, binDir string, outputLines []string, exitCode int) {
	t.Helper()

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	for _, line := range outputLines {
		fmt.Fprintf(&sb, "printf '%%s\\n' %q\n", line)
	}
	fmt.Fprintf(&sb, "exit %d\n", exitCode)

	scriptPath := filepath.Join(binDir, "tmux")
	//nolint:gosec // G306: executable test script, 0755 is intentional
	if err := os.WriteFile(scriptPath, []byte(sb.String()), 0o755); err != nil {
		t.Fatalf("osAdapterFixtureWriteFakeTmux: WriteFile: %v", err)
	}
}

// osAdapterFixtureBinDir creates a temp directory for the fake tmux binary and
// returns its path. The caller owns prepending this to PATH.
func osAdapterFixtureBinDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// osAdapterFixtureWithFakeTmux returns a context and a PATH that prepends
// binDir, so exec.CommandContext calls in OSAdapter find the fake tmux.
func osAdapterFixtureWithFakeTmux(t *testing.T, binDir string) string {
	t.Helper()
	origPath := os.Getenv("PATH")
	newPath := binDir + string(os.PathListSeparator) + origPath
	t.Setenv("PATH", newPath)
	return newPath
}

// ──────────────────────────────────────────────────────────────────────────────
// parseTmuxMajorVersion unit tests
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_ParseTmuxMajorVersion exercises the version-string parser used
// by ProbeTmux. Happy path and edge cases from real tmux output formats.
func TestOSAdapter_ParseTmuxMajorVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"tmux 3.4", 3, false},
		{"tmux 3.4a", 3, false},
		{"tmux 2.9", 2, false},
		{"tmux 3.0", 3, false},
		{"tmux 3.3c", 3, false},
		{"tmux 4.0", 4, false},
		{"", 0, true},
		{"bad", 0, true},
		{"tmux", 0, true},
		{"tmux notanumber", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			got, err := parseTmuxMajorVersion(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseTmuxMajorVersion(%q): want error, got nil (major=%d)", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("parseTmuxMajorVersion(%q): unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("parseTmuxMajorVersion(%q): got %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// buildNewWindowArgs unit tests
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_BuildNewWindowArgs verifies the tmux new-window argument
// construction for the common cases: env injection, cwd, and command.
func TestOSAdapter_BuildNewWindowArgs(t *testing.T) {
	t.Parallel()

	t.Run("env-and-cwd-and-command", func(t *testing.T) {
		t.Parallel()

		params := NewWindowIn{
			Session:    "my-session",
			WindowName: "hk-abc123-task",
			Env:        []string{"HARMONIK_PHASE=single", "HARMONIK_BEAD_ID=hk-abc123"},
			WorkDir:    "/srv/project",
			Command:    "claude",
		}
		args := buildNewWindowArgs(params)

		// Verify required structural flags.
		if !sliceContains(args, "-d") {
			t.Error("buildNewWindowArgs: missing -d (detached) flag")
		}
		if !sliceContainsPair(args, "-t", "my-session:") {
			t.Errorf("buildNewWindowArgs: missing -t my-session:, got %v", args)
		}
		if !sliceContainsPair(args, "-n", "hk-abc123-task") {
			t.Errorf("buildNewWindowArgs: missing -n hk-abc123-task, got %v", args)
		}
		if !sliceContainsPair(args, "-c", "/srv/project") {
			t.Errorf("buildNewWindowArgs: missing -c /srv/project, got %v", args)
		}
		if !sliceContainsPair(args, "-e", "HARMONIK_PHASE=single") {
			t.Errorf("buildNewWindowArgs: missing -e HARMONIK_PHASE=single, got %v", args)
		}
		if !sliceContainsPair(args, "-e", "HARMONIK_BEAD_ID=hk-abc123") {
			t.Errorf("buildNewWindowArgs: missing -e HARMONIK_BEAD_ID=hk-abc123, got %v", args)
		}
		if !sliceContains(args, "--") {
			t.Error("buildNewWindowArgs: missing -- separator before command")
		}
		if !sliceContains(args, "claude") {
			t.Error("buildNewWindowArgs: missing command 'claude'")
		}
	})

	t.Run("no-cwd-no-command", func(t *testing.T) {
		t.Parallel()

		params := NewWindowIn{
			Session:    "sess",
			WindowName: "win",
		}
		args := buildNewWindowArgs(params)

		// -c and -- should be absent when WorkDir and Command are empty.
		if sliceContains(args, "-c") {
			t.Error("buildNewWindowArgs: unexpected -c when WorkDir is empty")
		}
		if sliceContains(args, "--") {
			t.Error("buildNewWindowArgs: unexpected -- when Command is empty")
		}
	})

	t.Run("multiple-env-vars", func(t *testing.T) {
		t.Parallel()

		params := NewWindowIn{
			Session:    "s",
			WindowName: "w",
			Env:        []string{"A=1", "B=2", "C=3"},
		}
		args := buildNewWindowArgs(params)

		// Each env var must be preceded by -e.
		for _, kv := range params.Env {
			if !sliceContainsPair(args, "-e", kv) {
				t.Errorf("buildNewWindowArgs: missing -e %s, got %v", kv, args)
			}
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// OSAdapter.ProbeTmux tests (fake binary on PATH)
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_ProbeTmux_MissingBinary verifies that ProbeTmux returns
// ErrTmuxMissing when tmux is not on PATH.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_ProbeTmux_MissingBinary(t *testing.T) {
	// Override PATH with a directory that contains no tmux binary.
	emptyBinDir := osAdapterFixtureBinDir(t)
	t.Setenv("PATH", emptyBinDir)

	a := OSAdapter{}
	err := a.ProbeTmux(context.Background())
	if !errors.Is(err, ErrTmuxMissing) {
		t.Errorf("ProbeTmux missing binary: want ErrTmuxMissing, got %v", err)
	}
}

// TestOSAdapter_ProbeTmux_VersionTooOld verifies that ProbeTmux returns an
// error (not ErrTmuxMissing) when tmux is present but reports major version < 3.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_ProbeTmux_VersionTooOld(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"tmux 2.9"}, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	err := a.ProbeTmux(context.Background())
	if err == nil {
		t.Fatal("ProbeTmux old version: want error, got nil")
	}
	if errors.Is(err, ErrTmuxMissing) {
		t.Errorf("ProbeTmux old version: got ErrTmuxMissing, want version-too-old error")
	}
	if !strings.Contains(err.Error(), "below required 3.0") {
		t.Errorf("ProbeTmux old version: error %q does not mention 'below required 3.0'", err.Error())
	}
}

// TestOSAdapter_ProbeTmux_HappyPath verifies that ProbeTmux returns nil when
// the fake tmux reports version 3.4 (major ≥ 3).
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_ProbeTmux_HappyPath(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"tmux 3.4"}, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	if err := a.ProbeTmux(context.Background()); err != nil {
		t.Errorf("ProbeTmux happy path: unexpected error: %v", err)
	}
}

// TestOSAdapter_ProbeTmux_NonZeroExit verifies that ProbeTmux returns
// *ErrTmuxFailure when tmux -V exits non-zero.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_ProbeTmux_NonZeroExit(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"error output"}, 1)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	err := a.ProbeTmux(context.Background())
	if err == nil {
		t.Fatal("ProbeTmux non-zero exit: want error, got nil")
	}
	var tf *ErrTmuxFailure
	if !errors.As(err, &tf) {
		t.Errorf("ProbeTmux non-zero exit: want *ErrTmuxFailure, got %T: %v", err, err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// OSAdapter.ListSessions tests
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_ListSessions_HappyPath verifies that ListSessions returns
// session names when fake tmux prints them.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_ListSessions_HappyPath(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"harmonik-abc123-default", "my-other-session"}, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions happy path: unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions happy path: got %d sessions, want 2: %v", len(sessions), sessions)
	}
	if sessions[0] != "harmonik-abc123-default" {
		t.Errorf("ListSessions happy path: sessions[0] = %q, want %q", sessions[0], "harmonik-abc123-default")
	}
}

// TestOSAdapter_ListSessions_NoServer verifies that ListSessions returns
// (nil, nil) when tmux exits non-zero (no server / no sessions).
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_ListSessions_NoServer(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"no server running"}, 1)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Errorf("ListSessions no-server: want nil error, got %v", err)
	}
	if sessions != nil {
		t.Errorf("ListSessions no-server: want nil sessions, got %v", sessions)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// OSAdapter.ListWindows tests
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_ListWindows_HappyPath verifies that ListWindows returns window
// names when the fake tmux emits them.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_ListWindows_HappyPath(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"hk-abc123-my-task", "some-other-window"}, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	windows, err := a.ListWindows(context.Background(), "harmonik-abc123-default")
	if err != nil {
		t.Fatalf("ListWindows happy path: unexpected error: %v", err)
	}
	if len(windows) != 2 {
		t.Fatalf("ListWindows happy path: got %d windows, want 2: %v", len(windows), windows)
	}
}

// TestOSAdapter_ListWindows_NoSession verifies that ListWindows returns
// ErrNoSession when tmux reports the session does not exist.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_ListWindows_NoSession(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"can't find session: missing-session"}, 1)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	_, err := a.ListWindows(context.Background(), "missing-session")
	if !errors.Is(err, ErrNoSession) {
		t.Errorf("ListWindows no-session: want ErrNoSession, got %v", err)
	}
}

// TestOSAdapter_ListWindows_TmuxFailure verifies that ListWindows returns
// *ErrTmuxFailure for unexpected tmux errors.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_ListWindows_TmuxFailure(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"unexpected internal error"}, 2)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	_, err := a.ListWindows(context.Background(), "some-session")
	var tf *ErrTmuxFailure
	if !errors.As(err, &tf) {
		t.Errorf("ListWindows tmux-failure: want *ErrTmuxFailure, got %T: %v", err, err)
	}
	if tf != nil && tf.Op != "list-windows" {
		t.Errorf("ListWindows tmux-failure: tf.Op = %q, want %q", tf.Op, "list-windows")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// OSAdapter.NewWindowIn tests
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_NewWindowIn_HappyPath verifies that NewWindowIn returns an
// Outcome with a non-empty Handle and nil Err on fake tmux success.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_NewWindowIn_HappyPath(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, nil, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	params := NewWindowIn{
		Session:    "harmonik-abc123-default",
		WindowName: "hk-abc123-my-task",
		Env:        []string{"HARMONIK_PHASE=single"},
		WorkDir:    "/tmp/project",
		Command:    "claude",
	}
	outcome := a.NewWindowIn(context.Background(), params)
	if outcome.Err != nil {
		t.Fatalf("NewWindowIn happy path: unexpected Err: %v", outcome.Err)
	}
	expectedHandle := WindowHandle("harmonik-abc123-default:hk-abc123-my-task")
	if outcome.Handle != expectedHandle {
		t.Errorf("NewWindowIn happy path: Handle = %q, want %q", outcome.Handle, expectedHandle)
	}
}

// TestOSAdapter_NewWindowIn_NoSession verifies that NewWindowIn returns
// ErrNoSession when the target session does not exist.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_NewWindowIn_NoSession(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"can't find session: no-such-session"}, 1)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	outcome := a.NewWindowIn(context.Background(), NewWindowIn{
		Session:    "no-such-session",
		WindowName: "my-window",
	})
	if !errors.Is(outcome.Err, ErrNoSession) {
		t.Errorf("NewWindowIn no-session: want ErrNoSession, got %v", outcome.Err)
	}
}

// TestOSAdapter_NewWindowIn_WindowCollision verifies that NewWindowIn returns
// ErrWindowCollision when a window with the same name already exists.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_NewWindowIn_WindowCollision(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"duplicate window name: my-window"}, 1)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	outcome := a.NewWindowIn(context.Background(), NewWindowIn{
		Session:    "my-session",
		WindowName: "my-window",
	})
	if !errors.Is(outcome.Err, ErrWindowCollision) {
		t.Errorf("NewWindowIn collision: want ErrWindowCollision, got %v", outcome.Err)
	}
}

// TestOSAdapter_NewWindowIn_TmuxFailure verifies that NewWindowIn returns
// *ErrTmuxFailure for generic tmux errors.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_NewWindowIn_TmuxFailure(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"something went wrong"}, 3)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	outcome := a.NewWindowIn(context.Background(), NewWindowIn{
		Session:    "my-session",
		WindowName: "my-window",
	})
	var tf *ErrTmuxFailure
	if !errors.As(outcome.Err, &tf) {
		t.Errorf("NewWindowIn tmux-failure: want *ErrTmuxFailure, got %T: %v", outcome.Err, outcome.Err)
	}
	if tf != nil && tf.Op != "new-window" {
		t.Errorf("NewWindowIn tmux-failure: tf.Op = %q, want %q", tf.Op, "new-window")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// OSAdapter.KillWindow tests
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_KillWindow_HappyPath verifies that KillWindow returns nil on
// fake tmux success.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_KillWindow_HappyPath(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, nil, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	if err := a.KillWindow(context.Background(), "my-session:my-window"); err != nil {
		t.Errorf("KillWindow happy path: unexpected error: %v", err)
	}
}

// TestOSAdapter_KillWindow_Idempotent verifies that KillWindow returns nil
// when tmux reports the window is already gone ("no window").
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_KillWindow_Idempotent(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"no window: my-window"}, 1)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	if err := a.KillWindow(context.Background(), "my-session:my-window"); err != nil {
		t.Errorf("KillWindow idempotent: want nil, got %v", err)
	}
}

// TestOSAdapter_KillWindow_NoSession verifies that KillWindow returns
// ErrNoSession when tmux reports the session is gone.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_KillWindow_NoSession(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"can't find session: gone-session"}, 1)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	err := a.KillWindow(context.Background(), "gone-session:my-window")
	if !errors.Is(err, ErrNoSession) {
		t.Errorf("KillWindow no-session: want ErrNoSession, got %v", err)
	}
}

// TestOSAdapter_KillWindow_TmuxFailure verifies that KillWindow returns
// *ErrTmuxFailure for unexpected errors.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_KillWindow_TmuxFailure(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"unexpected tmux error"}, 5)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	err := a.KillWindow(context.Background(), "my-session:my-window")
	var tf *ErrTmuxFailure
	if !errors.As(err, &tf) {
		t.Errorf("KillWindow tmux-failure: want *ErrTmuxFailure, got %T: %v", err, err)
	}
	if tf != nil && tf.Op != "kill-window" {
		t.Errorf("KillWindow tmux-failure: tf.Op = %q, want %q", tf.Op, "kill-window")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// OSAdapter.WindowPanePID tests
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_WindowPanePID_HappyPath verifies that WindowPanePID returns
// the PID reported by the fake tmux.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_WindowPanePID_HappyPath(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"12345"}, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	pid, err := a.WindowPanePID(context.Background(), "my-session:my-window")
	if err != nil {
		t.Fatalf("WindowPanePID happy path: unexpected error: %v", err)
	}
	if pid != 12345 {
		t.Errorf("WindowPanePID happy path: got %d, want 12345", pid)
	}
}

// TestOSAdapter_WindowPanePID_NoSession verifies that WindowPanePID returns
// ErrNoSession when the session is gone.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_WindowPanePID_NoSession(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"no server running"}, 1)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	_, err := a.WindowPanePID(context.Background(), "gone-session:my-window")
	if !errors.Is(err, ErrNoSession) {
		t.Errorf("WindowPanePID no-session: want ErrNoSession, got %v", err)
	}
}

// TestOSAdapter_WindowPanePID_TmuxFailure verifies that WindowPanePID returns
// *ErrTmuxFailure when display-message exits non-zero with an unrecognized message.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_WindowPanePID_TmuxFailure(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"some tmux error"}, 2)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	_, err := a.WindowPanePID(context.Background(), "my-session:my-window")
	var tf *ErrTmuxFailure
	if !errors.As(err, &tf) {
		t.Errorf("WindowPanePID tmux-failure: want *ErrTmuxFailure, got %T: %v", err, err)
	}
	if tf != nil && tf.Op != "display-message" {
		t.Errorf("WindowPanePID tmux-failure: tf.Op = %q, want %q", tf.Op, "display-message")
	}
}

// TestOSAdapter_WindowPanePID_BadOutput verifies that WindowPanePID returns an
// error when the PID output is non-numeric.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_WindowPanePID_BadOutput(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"notanumber"}, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{}
	_, err := a.WindowPanePID(context.Background(), "my-session:my-window")
	if err == nil {
		t.Error("WindowPanePID bad output: want error for non-numeric PID, got nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// ErrTmuxFailure.Error tests
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_ErrTmuxFailureError verifies the Error() message format of
// *ErrTmuxFailure, which must carry Op, ExitCode, and Stderr.
func TestOSAdapter_ErrTmuxFailureError(t *testing.T) {
	t.Parallel()

	e := &ErrTmuxFailure{Op: "new-window", ExitCode: 1, Stderr: "some error output"}
	msg := e.Error()
	if !strings.Contains(msg, "new-window") {
		t.Errorf("ErrTmuxFailure.Error: missing op name in %q", msg)
	}
	if !strings.Contains(msg, "1") {
		t.Errorf("ErrTmuxFailure.Error: missing exit code in %q", msg)
	}
	if !strings.Contains(msg, "some error output") {
		t.Errorf("ErrTmuxFailure.Error: missing stderr in %q", msg)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Interface compliance
// ──────────────────────────────────────────────────────────────────────────────

// TestOSAdapter_ImplementsAdapter is a compile-time check that OSAdapter satisfies
// the Adapter interface. The variable is intentionally blank-assigned.
func TestOSAdapter_ImplementsAdapter(t *testing.T) {
	t.Parallel()

	var _ Adapter = OSAdapter{}
	// If OSAdapter does not implement Adapter, this file will not compile.
}

// ──────────────────────────────────────────────────────────────────────────────
// Helper utilities
// ──────────────────────────────────────────────────────────────────────────────

// sliceContains reports whether s is present anywhere in slice.
func sliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// sliceContainsPair reports whether flag is immediately followed by value in
// the slice (as in flag-value CLI argument pairs).
func sliceContainsPair(slice []string, flag, value string) bool {
	for i := 0; i+1 < len(slice); i++ {
		if slice[i] == flag && slice[i+1] == value {
			return true
		}
	}
	return false
}

// Ensure the test helpers compile when unused by individual test functions.
var _ = fmt.Sprintf
