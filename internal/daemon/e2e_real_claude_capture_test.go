//go:build e2e_real_claude

package daemon_test

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

	if err := captureCopyFile(jsonlPath, filepath.Join(captureDir, "events.jsonl")); err != nil {
		t.Fatalf("capture: copy events.jsonl: %v", err)
	}

	wirePath := filepath.Join(captureDir, "wire.ndjson")
	if _, err := os.Stat(wirePath); os.IsNotExist(err) {
		note := "# wire.ndjson not produced: daemon WireTap opt-in for " +
			captureClaudeEnvDir + " is a follow-up. The seam exists\n" +
			"# (internal/handlercontract SpawnWatcherConfig.WireTap); the daemon must point it here.\n"
		if wErr := os.WriteFile(wirePath, []byte(note), 0o644); wErr != nil { //nolint:gosec // G306: committed fixture file must be world-readable
			t.Fatalf("capture: write wire.ndjson placeholder: %v", wErr)
		}
	}

	if err := os.WriteFile(filepath.Join(captureDir, "meta.yaml"), captureMetaYAML(scn), 0o644); err != nil { //nolint:gosec // G306: committed fixture file must be world-readable
		t.Fatalf("capture: write meta.yaml: %v", err)
	}

	t.Logf("capture: wrote twin-parity fixture dir %s (%d events)", captureDir, len(events))
}

func captureCopyFile(src, dst string) error {
	//nolint:gosec // G304: src is the daemon-written events.jsonl under t.TempDir(); not user input
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644) //nolint:gosec // G306: committed fixture file must be world-readable
}

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
