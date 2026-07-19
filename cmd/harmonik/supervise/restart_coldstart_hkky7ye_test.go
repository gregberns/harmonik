package supervisecmd

// restart_coldstart_hkky7ye_test.go — regression test for hk-ky7ye.
//
// The in-daemon supervisor-watchdog revives a dead supervisor by spawning
// `harmonik supervise restart --watch-restart --project <dir>` (main.go
// buildSupervisorWatchdogSpec). For a STANDALONE daemon whose supervisor never
// ran, there is no .harmonik/cognition/config.json. Before the fix, RunRestart
// aborted at the ReadConfig gate ("restart: read config", exit 1) BEFORE
// reaching start, so no supervisor was ever launched and no supervisor.pid was
// written; the watchdog then exhausted max_revives and left the daemon
// permanently unsupervised. A manual `supervise start` worked because start
// WRITES config.json rather than requiring a pre-existing one.
//
// The fix makes a missing config.json non-fatal in restart: restart falls
// through to start (cold-start), which writes a fresh config. This test proves
// restart no longer stops at the config gate — with no prior config and no
// daemon it now reaches start and fails at the daemon-socket probe (exit 17),
// not at ReadConfig (exit 1).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRestart_NoPriorConfig_ColdStartsPastConfigGate(t *testing.T) {
	t.Parallel()

	// A bare project dir: no .harmonik/cognition/config.json (supervisor never
	// ran) and no daemon socket. This is exactly the standalone-daemon revive
	// scenario the watchdog hits (hk-ky7ye).
	projectDir := t.TempDir()

	// Sanity: config.json must be absent for this to exercise the cold-start path.
	if _, err := os.Stat(ConfigPath(projectDir)); !os.IsNotExist(err) {
		t.Fatalf("precondition: expected no config.json at %s, stat err = %v", ConfigPath(projectDir), err)
	}

	var stdout, stderr bytes.Buffer
	code := RunRestart([]string{"--project", projectDir, "--watch-restart"}, &stdout, &stderr)

	// Post-fix: restart cold-starts past BOTH the stop gate (ReadPidfile ENOENT
	// must be idempotent, exit 0) and the config gate (missing config.json is
	// non-fatal), reaching start — which fails at the daemon-socket probe
	// (exit 17) because no daemon is running. Pre-fix it aborted earlier: at
	// stop ("stop failed", exit 1) or at ReadConfig ("restart: read config").
	got := stderr.String()
	if strings.Contains(got, "restart: read config") {
		t.Fatalf("restart aborted at the config gate instead of cold-starting (hk-ky7ye regression); stderr:\n%s", got)
	}
	if strings.Contains(got, "stop failed") {
		t.Fatalf("restart aborted at the stop gate (ReadPidfile ENOENT not treated as idempotent — hk-ky7ye); stderr:\n%s", got)
	}
	if code != ExitCodeDaemonDown {
		t.Fatalf("RunRestart exit = %d, want %d (ExitCodeDaemonDown) — restart should reach start and fail at the daemon probe; stderr:\n%s",
			code, ExitCodeDaemonDown, got)
	}
	// Proof restart actually reached the start path: the failing message is
	// emitted by start (the "supervise start:" prefix), not by restart/stop.
	// The exact probe error (ENOENT "daemon not running" vs EINVAL for an
	// over-long socket path) varies by environment; the exit code is the stable
	// contract, and the "start:" prefix proves the code path.
	if !strings.Contains(got, "supervise start:") {
		t.Fatalf("expected a start-path (supervise start:) failure, proving restart reached start; stderr:\n%s", got)
	}
}

func TestRunRestart_CorruptExistingConfig_StillFailsClosed(t *testing.T) {
	t.Parallel()

	// A config.json that EXISTS but is unparseable is a genuine corruption:
	// restart must still refuse (exit 1) rather than silently relaunch over it.
	projectDir := t.TempDir()
	//nolint:gosec // G301: test fixture dir
	if err := os.MkdirAll(CognitionDir(projectDir), 0o755); err != nil {
		t.Fatalf("mkdir cognition: %v", err)
	}
	if err := os.WriteFile(ConfigPath(projectDir), []byte("{ this is not json"), 0o600); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}
	// Guard against a stray absolute path.
	if !strings.HasPrefix(ConfigPath(projectDir), filepath.Clean(projectDir)) {
		t.Fatalf("config path %q escaped project dir %q", ConfigPath(projectDir), projectDir)
	}

	var stdout, stderr bytes.Buffer
	code := RunRestart([]string{"--project", projectDir, "--watch-restart"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("RunRestart exit = %d, want 1 for a corrupt existing config; stderr:\n%s", code, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "restart: read config") {
		t.Fatalf("expected the config-read failure message for corrupt config; stderr:\n%s", got)
	}
}
