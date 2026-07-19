package daemon_test

// runner_seam_hkhd2w6_test.go — static audit + contract tests for the runner-seam
// (hk-hd2w6). Verifies that no run-path function in the daemon/workspace packages
// uses bare os.ReadFile/os.Stat/os.Open for run-scoped marker files, and that
// readGateVerdictVia / gateVerdictExistsVia route through the runner on remote runs.

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestNoBareOsOnRunPath_hkhd2w6 is the static "no bare os.* on run path" audit.
//
// After hk-hd2w6, ALL run-scoped marker-file reads in the listed files MUST go
// through a CommandRunner-aware Via function. Any bare os.ReadFile/os.Stat/os.Open
// call on a marker file path in a non-helper function is a bug.
//
// The audit scans each target file for the combination of:
//  1. A bare os.ReadFile( or os.Stat( or os.Open( call
//  2. Where the argument references a run-scoped marker identifier
//     (verdictPath, gateVerdict, auto_status, reviewerBudget, review.json)
//
// It then checks that each hit is ONLY inside a known local-helper function
// (e.g. readGateVerdict, ReadAutoStatusMarker, ReadReviewVerdict,
// ReadReviewerBudgetSentinel). Any hit outside those functions fails the test.
func TestNoBareOsOnRunPath_hkhd2w6(t *testing.T) {
	t.Parallel()

	// Find repo root (the directory containing internal/).
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile is .../internal/daemon/runner_seam_hkhd2w6_test.go
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	// Target files to scan.
	targets := []string{
		filepath.Join(repoRoot, "internal", "daemon", "dot_gate.go"),
		filepath.Join(repoRoot, "internal", "daemon", "dot_cascade.go"),
		filepath.Join(repoRoot, "internal", "daemon", "reviewloop.go"),
		filepath.Join(repoRoot, "internal", "workspace", "reviewverdict.go"),
		filepath.Join(repoRoot, "internal", "workspace", "autostatusmarker.go"),
	}

	// Marker identifiers that appear in run-scoped file path variables/strings.
	markerKeywords := []string{
		"verdictPath",
		"gateVerdict",
		"auto_status",
		"review.json",
		"budgetSentinel",
		"reviewerBudget",
		"AutoStatusMarkerPath",
	}

	// Known-OK local-helper function names (these ARE allowed to call bare os.*).
	localHelpers := map[string]bool{
		"readGateVerdict":            true,
		"ReadAutoStatusMarker":       true,
		"ReadReviewVerdict":          true,
		"ReadReviewerBudgetSentinel": true,
		"ParseAutoStatusMarker":      true,
		"parseReviewVerdict":         true,
		"writeCognitionGateTask":     true,
		"AutoStatusMarkerPath":       true,
		"statTaskFile":               true,
		"statTaskFileVia":            true,
	}

	for _, target := range targets {
		checkRunPathFile(t, target, markerKeywords, localHelpers)
	}
}

func checkRunPathFile(t *testing.T, path string, markers []string, localHelpers map[string]bool) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var currentFunc string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Track current function name (naive: look for "func " prefix at the line start).
		if strings.HasPrefix(trimmed, "func ") {
			name := extractFuncNameFromLine(trimmed)
			if name != "" {
				currentFunc = name
			}
		}

		// Check for bare os.ReadFile/os.Stat/os.Open.
		bareCall := ""
		for _, op := range []string{"os.ReadFile(", "os.Stat(", "os.Open("} {
			if strings.Contains(line, op) {
				bareCall = op
				break
			}
		}
		if bareCall == "" {
			continue
		}

		// Check if this line references a marker keyword.
		hasMarker := false
		for _, kw := range markers {
			if strings.Contains(line, kw) {
				hasMarker = true
				break
			}
		}
		if !hasMarker {
			continue
		}

		// Skip comment lines.
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		// This is a bare os.* call on a marker path. Is it in an allowed function?
		if localHelpers[currentFunc] {
			continue // OK: this is a local-helper function
		}

		t.Errorf("bare %s on run-scoped marker at %s:%d (in function %q) — must route through a Via function (hk-hd2w6)",
			bareCall, filepath.Base(path), lineNum, currentFunc)
	}
}

func extractFuncNameFromLine(line string) string {
	// "func foo(...)" or "func (r Type) foo(...)"
	line = strings.TrimPrefix(line, "func ")
	// Skip method receiver if present.
	if strings.HasPrefix(line, "(") {
		end := strings.Index(line, ")")
		if end < 0 {
			return ""
		}
		line = strings.TrimSpace(line[end+1:])
	}
	// Extract function name up to '(' or ' '.
	for i, c := range line {
		if c == '(' || c == ' ' {
			return line[:i]
		}
	}
	return line
}

// gateVerdictCatRunner is a non-local CommandRunner stub for gate-verdict tests.
// Routes "cat" calls to the stored srcPath and "test" calls to os.Stat on srcPath.
// The distinct (non-LocalRunner) type causes runnerIsLocalFS to return false.
type gateVerdictCatRunner struct {
	srcPath string // path to cat/test; "" → absent file
}

