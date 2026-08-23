package supervisecmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func socketSafeTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hkt-*")
	if err != nil {
		t.Fatalf("socketSafeTempDir: %v", err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			t.Errorf("remove socket-safe temporary directory %q: %v", dir, removeErr)
		}
	})
	return dir
}

type fakeSocketResp struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func startFakeSocketServer(t *testing.T, dir string) {
	t.Helper()

	sockDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(sockDir, 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	sockPath := filepath.Join(sockDir, "daemon.sock")

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
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
				defer func() {
					if closeErr := c.Close(); closeErr != nil {
						t.Errorf("close fake daemon connection: %v", closeErr)
					}
				}()

				var req struct {
					Op string `json:"op"`
				}
				if err := json.NewDecoder(c).Decode(&req); err != nil {
					return
				}

				resp := fakeSocketResp{Ok: true}
				_ = req
				if err := json.NewEncoder(c).Encode(resp); err != nil {
					return
				}
			}(conn)
		}
	}()

	t.Cleanup(func() {
		if closeErr := ln.Close(); closeErr != nil {
			t.Errorf("close fake daemon listener: %v", closeErr)
		}
	})
}

// TestON056_AgentAndHumanSameCommandSurface_Pause confirms that RunPause uses
// no interactive / human-only gate: it dials the socket and returns exit 0
// with a single-line machine-readable confirmation.
//
// This is the agent-callable path described in ON-056: an agent may issue
// `harmonik supervise pause` over PL-003a without human intervention.
func TestON056_AgentAndHumanSameCommandSurface_Pause(t *testing.T) {
	t.Parallel()

	dir := socketSafeTempDir(t)
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
	if strings.Contains(out, "\x1b[") {
		t.Errorf("ON-056: RunPause stdout contains ANSI escape sequences; want plain text: %q", out)
	}
}

// TestON056_AgentAndHumanSameCommandSurface_Resume mirrors the pause test for
// the resume verb, confirming identical behaviour (ON-056 covers both).
func TestON056_AgentAndHumanSameCommandSurface_Resume(t *testing.T) {
	t.Parallel()

	dir := socketSafeTempDir(t)
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

// TestON056_DaemonNotRunning_Exit17_Pause confirms that RunPause returns exit
// 17 (not 1) when no daemon socket is present.  The same exit code must be
// produced regardless of whether the caller is a human or an agent so that
// automated retry logic can act on it.
func TestON056_DaemonNotRunning_Exit17_Pause(t *testing.T) {
	t.Parallel()

	dir := socketSafeTempDir(t)

	var stdout, stderr bytes.Buffer
	code := RunPause([]string{"--project", dir}, &stdout, &stderr)

	if code != 17 {
		t.Fatalf("ON-056: RunPause with no daemon: exit code = %d, want 17; stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("ON-056: RunPause (no daemon) must not write to stdout; got %q", stdout.String())
	}
}

// TestON056_DaemonNotRunning_Exit17_Resume mirrors the pause/no-daemon test
// for the resume verb.
func TestON056_DaemonNotRunning_Exit17_Resume(t *testing.T) {
	t.Parallel()

	dir := socketSafeTempDir(t)

	var stdout, stderr bytes.Buffer
	code := RunResume([]string{"--project", dir}, &stdout, &stderr)

	if code != 17 {
		t.Fatalf("ON-056: RunResume with no daemon: exit code = %d, want 17; stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("ON-056: RunResume (no daemon) must not write to stdout; got %q", stdout.String())
	}
}

// TestON056_NoPauseOrResumeInUsageText_NoInteractivePrompt confirms that the
// --help output for both verbs contains no interactive/human-only gate
// language ("press Enter", "confirm", "y/N", etc.) — documenting the
// machine-callable contract per ON-056.
func TestON056_NoPauseOrResumeInUsageText_NoInteractivePrompt(t *testing.T) {
	t.Parallel()

	interactiveMarkers := []string{"press Enter", "Press Enter", "y/N", "Y/n", "[y/n]", "confirm", "Confirm"}

	for _, tc := range []struct {
		verb string
		fn   func([]string, io.Writer, io.Writer) int
	}{
		{"pause", RunPause},
		{"resume", RunResume},
	} {
		var buf bytes.Buffer
		if code := tc.fn([]string{"--help"}, &buf, &buf); code != 0 {
			t.Errorf("ON-056: %q --help exit code = %d, want 0", tc.verb, code)
		}
		usage := buf.String()
		for _, marker := range interactiveMarkers {
			if strings.Contains(usage, marker) {
				t.Errorf("ON-056: %q usage text contains interactive gate marker %q; command must be agent-callable without human intervention", tc.verb, marker)
			}
		}
	}
}
