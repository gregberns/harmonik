package main

// state_cmd_coverage_test.go — behavior tests for the `harmonik state` pure
// logic: project-root discovery, project-dir resolution, single-row writing,
// the human summary renderer, and the disk-fallback / --help / --json paths of
// the top-level subcommand. No live daemon is required — every path exercised
// here reads disk or a supplied fixture snapshot.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// captureStateStdout redirects os.Stdout around fn and returns what was written.
func captureStateStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	os.Stdout = old
	buf, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	return string(buf)
}

// errWriter always fails, to drive writeStateRow's error branch.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestFindProjectRoot(t *testing.T) {
	base := t.TempDir()
	// base/proj/.harmonik exists; base/proj/sub/deep is a descendant.
	proj := filepath.Join(base, "proj")
	deep := filepath.Join(proj, "sub", "deep")
	if err := os.MkdirAll(filepath.Join(proj, ".harmonik"), 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatalf("mkdir deep: %v", err)
	}
	noRoot := t.TempDir() // separate tree, no .harmonik anywhere up to filesystem walls

	tests := []struct {
		name string
		dir  string
		want string
	}{
		{"root itself", proj, proj},
		{"descendant walks up", deep, proj},
		{"no .harmonik returns empty", noRoot, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := findProjectRoot(tc.dir); got != tc.want {
				t.Errorf("findProjectRoot(%q) = %q, want %q", tc.dir, got, tc.want)
			}
		})
	}
}

func TestResolveProjectDirForState_UsesEnv(t *testing.T) {
	t.Setenv("HK_PROJECT", "/some/explicit/project")
	got, err := resolveProjectDirForState()
	if err != nil {
		t.Fatalf("resolveProjectDirForState: %v", err)
	}
	if got != "/some/explicit/project" {
		t.Errorf("got %q, want the HK_PROJECT value", got)
	}
}

func TestResolveProjectDirForState_FallsBackToCwd(t *testing.T) {
	t.Setenv("HK_PROJECT", "")
	got, err := resolveProjectDirForState()
	if err != nil {
		t.Fatalf("resolveProjectDirForState: %v", err)
	}
	if got == "" {
		t.Error("expected a non-empty project dir when HK_PROJECT is unset")
	}
}

func TestWriteStateRow_Success(t *testing.T) {
	var buf bytes.Buffer
	if err := writeStateRow(&buf, "daemon\t%s\n", "up"); err != nil {
		t.Fatalf("writeStateRow: %v", err)
	}
	if got := buf.String(); got != "daemon\tup\n" {
		t.Errorf("buffer = %q, want %q", got, "daemon\tup\n")
	}
}

func TestWriteStateRow_Error(t *testing.T) {
	err := writeStateRow(errWriter{}, "x\t%s\n", "y")
	if err == nil {
		t.Fatal("expected an error from a failing writer")
	}
	if !strings.Contains(err.Error(), "write state summary") {
		t.Errorf("error %q: expected it to be wrapped with 'write state summary'", err)
	}
}

// fixtureSnapshot builds a fully-populated snapshot to exercise every branch of
// printStateHuman (daemon up, unsure read-quality, runs, queues, all three
// session states).
func fixtureSnapshot() daemon.StateSnapshot {
	return daemon.StateSnapshot{
		SchemaVersion: 1,
		CapturedAt:    "2026-07-23T00:00:00Z",
		Daemon:        daemon.StateDaemon{Up: true, Pid: 4242},
		ActivityLabel: daemon.ActivityProcessing,
		ReadQuality:   daemon.ReadQuality{Ok: false, Unsure: true, Reasons: []string{"socket absent"}},
		Runs: []daemon.StateRun{
			{RunID: "run/abc", BeadID: "hk-1", QueueName: "main", LifecycleState: "PROCESSING"},
		},
		Queues: []daemon.StateQueue{
			{Name: "main", Status: "active", ItemCount: 3, ActiveCount: 1, EffectiveWorkerCap: 4, EligibleNow: true},
			{Name: "paused-q", Status: "paused", ItemCount: 0, ActiveCount: 0, EffectiveWorkerCap: 0},
		},
		Sessions: []daemon.StateSession{
			{Agent: "captain", Alive: true, Cognition: &daemon.SessionCognition{Context: daemon.SessionContext{FillFrac: 0.42}}},
			{Agent: "paul", Alive: false},
			{Agent: "resting", Alive: true, AtRest: true},
		},
	}
}

func TestPrintStateHuman_RendersAllSections(t *testing.T) {
	snap := fixtureSnapshot()
	var err error
	out := captureStateStdout(t, func() { err = printStateHuman(snap) })
	if err != nil {
		t.Fatalf("printStateHuman: %v", err)
	}

	wantSubstrings := []string{
		"up (pid 4242)", // daemon up branch
		"PROCESSING",    // activity label
		"read_quality",  // unsure branch
		"socket absent", // reason line
		"run/abc",       // run row
		"bead=hk-1",
		"main", // queue name
		"[eligible]",
		"captain",    // alive session
		"fill=42.0%", // cognition fill
		"dead",       // !Alive session
		"sleeping",   // AtRest session
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; full output:\n%s", want, out)
		}
	}
}

func TestPrintStateHuman_DaemonDownMinimal(t *testing.T) {
	// Down daemon, ok read-quality, no runs/queues/sessions: the compact path.
	snap := daemon.StateSnapshot{
		Daemon:        daemon.StateDaemon{Up: false},
		ActivityLabel: daemon.ActivityInactive,
		CapturedAt:    "2026-07-23T00:00:00Z",
		ReadQuality:   daemon.ReadQuality{Ok: true},
	}
	var err error
	out := captureStateStdout(t, func() { err = printStateHuman(snap) })
	if err != nil {
		t.Fatalf("printStateHuman: %v", err)
	}
	if !strings.Contains(out, "down") {
		t.Errorf("expected 'down' in output; got:\n%s", out)
	}
	if strings.Contains(out, "read_quality") {
		t.Errorf("ok read-quality should not print a read_quality row; got:\n%s", out)
	}
}

func TestRunStateSubcommand_Help(t *testing.T) {
	if code := runStateSubcommand([]string{"--help"}); code != 0 {
		t.Errorf("--help exit = %d, want 0", code)
	}
}

// TestRunStateSubcommand_DiskFallbackJSON drives the daemon-down disk path end
// to end against a temp project and asserts valid JSON is emitted, exit 0.
func TestRunStateSubcommand_DiskFallbackJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	t.Setenv("HK_PROJECT", dir)

	var code int
	out := captureStateStdout(t, func() { code = runStateSubcommand([]string{"--json"}) })
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var snap daemon.StateSnapshot
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatalf("emitted output is not valid StateSnapshot JSON: %v\n%s", err, out)
	}
	if snap.Daemon.Up {
		t.Errorf("daemon should be reported down for a bare temp project")
	}
}