func (r gateVerdictCatRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	src := r.srcPath
	if src == "" {
		src = filepath.Join(os.TempDir(), "hkhd2w6-nonexistent-gate-verdict-42b7")
	}
	switch name {
	case "cat":
		//nolint:gosec // G204: test-controlled temp path
		return exec.CommandContext(ctx, "cat", src)
	case "test":
		// Simulate `test -f <path>` by using /bin/sh -c "test -f <path>"
		//nolint:gosec // G204: test-controlled temp path
		return exec.CommandContext(ctx, "/bin/sh", "-c", "test -f "+src)
	default:
		//nolint:gosec // G204: test-controlled temp path
		return exec.CommandContext(ctx, "false")
	}
}

func gateVerdictFixtureJSON() []byte {
	return []byte(`{"schema_version":1,"decision":"allow","reason":"contract-test"}`)
}

// TestReadGateVerdictVia_NilRunner_ReadsLocally verifies nil runner → local read (NFR7).
func TestReadGateVerdictVia_NilRunner_ReadsLocally(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	verdictPath := filepath.Join(dir, ".harmonik", "gate-verdict.json")
	if err := os.MkdirAll(filepath.Dir(verdictPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(verdictPath, gateVerdictFixtureJSON(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	action, err := daemon.ExportedReadGateVerdictVia(context.Background(), nil, verdictPath)
	if err != nil {
		t.Fatalf("ExportedReadGateVerdictVia(nil): %v", err)
	}
	if action != core.GateActionAllow {
		t.Errorf("action = %q; want %q", action, core.GateActionAllow)
	}
}

// TestReadGateVerdictVia_LocalRunner_ReadsLocally verifies LocalRunner → local read (NFR7).
func TestReadGateVerdictVia_LocalRunner_ReadsLocally(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	verdictPath := filepath.Join(dir, ".harmonik", "gate-verdict.json")
	if err := os.MkdirAll(filepath.Dir(verdictPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(verdictPath, gateVerdictFixtureJSON(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	action, err := daemon.ExportedReadGateVerdictVia(context.Background(), tmux.LocalRunner{}, verdictPath)
	if err != nil {
		t.Fatalf("ExportedReadGateVerdictVia(LocalRunner): %v", err)
	}
	if action != core.GateActionAllow {
		t.Errorf("action = %q; want %q", action, core.GateActionAllow)
	}
}

// TestReadGateVerdictVia_RemoteRunner_ReadsViaRunner verifies that for a non-local
// runner, the verdict is read via runner.Command (not bare os.ReadFile on box A).
func TestReadGateVerdictVia_RemoteRunner_ReadsViaRunner(t *testing.T) {
	t.Parallel()
	// "Worker-side" gate-verdict.json at an arbitrary path.
	workerFile := filepath.Join(t.TempDir(), "worker-gate-verdict.json")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(workerFile, gateVerdictFixtureJSON(), 0o644); err != nil {
		t.Fatalf("write worker file: %v", err)
	}
	// Box-A verdict path does NOT exist.
	boxAVerdictPath := filepath.Join(t.TempDir(), ".harmonik", "gate-verdict.json")
	if _, err := os.Stat(boxAVerdictPath); err == nil {
		t.Fatal("precondition: box-A verdict path must NOT exist")
	}
	runner := gateVerdictCatRunner{srcPath: workerFile}
	action, err := daemon.ExportedReadGateVerdictVia(context.Background(), runner, boxAVerdictPath)
	if err != nil {
		t.Fatalf("ExportedReadGateVerdictVia(remote): %v", err)
	}
	if action != core.GateActionAllow {
		t.Errorf("action = %q; want %q", action, core.GateActionAllow)
	}
}

// TestGateVerdictExistsVia_NilRunner_LocalStat verifies nil runner → os.Stat (NFR7).
func TestGateVerdictExistsVia_NilRunner_LocalStat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	verdictPath := filepath.Join(dir, "gate-verdict.json")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(verdictPath, gateVerdictFixtureJSON(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !daemon.ExportedGateVerdictExistsVia(context.Background(), nil, verdictPath) {
		t.Error("ExportedGateVerdictExistsVia(nil, present) = false; want true")
	}
	absent := filepath.Join(t.TempDir(), "absent.json")
	if daemon.ExportedGateVerdictExistsVia(context.Background(), nil, absent) {
		t.Error("ExportedGateVerdictExistsVia(nil, absent) = true; want false")
	}
}

// TestGateVerdictExistsVia_RemoteRunner_RoutesViaRunner verifies that for a non-local
// runner the existence check routes through runner.Command("test", "-s", ...).
func TestGateVerdictExistsVia_RemoteRunner_RoutesViaRunner(t *testing.T) {
	t.Parallel()
	workerFile := filepath.Join(t.TempDir(), "present-gate-verdict.json")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(workerFile, gateVerdictFixtureJSON(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Box-A path is always absent; runner routes to workerFile.
	boxAPath := filepath.Join(t.TempDir(), "gate-verdict.json")
	runner := gateVerdictCatRunner{srcPath: workerFile}
	if !daemon.ExportedGateVerdictExistsVia(context.Background(), runner, boxAPath) {
		t.Error("ExportedGateVerdictExistsVia(remote, present) = false; want true (worker file exists)")
	}
	// Now test absent worker file.
	absentRunner := gateVerdictCatRunner{srcPath: ""}
	if daemon.ExportedGateVerdictExistsVia(context.Background(), absentRunner, boxAPath) {
		t.Error("ExportedGateVerdictExistsVia(remote, absent) = true; want false")
	}
}
