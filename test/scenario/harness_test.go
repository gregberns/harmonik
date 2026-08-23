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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/testhelpers/hermetic"
)

var twinBinaryPath string

var codexTwinBinaryPath string

// TestMain builds the harmonik-twin-claude and harmonik-twin-codex binaries
// once per test binary run and stores their paths in twinBinaryPath /
// codexTwinBinaryPath. A twin build failure fails the whole tier. A skipped
// twin scenario would hide the failed build and turn a broken tier green.
//
// It also fences the tier off the host, through hermetic.Setup. The scenario
// fixtures run `git commit` and set a user identity but never turn signing off,
// so on a machine with commit.gpgsign=true in its global gitconfig — a common
// operator setting — every one of them fails.
//
// This tier is easy to miss and was: it is behind //go:build scenario, so a
// hostile-environment sweep with `go test ./...` never compiles it, while
// `make full` does run it. A verification that cannot see a tier is not evidence
// about that tier.
func TestMain(m *testing.M) {
	cleanup := hermetic.Setup()

	bin, err := scenarioFixtureBuildTwin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "scenario: build harmonik-twin-claude: %v\n", err)
		os.Exit(1)
	}
	twinBinaryPath = bin

	codexBin, codexErr := scenarioFixtureBuildCodexTwin()
	if codexErr != nil {
		fmt.Fprintf(os.Stderr, "scenario: build harmonik-twin-codex: %v\n", codexErr)
		os.Exit(1)
	}
	codexTwinBinaryPath = codexBin

	code := m.Run()
	cleanup()
	os.Exit(code)
}

func scenarioFixtureBuildTwin() (string, error) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		return "", err
	}

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

func scenarioFixtureBuildCodexTwin() (string, error) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		return "", err
	}

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

	outDir, err := os.MkdirTemp("", "scenario-codex-twin-")
	if err != nil {
		return "", err
	}
	binPath := filepath.Join(outDir, "harmonik-twin-codex")

	buildCmd := exec.Command( //nolint:gosec // G204: goTool from LookPath
		goTool, "build", "-o", binPath,
		"github.com/gregberns/harmonik/cmd/harmonik-twin-codex",
	)
	buildCmd.Dir = moduleRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		_ = os.RemoveAll(outDir)
		return "", &buildError{out: string(out), err: buildErr}
	}
	return binPath, nil
}

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string {
	return "go build failed: " + e.err.Error() + "\n" + e.out
}

type scenarioFixtureProjectResult struct {
	projectDir string
	jsonlPath  string
	sockPath   string
}

func scenarioFixtureProjectDir(t *testing.T) scenarioFixtureProjectResult {
	t.Helper()

	resolve := func(dir string) string {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatalf("scenarioFixtureProjectDir: EvalSymlinks %q: %v", dir, err)
		}
		return resolved
	}
	sockUnder := func(dir string) string {
		return filepath.Join(dir, ".harmonik", "daemon.sock")
	}

	projectDir := resolve(t.TempDir())
	if lifecycle.ValidateSocketPathLength(sockUnder(projectDir)) != nil {
		dir, err := os.MkdirTemp("/tmp", "sc-")
		if err != nil {
			t.Fatalf("scenarioFixtureProjectDir: MkdirTemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		projectDir = resolve(dir)
	}

	sockPath := sockUnder(projectDir)
	if err := lifecycle.ValidateSocketPathLength(sockPath); err != nil {
		t.Fatalf("scenarioFixtureProjectDir: no bindable socket path for this test: %v", err)
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
		sockPath:   sockPath,
	}
}

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

func scenarioFixtureLineContainsAll(line string, needles []string) bool {
	for _, n := range needles {
		if !strings.Contains(line, n) {
			return false
		}
	}
	return true
}

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

func scenarioFixtureStartDaemon(t *testing.T, cfg daemon.Config) (cancel func(), done <-chan error) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(t.Context())
	ch := make(chan error, 1)
	go func() {
		ch <- daemon.Start(ctx, cfg)
	}()
	return cancelFn, ch
}

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
