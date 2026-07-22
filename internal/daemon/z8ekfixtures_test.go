package daemon

// z8ekfixtures_test.go — the three hk-z8ek remote-run fixtures that could not
// leave with claudelaunchspec_remote_hkz8ek_test.go.
//
// P2 unit E1b moved that file to
// internal/harness/claude/launchspec_remote_test.go. Three daemon tests that
// STAY consume these helpers and exercise the codex/pi remote-dispatch paths,
// not the claude launch builder:
//
//   - conformance_m4c7_test.go            (z8ekRunID, newNoOpRecorderZ8ek)
//   - harnessregistry_remote_hkr36v_test.go (all three)
//   - harnessregistry_pi_remote_runner_m4c4_test.go (z8ekRunID, newNoOpRecorderZ8ek)
//
// A Go test helper is not visible across a package boundary, so the ~25 lines
// are duplicated rather than shared: inventing a cross-package test-fixture
// package to save them would be exactly the new seam the extraction plan
// forbids (plans/2026-07-21-p2-extraction/E1b-claude.md §1, R4).
//
// Kept byte-identical to the copies that moved. If one changes, change both.

import (
	"context"
	"encoding/base64"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// newNoOpRecorderZ8ek returns a RecordingRunner that succeeds for every call
// without side effects (exec.Command("true")).
func newNoOpRecorderZ8ek() *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.Command("true")
		},
	}
}

func z8ekRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("mint run id: %v", err)
	}
	return core.RunID(u)
}

// decodeBase64FromScript extracts and decodes the base64 payload from a
// `... printf %s '<b64>' | base64 -d > '<path>'` remote-write script.
func decodeBase64FromScript(t *testing.T, script string) string {
	t.Helper()
	const pfx = "printf %s '"
	i := strings.Index(script, pfx)
	if i < 0 {
		t.Fatalf("no printf in script: %q", script)
	}
	rest := script[i+len(pfx):]
	j := strings.Index(rest, "'")
	if j < 0 {
		t.Fatalf("unterminated base64 in script: %q", script)
	}
	raw, err := base64.StdEncoding.DecodeString(rest[:j])
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	return string(raw)
}
