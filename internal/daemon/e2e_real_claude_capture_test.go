//go:build e2e_real_claude

package daemon_test

// e2e_real_claude_capture_test.go — WS3-Claude-A real-session capture harness.
//
// This test drives the SAME real-Claude happy path as the single-mode smoke
// (e2e_real_claude_single_test.go) and, on success, writes a twin-parity capture
// dir under HARMONIK_WIRE_CAPTURE_DIR:
//
//	<dir>/<scn>/wire.ndjson   — the raw tee'd NDJSON progress stream (via the
//	                            watcher's WireTap seam). NOTE: capturing wire.ndjson
//	                            requires the daemon to honor HARMONIK_WIRE_CAPTURE_DIR
//	                            and point SpawnWatcherConfig.WireTap at this file —
//	                            that daemon wiring is a SEPARATE follow-up (the seam
//	                            landed here; the daemon opt-in has not). Until then
//	                            this harness captures the DURABLE events.jsonl only
//	                            and leaves a wire.ndjson placeholder note.
//	<dir>/<scn>/events.jsonl  — the durable event log, copied on run_completed.
//	<dir>/<scn>/meta.yaml     — scn/agent/date/sha + the §5 carve-out excludes block.
//
// # Build tag / skip guards
//
// Shares the //go:build e2e_real_claude tag and rcsmFixtureCheckPreconditions
// skip guards (claude/tmux/git/br/ntm binaries + ANTHROPIC_API_KEY or
// CLAUDE_CODE_OAUTH_TOKEN). On a box with no auth this test SKIPS cleanly.
//
// # Credfence
//
// The Makefile target runs this under `env -u ANTHROPIC_API_KEY
// -u ANTHROPIC_AUTH_TOKEN` (subscription-billing path, codename:credfence,
// scripts/scratch-daemon.sh:237-240) — never an API key (D2).
//
// Bead: WS3-Claude-A (twin-parity real-Claude capture harness).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// captureClaudeEnvDir is the env var naming the output directory for captured
// twin-parity fixtures. When unset, the test still runs the real-Claude path
// but writes the capture to t.TempDir() (self-check only, not committed).
const captureClaudeEnvDir = "HARMONIK_WIRE_CAPTURE_DIR"

// TestCaptureClaudeFixtures runs the real-Claude happy path and, on success,
// writes a twin-parity capture dir. It SKIPS cleanly when Claude/auth/tmux are
// absent (this box) via the shared rcsmFixtureCheckPreconditions guard.
func TestCaptureClaudeFixtures(t *testing.T) {
	rcsmFixtureCheckPreconditions(t)

	scn := os.Getenv("HARMONIK_CAPTURE_SCN")
	if scn == "" {
		scn = "happy-path"
	}

	outRoot := os.Getenv(captureClaudeEnvDir)
	if outRoot == "" {
		outRoot = t.TempDir()
		t.Logf("capture: %s unset; writing self-check capture to %s (not committed)", captureClaudeEnvDir, outRoot)
	}
	captureDir := filepath.Join(outRoot, scn)
	if err := os.MkdirAll(captureDir, 0o755); err != nil { //nolint:gosec // G301: committed fixture dir must be world-readable
		t.Fatalf("capture: mkdir %s: %v", captureDir, err)
	}

	harmonikBin := rcsmFixtureBuildHarmonik(t)
	smokeDir, beadID := rcsmFixtureProject(t)
	t.Logf("capture: scn=%s smokeDir=%s beadID=%s captureDir=%s", scn, smokeDir, captureDir, harmonikBin)

	sessionName, tmuxCleanup := rcsmFixtureTmuxSession(t, "capture-"+beadID)
	defer tmuxCleanup()

	jsonlPath := filepath.Join(smokeDir, ".harmonik", "events", "events.jsonl")

	watchCtx, watchCancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer watchCancel()

	eventsCh := make(chan []rcsmEvent, 1)
	go func() {
		eventsCh <- rcsmFixtureTailEvents(watchCtx, t, jsonlPath)
	}()

	rawStop := rcsmFixtureLaunchHarmonik(t, harmonikBin, sessionName, smokeDir)
	var stopOnce sync.Once
	stopDaemon := func() { stopOnce.Do(rawStop) }
	defer stopDaemon()

	var events []rcsmEvent
	scenariotest.MustCompleteWithin(t, jsonlPath, "", tmux.OSAdapter{}, 210*time.Second, func() {
		events = <-eventsCh
	})
	stopDaemon()

	rcsmAssertRunCompleted(t, events)

	// ── Copy the durable events.jsonl into the capture dir ────────────────────
	if err := captureCopyFile(jsonlPath, filepath.Join(captureDir, "events.jsonl")); err != nil {
		t.Fatalf("capture: copy events.jsonl: %v", err)
	}

	// ── wire.ndjson: written by the daemon WireTap sink when wired; leave a
	// clear placeholder until the daemon honors HARMONIK_WIRE_CAPTURE_DIR. ─────
	wirePath := filepath.Join(captureDir, "wire.ndjson")
	if _, err := os.Stat(wirePath); os.IsNotExist(err) {
		note := "# wire.ndjson not produced: daemon WireTap opt-in for " +
			captureClaudeEnvDir + " is a follow-up. The seam exists\n" +
			"# (internal/handlercontract SpawnWatcherConfig.WireTap); the daemon must point it here.\n"
		if wErr := os.WriteFile(wirePath, []byte(note), 0o644); wErr != nil { //nolint:gosec // G306: committed fixture file must be world-readable
			t.Fatalf("capture: write wire.ndjson placeholder: %v", wErr)
		}
	}

	// ── meta.yaml ─────────────────────────────────────────────────────────────
	if err := os.WriteFile(filepath.Join(captureDir, "meta.yaml"), captureMetaYAML(scn), 0o644); err != nil { //nolint:gosec // G306: committed fixture file must be world-readable
		t.Fatalf("capture: write meta.yaml: %v", err)
	}

	t.Logf("capture: wrote twin-parity fixture dir %s (%d events)", captureDir, len(events))
}

// captureCopyFile copies src to dst (small fixture files).
func captureCopyFile(src, dst string) error {
	//nolint:gosec // G304: src is the daemon-written events.jsonl under t.TempDir(); not user input
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644) //nolint:gosec // G306: committed fixture file must be world-readable
}

// captureMetaYAML renders the meta.yaml for a real capture. hand_authored:false
// distinguishes it from the committed happy-path-sample.
func captureMetaYAML(scn string) []byte {
	sha := os.Getenv("HARMONIK_CAPTURE_COMMIT_SHA")
	if sha == "" {
		sha = "UNKNOWN"
	}
	return []byte(fmt.Sprintf(`# Real Claude capture (produced by make capture-claude-fixtures).
scn: %s
agent: claude
hand_authored: false
capture_date: %q
commit_sha: %q

# excludes: wire-layer equivalence carve-outs per
# docs/twin-parity-audit-2026-05-14.md §5 (Real-Claude Conformance Carve-Outs).
excludes:
  - fix: Fix5
    bead: hk-yngq2
    name: pane %%NNNN stable send-keys target
    reason: tmux topology; no NDJSON message can observe it.
  - fix: Fix8
    bead: hk-rf4ux
    name: splash dismiss (SendEnterToLastPane + 750ms splashDismissDelay)
    reason: requires a real tmux pane + physical Enter delivery; wire twin has no terminal.
`, scn, time.Now().UTC().Format(time.RFC3339), sha))
}
