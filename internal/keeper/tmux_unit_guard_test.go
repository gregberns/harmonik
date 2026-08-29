//go:build !integration

package keeper_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	guardDir, err := os.MkdirTemp("", "keeper-tmux-guard-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "keeper tmux guard: create temp dir: %v\n", err)
		os.Exit(1)
	}
	logPath := filepath.Join(guardDir, "calls")
	shimPath := filepath.Join(guardDir, "tmux")
	shim := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$KEEPER_TMUX_GUARD_LOG\"\nexit 127\n"
	//nolint:gosec // G306: the test-owned command shim must be executable.
	if err := os.WriteFile(shimPath, []byte(shim), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "keeper tmux guard: write shim: %v\n", err)
		os.Exit(1)
	}

	originalPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", guardDir+string(os.PathListSeparator)+originalPath); err != nil {
		fmt.Fprintf(os.Stderr, "keeper tmux guard: set PATH: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("KEEPER_TMUX_GUARD_LOG", logPath); err != nil {
		fmt.Fprintf(os.Stderr, "keeper tmux guard: set log path: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	//nolint:gosec // G304: logPath is derived from the test-owned temporary directory.
	calls, readErr := os.ReadFile(logPath)
	if readErr == nil && len(calls) > 0 {
		fmt.Fprintf(os.Stderr, "keeper unit tests reached real tmux command paths:\n%s", calls)
		code = 1
	} else if readErr != nil && !os.IsNotExist(readErr) {
		fmt.Fprintf(os.Stderr, "keeper tmux guard: read calls: %v\n", readErr)
		code = 1
	}
	if err := os.RemoveAll(guardDir); err != nil {
		fmt.Fprintf(os.Stderr, "keeper tmux guard: remove temp dir: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
