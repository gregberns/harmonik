//go:build scenario

// Package scenario is the scenario test harness for harmonik.
//
// # Design decisions
//
// In-process daemon boot: each scenario calls daemon.Start in a goroutine with a
// cancellable context, following the same pattern as internal/daemon/smoke_test.go.
// Rationale: subprocess boot would require building the harmonik binary per test run
// and coordinating stdout parsing; in-process keeps the test fast and observable.
//
// Twin selection: scenarios that need a twin binary call harnessBuildTwin which
// compiles cmd/harmonik-twin-claude once per test binary run (stored in a
// package-level path by TestMain). The twin is invoked via daemon.Config.HandlerBinary
// + HandlerArgs, matching the production dispatch path (CHB-022 twin-blind routing).
//
// Per-scenario isolation: each test creates its own t.TempDir() for project dir and
// JSONL log. No shared state between tests. Tests run in parallel.
//
// Bead ledger: tests use stubScenarioLedger (an in-memory implementation of the
// beadLedger interface) rather than a real br binary. This avoids a br dependency
// and keeps the scenario harness self-contained. The smoke test in
// internal/daemon/smoke_test.go covers the real-br integration path.
//
// Helper prefix: scenarioFixture (bead hk-mg1ya).
//
// Spec ref: specs/scenario-harness.md §4 (fixture lifecycle, twin substitution, event
// capture); specs/handler-contract.md §4.8 HC-036 (twin-parity).
// Bead ref: hk-mg1ya.
package scenario

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// twinBinaryPath is set by TestMain after the twin binary is built once.
// The zero value means the twin was not found / built; scenarios that require
// it call t.Skip if this is empty.
var twinBinaryPath string

// TestMain builds the harmonik-twin-claude binary once per test binary run and
// stores its path in twinBinaryPath. Tests that require the twin check this
// field before proceeding.
func TestMain(m *testing.M) {
	bin, err := scenarioFixtureBuildTwin()
	if err == nil {
		twinBinaryPath = bin
	}
	os.Exit(m.Run())
}

// scenarioFixtureBuildTwin builds cmd/harmonik-twin-claude into a temp directory
// and returns its absolute path.
//
// On failure, a descriptive error is returned. Callers (test functions) use
// twinBinaryPath and skip if it is empty — build failures are not fatal to
// tests that don't require the twin.
func scenarioFixtureBuildTwin() (string, error) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		return "", err
	}

	// Derive the module root from go env GOMOD.
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	goModCmd := exec.Command(goTool, "env", "GOMOD") //nolint:gosec // G204: goTool from LookPath
	goModCmd.Dir = cwd
	goModOut, err := goModCmd.Output()
	if err != nil {
		return "", err
	}
	moduleRoot := filepath.Dir(strings.TrimSpace(string(goModOut)))

	outDir, err := os.MkdirTemp("", "scenario-twin-")
	if err != nil {
		return "", err
	}
	binPath := filepath.Join(outDir, "harmonik-twin-claude")

	buildCmd := exec.Command( //nolint:gosec // G204: goTool from LookPath
		goTool, "build", "-o", binPath,
		"github.com/gregberns/harmonik/cmd/harmonik-twin-claude",
	)
	buildCmd.Dir = moduleRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		_ = os.RemoveAll(outDir)
		return "", &buildError{out: string(out), err: buildErr}
	}
	return binPath, nil
}

// buildError wraps a go-build failure with its combined output.
type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string {
	return "go build failed: " + e.err.Error() + "\n" + e.out
}

// ─────────────────────────────────────────────────────────────────────────────
// scenarioFixtureProjectDir — per-test temp project directory
// ─────────────────────────────────────────────────────────────────────────────

// scenarioFixtureProjectResult holds paths for a single scenario's project tree.
type scenarioFixtureProjectResult struct {
	projectDir string
	jsonlPath  string
	sockPath   string
}

