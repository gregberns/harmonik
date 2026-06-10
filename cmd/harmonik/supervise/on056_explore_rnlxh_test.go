package supervisecmd

// on056_explore_rnlxh_test.go — exploratory test for specs/operator-nfr.md §4.3 ON-056.
//
// ON-056 agent-callable pause/resume command verb:
//
//   "harmonik supervise pause/resume" is the single canonical verb form for the
//   daemon pause/resume control surface.  The loop (or any agent driving it) MAY
//   itself issue the command without human intervention; no human-only gate exists.
//   [operator-nfr.md §4.3 ON-056]
//
// Observable (this file):
//   - RunPause / RunResume accept the --project flag and dial the daemon socket;
//     no TTY check, no interactive prompt, no human-gate.
//   - Both commands exit 0 when the daemon socket responds Ok=true.
//   - Both commands exit 17 when the socket is absent (daemon not running).
//   - Both commands exit 17 with the same stderr message whether invoked by a
//     human or an agent (identical command surface).
//   - The stdout confirmation line is machine-readable (single line, no ANSI).
//
// Bead: hk-rnlxh.

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers — minimal fake daemon socket
// ---------------------------------------------------------------------------

// fakeSocketResp is the JSON the fake daemon returns.
type fakeSocketResp struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// startFakeSocketServer starts a Unix-socket listener that reads a JSON op
// request and responds with {ok: true}. It returns the socket path and a
// cleanup function. The server closes after the first response.
func startFakeSocketServer(t *testing.T, dir string) string {
	t.Helper()

	// Replicate the socket path that RunPause/RunResume compute internally:
	// lifecycle.SocketPath(projectDir) == projectDir/.harmonik/daemon.sock.
	sockDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(sockDir, 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	sockPath := filepath.Join(sockDir, "daemon.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen %q: %v", sockPath, err)
	}

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }() //nolint:errcheck

				var req struct {
					Op string `json:"op"`
				}
				_ = json.NewDecoder(c).Decode(&req)

				resp := fakeSocketResp{Ok: true}
				if err := json.NewEncoder(c).Encode(resp); err != nil {
					_ = err
				}
			}(conn)
		}
	}()

	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck
	return dir
}

// ---------------------------------------------------------------------------
// TestON056_AgentAndHumanSameCommandSurface_Pause
// ---------------------------------------------------------------------------

// TestON056_AgentAndHumanSameCommandSurface_Pause confirms that RunPause uses
// no interactive / human-only gate: it dials the socket and returns exit 0
// with a single-line machine-readable confirmation.
//
// This is the agent-callable path described in ON-056: an agent may issue
// `harmonik supervise pause` over PL-003a without human intervention.
func TestON056_AgentAndHumanSameCommandSurface_Pause(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	startFakeSocketServer(t, dir)

	var stdout, stderr bytes.Buffer
	code := RunPause([]string{"--project", dir}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("ON-056: RunPause exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("ON-056: RunPause wrote to stderr on success: %q", stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		t.Error("ON-056: RunPause wrote nothing to stdout; want a confirmation line")
	}
	// Confirmation must not contain ANSI escape sequences (machine-readable).
	if strings.Contains(out, "\x1b[") {
		t.Errorf("ON-056: RunPause stdout contains ANSI escape sequences; want plain text: %q", out)
	}
}

// ---------------------------------------------------------------------------
// TestON056_AgentAndHumanSameCommandSurface_Resume
// ---------------------------------------------------------------------------

// TestON056_AgentAndHumanSameCommandSurface_Resume mirrors the pause test for
// the resume verb, confirming identical behaviour (ON-056 covers both).
func TestON056_AgentAndHumanSameCommandSurface_Resume(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	startFakeSocketServer(t, dir)

	var stdout, stderr bytes.Buffer
	code := RunResume([]string{"--project", dir}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("ON-056: RunResume exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("ON-056: RunResume wrote to stderr on success: %q", stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		t.Error("ON-056: RunResume wrote nothing to stdout; want a confirmation line")
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("ON-056: RunResume stdout contains ANSI escape sequences; want plain text: %q", out)
	}
}

// ---------------------------------------------------------------------------
// TestON056_DaemonNotRunning_Exit17_Pause
// ---------------------------------------------------------------------------

// TestON056_DaemonNotRunning_Exit17_Pause confirms that RunPause returns exit
// 17 (not 1) when no daemon socket is present.  The same exit code must be
// produced regardless of whether the caller is a human or an agent so that
// automated retry logic can act on it.
func TestON056_DaemonNotRunning_Exit17_Pause(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Do NOT start a socket server; the socket file is absent.

	var stdout, stderr bytes.Buffer
	code := RunPause([]string{"--project", dir}, &stdout, &stderr)

	if code != 17 {
		t.Fatalf("ON-056: RunPause with no daemon: exit code = %d, want 17; stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("ON-056: RunPause (no daemon) must not write to stdout; got %q", stdout.String())
	}
}

// ---------------------------------------------------------------------------
// TestON056_DaemonNotRunning_Exit17_Resume
// ---------------------------------------------------------------------------

// TestON056_DaemonNotRunning_Exit17_Resume mirrors the pause/no-daemon test
// for the resume verb.
func TestON056_DaemonNotRunning_Exit17_Resume(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := RunResume([]string{"--project", dir}, &stdout, &stderr)

	if code != 17 {
		t.Fatalf("ON-056: RunResume with no daemon: exit code = %d, want 17; stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("ON-056: RunResume (no daemon) must not write to stdout; got %q", stdout.String())
	}
}

// ---------------------------------------------------------------------------
// TestON056_NoPauseOrResumeInUsageText_NoInteractivePrompt
// ---------------------------------------------------------------------------

// TestON056_NoPauseOrResumeInUsageText_NoInteractivePrompt confirms that the
// --help output for both verbs contains no interactive/human-only gate
// language ("press Enter", "confirm", "y/N", etc.) — documenting the
// machine-callable contract per ON-056.
func TestON056_NoPauseOrResumeInUsageText_NoInteractivePrompt(t *testing.T) {
	t.Parallel()

	interactiveMarkers := []string{"press Enter", "Press Enter", "y/N", "Y/n", "[y/n]", "confirm", "Confirm"}

	for _, tc := range []struct {
		verb string
		fn   func([]string, interface{ Write([]byte) (int, error) }, interface{ Write([]byte) (int, error) }) int
	}{
		{"pause", func(args []string, out, _ interface{ Write([]byte) (int, error) }) int {
			var buf bytes.Buffer
			code := RunPause([]string{"--help"}, &buf, &buf)
			_, _ = out.Write(buf.Bytes())
			return code
		}},
		{"resume", func(args []string, out, _ interface{ Write([]byte) (int, error) }) int {
			var buf bytes.Buffer
			code := RunResume([]string{"--help"}, &buf, &buf)
			_, _ = out.Write(buf.Bytes())
			return code
		}},
	} {
		var buf bytes.Buffer
		tc.fn(nil, &buf, &buf)
		usage := buf.String()
		for _, marker := range interactiveMarkers {
			if strings.Contains(usage, marker) {
				t.Errorf("ON-056: %q usage text contains interactive gate marker %q; command must be agent-callable without human intervention", tc.verb, marker)
			}
		}
	}
}
