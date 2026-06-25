package workspace

// autostatusmarker_via_hkhd2w6_test.go — unit tests for ReadAutoStatusMarkerVia (hk-hd2w6).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// autoStatusCatRunner is a non-local CommandRunner stub for testing.
// Routes all Command calls to `cat <srcPath>`, simulating a remote worker whose
// auto_status.json lives at srcPath. The distinct (non-LocalRunner) type causes
// runnerIsLocalFS to return false, routing through the runner.
type autoStatusCatRunner struct {
	srcPath string // path to cat; "" → nonexistent path (cat fails)
}

func (r autoStatusCatRunner) Command(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	src := r.srcPath
	if src == "" {
		src = filepath.Join(os.TempDir(), "hkhd2w6-nonexistent-auto-status-42b7")
	}
	//nolint:gosec // G204: test-controlled temp path
	return exec.CommandContext(ctx, "cat", src)
}

func autoStatusViaFixtureJSON() []byte {
	return []byte(`{"schema_version":1,"status":"FAIL","failure_class":"deterministic","notes":"test","signals":{}}`)
}

func autoStatusViaFixtureWrite(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := AutoStatusMarkerPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write auto_status: %v", err)
	}
	return dir
}

// TestReadAutoStatusMarkerVia_NilRunner_ReadsLocally verifies nil runner → local read (NFR7).
func TestReadAutoStatusMarkerVia_NilRunner_ReadsLocally(t *testing.T) {
	t.Parallel()
	dir := autoStatusViaFixtureWrite(t, autoStatusViaFixtureJSON())
	m, err := ReadAutoStatusMarkerVia(context.Background(), nil, dir)
	if err != nil {
		t.Fatalf("ReadAutoStatusMarkerVia(nil): %v", err)
	}
	if m == nil {
		t.Fatal("ReadAutoStatusMarkerVia(nil) = nil; want marker")
	}
	if m.Status != "FAIL" {
		t.Errorf("Status = %q; want FAIL", m.Status)
	}
}

// TestReadAutoStatusMarkerVia_LocalRunner_ReadsLocally verifies LocalRunner → local read (NFR7).
func TestReadAutoStatusMarkerVia_LocalRunner_ReadsLocally(t *testing.T) {
	t.Parallel()
	dir := autoStatusViaFixtureWrite(t, autoStatusViaFixtureJSON())
	m, err := ReadAutoStatusMarkerVia(context.Background(), tmux.LocalRunner{}, dir)
	if err != nil {
		t.Fatalf("ReadAutoStatusMarkerVia(LocalRunner): %v", err)
	}
	if m == nil || m.Status != "FAIL" {
		t.Fatalf("ReadAutoStatusMarkerVia(LocalRunner) = %+v; want FAIL marker", m)
	}
}

// TestReadAutoStatusMarkerVia_RemoteRunner_ReadsViaRunner verifies that for a non-local
// runner, the marker is read via runner.Command (not bare os.ReadFile on box A).
func TestReadAutoStatusMarkerVia_RemoteRunner_ReadsViaRunner(t *testing.T) {
	t.Parallel()
	// "Worker-side" auto_status.json lives at an arbitrary path the stub cats.
	workerFile := filepath.Join(t.TempDir(), "worker-auto-status.json")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(workerFile, autoStatusViaFixtureJSON(), 0o644); err != nil {
		t.Fatalf("write worker file: %v", err)
	}
	// Box-A workspace has NO auto_status.json.
	boxAWorkspace := t.TempDir()
	if _, err := os.Stat(AutoStatusMarkerPath(boxAWorkspace)); err == nil {
		t.Fatal("precondition: box-A workspace must NOT contain auto_status.json")
	}
	runner := autoStatusCatRunner{srcPath: workerFile}
	m, err := ReadAutoStatusMarkerVia(context.Background(), runner, boxAWorkspace)
	if err != nil {
		t.Fatalf("ReadAutoStatusMarkerVia(remote): %v", err)
	}
	if m == nil {
		t.Fatal("ReadAutoStatusMarkerVia(remote) = nil; want worker-side marker routed via runner")
	}
	if m.Status != "FAIL" {
		t.Errorf("Status = %q; want FAIL", m.Status)
	}
}

// TestReadAutoStatusMarkerVia_RemoteRunner_AbsentReturnsNil verifies absent worker file → (nil, nil).
func TestReadAutoStatusMarkerVia_RemoteRunner_AbsentReturnsNil(t *testing.T) {
	t.Parallel()
	runner := autoStatusCatRunner{srcPath: ""}
	m, err := ReadAutoStatusMarkerVia(context.Background(), runner, t.TempDir())
	if err != nil {
		t.Fatalf("ReadAutoStatusMarkerVia(remote-absent) error = %v; want nil", err)
	}
	if m != nil {
		t.Errorf("ReadAutoStatusMarkerVia(remote-absent) = %+v; want nil", m)
	}
}