// scenarioFixtureProjectDir creates a minimal harmonik project directory:
//   - .harmonik/events/    (for events.jsonl)
//   - .harmonik/beads-intents/  (intent-log protocol)
//
// Returns paths for the project dir, JSONL log, and daemon socket. The socket
// path is short enough to satisfy macOS sun_path ≤ 104 bytes; if t.TempDir()
// would exceed the limit, a shorter path under /tmp is used.
//
// All created paths are under a directory registered for t.Cleanup removal.
func scenarioFixtureProjectDir(t *testing.T) scenarioFixtureProjectResult {
	t.Helper()

	const sunPathMax = 104
	const harmonikRelSock = "/.harmonik/daemon.sock"

	candidate := t.TempDir()
	var projectDir string
	if len(candidate)+len(harmonikRelSock) <= sunPathMax {
		projectDir = candidate
	} else {
		dir, err := os.MkdirTemp("/tmp", "sc-")
		if err != nil {
			t.Fatalf("scenarioFixtureProjectDir: MkdirTemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		projectDir = dir
	}

	for _, sub := range []string{
		".harmonik/events",
		".harmonik/beads-intents",
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("scenarioFixtureProjectDir: MkdirAll %s: %v", sub, err)
		}
	}

	return scenarioFixtureProjectResult{
		projectDir: projectDir,
		jsonlPath:  filepath.Join(projectDir, ".harmonik", "events", "events.jsonl"),
		sockPath:   filepath.Join(projectDir, ".harmonik", "daemon.sock"),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// JSONL event capture and assertion helpers
// ─────────────────────────────────────────────────────────────────────────────

// scenarioFixtureReadJSONLLines reads all non-empty JSONL lines from path.
// Returns nil (not an error) when the file does not yet exist.
func scenarioFixtureReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("scenarioFixtureReadJSONLLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// scenarioFixturePollJSONLForEvent polls the JSONL log at path at 10 ms
// intervals for up to budget. Returns true if any line contains all of the
// provided needles.
func scenarioFixturePollJSONLForEvent(t *testing.T, path string, needles []string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		lines := scenarioFixtureReadJSONLLines(t, path)
		for _, line := range lines {
			if scenarioFixtureLineContainsAll(line, needles) {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// scenarioFixtureLineContainsAll reports whether line contains every needle.
func scenarioFixtureLineContainsAll(line string, needles []string) bool {
	for _, n := range needles {
		if !strings.Contains(line, n) {
			return false
		}
	}
	return true
}

// scenarioEventSequence asserts that the JSONL lines collected in jsonlLines
// contain eventTypes as a subsequence (each type appears in order; additional
// events between them are tolerated).
//
// The "type" field in each JSONL object is matched against eventTypes[i].
//
// Returns true if the full sequence is matched; callers use this for test
// assertions.
func scenarioEventSequence(t *testing.T, jsonlLines []string, eventTypes []string) bool {
	t.Helper()

	wi := 0
	for _, line := range jsonlLines {
		if wi >= len(eventTypes) {
			break
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		var et string
		if raw, ok := obj["event_type"]; ok {
			_ = json.Unmarshal(raw, &et)
		}
		if et == "" {
			// Progress-stream NDJSON uses "type" (not "event_type").
			if raw, ok := obj["type"]; ok {
				_ = json.Unmarshal(raw, &et)
			}
		}
		if et == eventTypes[wi] {
			wi++
		}
	}
	return wi >= len(eventTypes)
}

// ─────────────────────────────────────────────────────────────────────────────
// daemon.Start wrapper: boots the daemon in a goroutine, returns cancel + done
// ─────────────────────────────────────────────────────────────────────────────

// scenarioFixtureStartDaemon launches daemon.Start in a goroutine with the
// supplied Config. Returns a cancel function and a channel that receives the
// Start error when the daemon exits.
//
// Callers MUST call cancel() (or defer it) to shut the daemon down cleanly.
func scenarioFixtureStartDaemon(t *testing.T, cfg daemon.Config) (cancel func(), done <-chan error) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(t.Context())
	ch := make(chan error, 1)
	go func() {
		ch <- daemon.Start(ctx, cfg)
	}()
	return cancelFn, ch
}

// scenarioFixtureWaitDaemon waits for the daemon goroutine to exit within
// budget after cancel() has been called. Fails the test if the deadline is
// exceeded or the daemon returned a non-nil error.
func scenarioFixtureWaitDaemon(t *testing.T, done <-chan error, budget time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("daemon.Start returned non-nil after context cancel: %v", err)
		}
	case <-time.After(budget):
		t.Error("daemon.Start did not exit within budget after context cancel")
	}
}

// scenarioFixturePollSocket polls for a Unix-domain socket file at sockPath at
// 5 ms intervals for up to budget. Returns true if the socket is found with
// mode 0600.
func scenarioFixturePollSocket(sockPath string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		info, err := os.Stat(sockPath)
		if err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0o600 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// stubScenarioLedger — in-memory bead ledger for scenario tests
// ─────────────────────────────────────────────────────────────────────────────

// These are re-declared here so the scenario package is self-contained and does
// not import internal/daemon's test-only stubs.

// The stubScenarioLedger and stubScenarioCollector below are used by scenarios
// that exercise the work loop via ExportedWorkLoopDeps / ExportedRunWorkLoop
// rather than daemon.Start. Scenarios that test the full Start path use the
// real daemon.Config with BrPath="" (skips the work loop).

// scenarioCheckRunCompleted polls the JSONL log for a run_completed or run_failed
// event within budget.
func scenarioCheckRunCompleted(t *testing.T, jsonlPath string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		lines := scenarioFixtureReadJSONLLines(t, jsonlPath)
		for _, line := range lines {
			if strings.Contains(line, string(core.EventTypeRunCompleted)) ||
				strings.Contains(line, string(core.EventTypeRunFailed)) {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
