package daemon_test

// hk-uzvt9 regression guard: applyBootBackoff's sleep must not block the
// daemon's socket bind. Before the fix, the backoff delay was applied
// synchronously BEFORE the socket-bind block (daemon.go ~L816, pre-fix), so a
// restart-backoff delay (n>=2 rapid boots) held up bind long enough for the
// supervisor's 30s health-window to see no socket and revert to last-good
// (the "c0437aeb hangs on live boot" incident, confirmed 2026-06-26).
//
// This test seeds a restart-record with two prior boots (n=2, so
// computeRestartBackoffDelay yields 2×Base) and configures a Base long enough
// that if the daemon slept BEFORE binding, the socket would not appear within
// a short poll window well under Base. It asserts the socket appears almost
// immediately — i.e. bind happens independent of (and before) the backoff
// sleep completes.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func TestDaemonStart_SocketBindsBeforeRestartBackoffSleep(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	// sunPathMax is the sockaddr_un.sun_path buffer size on darwin/BSD (104
	// bytes). It holds the path INCLUDING the NUL terminator, so the longest
	// bindable path is sunPathMax-1 (103) bytes; a path of exactly sunPathMax
	// bytes still overflows with ENAMETOOLONG. The guard below must therefore be
	// strictly-less-than, not <=, or a 104-byte candidate slips past and the
	// bind false-reds under a long TMPDIR (hk-0oebl).
	const sunPathMax = 104
	const harmonikRelSock = "/.harmonik/daemon.sock"

	candidate := t.TempDir()
	var projectDir string
	if len(candidate)+len(harmonikRelSock) < sunPathMax {
		projectDir = candidate
	} else {
		dir, err := os.MkdirTemp("/tmp", "hk-uzvt9-")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // cleanup error unactionable
		projectDir = dir
	}

	harmonikDir := filepath.Join(projectDir, ".harmonik")
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	// Configure a restart-backoff base long enough that a pre-fix
	// sleep-before-bind would keep the socket absent well past our poll
	// window, but short enough the test doesn't hang.
	const backoffBase = 3 * time.Second
	cfgYAML := `schema_version: 1
daemon:
  restart_backoff:
    base: 3s
    cap: 30s
    window: 1h
`
	if err := os.WriteFile(filepath.Join(harmonikDir, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	// Seed the boot record with two prior boots inside the window so this
	// boot computes n=2 -> delay = 2*base (well above our poll deadline).
	now := time.Now()
	recordYAML := struct {
		SchemaVersion int     `json:"schema_version"`
		BootTimesUnix []int64 `json:"boot_times_unix_sec"`
	}{
		SchemaVersion: 1,
		BootTimesUnix: []int64{
			now.Add(-20 * time.Minute).Unix(),
			now.Add(-10 * time.Minute).Unix(),
		},
	}
	cognitionDir := filepath.Join(harmonikDir, "cognition")
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(cognitionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll cognition: %v", err)
	}
	recordBytes, err := json.MarshalIndent(recordYAML, "", "  ")
	if err != nil {
		t.Fatalf("marshal restart record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cognitionDir, "restart-record.json"), recordBytes, 0o644); err != nil {
		t.Fatalf("write restart-record.json: %v", err)
	}

	jsonlPath := filepath.Join(harmonikDir, "events", "events.jsonl")
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(filepath.Dir(jsonlPath), 0o755); err != nil {
		t.Fatalf("MkdirAll events dir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(ctx, daemon.Config{
			ProjectDir:          projectDir,
			JSONLLogPath:        jsonlPath,
			WorkflowModeDefault: core.WorkflowModeReviewLoop,
		})
	}()

	sockPath := filepath.Join(harmonikDir, "daemon.sock")

	// Poll well under backoffBase (3s): if the fix regresses and the sleep
	// runs BEFORE bind, the socket will not appear within this window.
	deadline := time.Now().Add(backoffBase / 2)
	var sockFound bool
	for time.Now().Before(deadline) {
		info, statErr := os.Stat(sockPath)
		if statErr == nil && info.Mode()&os.ModeSocket != 0 {
			sockFound = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	<-startDone

	if !sockFound {
		t.Errorf("daemon.sock not found at %q within %s — backoff sleep may be blocking bind (hk-uzvt9 regression)", sockPath, backoffBase/2)
	}
}
