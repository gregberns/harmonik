package tmuxhost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installFakeTmux puts a "tmux" script on PATH that appends its argv (one
// call per line, args space-joined) to logPath and exits 0, WITHOUT a real
// tmux server. This is what lets TestHostArgv_ByteIdentical assert the exact
// subprocess argv the Host methods issue — the KH-1 extraction's central
// claim (byte-identical tmux invocations, pre- vs. post-move) — without a
// live tmux session.
func installFakeTmux(t *testing.T, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-tmux argv capture uses a POSIX shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"{\n" +
		"  first=1\n" +
		"  for a in \"$@\"; do\n" +
		"    if [ \"$first\" = 1 ]; then printf '%s' \"$a\"; first=0; else printf ' %s' \"$a\"; fi\n" +
		"  done\n" +
		"  printf '\\n'\n" +
		"} >> " + logPath + "\n" +
		"exit 0\n"
	path := filepath.Join(dir, "tmux")
	//nolint:gosec // G306: the file must be executable to stand in for the tmux binary on PATH; it lives under t.TempDir(), not shared state
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func readLoggedArgv(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("read fake-tmux log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// TestHostArgv_ByteIdentical pins the exact tmux argv each Host method
// issues. This is the KH-1 self-check: the tmux mechanics moved verbatim
// from internal/keeper into this package, and this test proves the move did
// not alter a single flag. A deliberate mutation of any one of these argv
// strings (see the sibling TestHostArgv_MutationIsCaught) must fail this
// test — that failure-on-purpose is what makes the assertion meaningful.
func TestHostArgv_ByteIdentical(t *testing.T) {
	origSettle, origRetry := SubmitSettle, SubmitRetryDelay
	SubmitSettle, SubmitRetryDelay = 0, 0
	t.Cleanup(func() { SubmitSettle, SubmitRetryDelay = origSettle, origRetry })

	h := New()
	ctx := context.Background()

	t.Run("Inject", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "argv.log")
		installFakeTmux(t, log)
		if err := h.Inject(ctx, "sess:agent", "hello"); err != nil {
			t.Fatalf("Inject: %v", err)
		}
		got := readLoggedArgv(t, log)
		want := []string{
			"load-buffer -b harmonik-keeper-inject -",
			"paste-buffer -b harmonik-keeper-inject -t sess:agent -d",
			"send-keys -t sess:agent Enter",
			"send-keys -t sess:agent Enter",
			"send-keys -t sess:agent Enter",
		}
		assertArgvEqual(t, got, want)
	})

	t.Run("Capture", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "argv.log")
		installFakeTmux(t, log)
		if _, err := h.Capture(ctx, "sess:agent"); err != nil {
			t.Fatalf("Capture: %v", err)
		}
		assertArgvEqual(t, readLoggedArgv(t, log), []string{
			"capture-pane -p -t sess:agent -S -200",
		})
	})

	t.Run("Resolve", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "argv.log")
		installFakeTmux(t, log)
		got := h.Resolve("/some/project", "captain", "")
		want := fmt.Sprintf("has-session -t =%s", HarmonikSessionName("/some/project", "captain"))
		assertArgvEqual(t, readLoggedArgv(t, log), []string{want})
		if got == "" {
			t.Fatalf("Resolve: want a non-empty target (fake tmux reports the session live), got %q", got)
		}
	})

	t.Run("OperatorAttached", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "argv.log")
		installFakeTmux(t, log)
		_ = h.OperatorAttached("sess:agent")
		assertArgvEqual(t, readLoggedArgv(t, log), []string{
			"list-clients -t sess:agent -F #{client_activity}",
		})
	})
}

func assertArgvEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("argv call count = %d, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
