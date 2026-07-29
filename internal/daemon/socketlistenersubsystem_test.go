package daemon

// socketlistenersubsystem_test.go — subsystem partitioning of the socket
// listener and the whole handler subtree constructed beneath it.
//
// Both states are driven through the REAL config edge: a .harmonik/config.yaml
// written to disk and read by the daemon's own boot path, exactly as an operator
// would set it. Nothing here hand-builds a SubsystemsConfig.
//
// "Off" is asserted BEHAVIOURALLY, not as a boolean:
//   - the Unix socket file never appears on disk (a constructed-but-inert
//     listener would still bind it), and
//   - the daemon still reaches its work loop, proving the ~13 nil bootState
//     fields the gate leaves behind do not take the daemon down. The branch
//     reaper is the sharp one: its StartWatcher spawns a goroutine that
//     dereferences the receiver, so an unguarded nil would kill the whole test
//     binary rather than fail one test.
//
// Helper prefix: sockpart (implementer-protocol.md §Helper-prefix discipline).

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

// sockpartBaseConfigYAML is the minimum .harmonik/config.yaml the daemon will
// boot with. Once a config file exists at all, the sentinel's G-liveness key is
// required with no compiled default, so it has to be present in BOTH the default
// and the disabled fixture — the only difference between them is the subsystems:
// block itself.
const sockpartBaseConfigYAML = `
schema_version: 1
sentinel:
  liveness_no_progress_n: 0
`

// sockpartDisabledConfigYAML is the same config with the socket listener
// switched off — the one line an operator writes to partition it away.
const sockpartDisabledConfigYAML = sockpartBaseConfigYAML + `
subsystems:
  socket_listener:
    enabled: false
`

// sockpartProjectDir creates a project root with the .harmonik/events sub-tree
// and, when yamlContent is non-empty, a .harmonik/config.yaml holding it.
func sockpartProjectDir(t *testing.T, yamlContent string) (projectDir, jsonlPath string) {
	t.Helper()
	// The socket path must fit in sun_path (104 bytes on darwin); the default
	// TMPDIR does not on macOS. Same fallback the socket-bind tests use.
	const harmonikRelSock = "/.harmonik/daemon.sock"
	const sockpartSunPathMax = 104
	projectDir = t.TempDir()
	if len(projectDir)+len(harmonikRelSock) > sockpartSunPathMax {
		dir, err := os.MkdirTemp("/tmp", "sockpart-")
		if err != nil {
			t.Fatalf("sockpartProjectDir: MkdirTemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // cleanup error unactionable
		projectDir = dir
	}
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(filepath.Join(harmonikDir, "events"), 0o750); err != nil {
		t.Fatalf("sockpartProjectDir: mkdir events: %v", err)
	}
	if yamlContent != "" {
		if err := os.WriteFile(filepath.Join(harmonikDir, "config.yaml"), []byte(yamlContent), 0o600); err != nil {
			t.Fatalf("sockpartProjectDir: write config.yaml: %v", err)
		}
	}
	return projectDir, filepath.Join(harmonikDir, "events", "events.jsonl")
}

// sockpartStubBr writes an executable stand-in for the br binary. The daemon
// refuses to boot without a working `br --version` handshake (BI-024a), and the
// work loop is only reached when BrPath is set — but no test here cares what br
// says, so the stub answers --version with a parseable string (a version delta is
// a notice, not a failure) and every other subcommand with an empty ledger.
func sockpartStubBr(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "br")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"br 0.0.0\"; exit 0; fi\necho '{\"issues\":[]}'\nexit 0\n"
	//nolint:gosec // G306: an executable stub in a test-only temp dir
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("sockpartStubBr: write: %v", err)
	}
	return path
}

// sockpartSyncBuffer is a concurrency-safe io.Writer for Config.LogWriter, which
// the daemon writes to from several goroutines.
type sockpartSyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *sockpartSyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *sockpartSyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// sockpartRunDaemon boots the daemon against projectDir in a goroutine, waits
// for waitFor, cancels, and returns the captured log output. A panic in any
// daemon goroutine (the branch reaper is the live hazard) takes the test binary
// down here, which is the point.
func sockpartRunDaemon(t *testing.T, projectDir, jsonlPath string, waitFor time.Duration) string {
	t.Helper()
	logBuf := &sockpartSyncBuffer{}
	cfg := Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              sockpartStubBr(t), // non-empty so the work loop is reached
		WorkflowModeDefault: core.WorkflowModeDot,
		LogWriter:           logBuf,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- StartForTesting(ctx, cfg) }()

	time.Sleep(waitFor)
	cancel()
	select {
	case startErr := <-done:
		t.Logf("daemon.Start returned: %v", startErr)
	case <-time.After(10 * time.Second):
		t.Fatal("daemon.Start did not return within 10 s after context cancellation")
	}
	return logBuf.String()
}

// sockpartSocketAppeared polls for the daemon socket for up to timeout.
func sockpartSocketAppeared(projectDir string, timeout time.Duration) bool {
	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Default state: no subsystems: block → the listener and its subtree are
// constructed and the socket is bound, exactly as before partitioning existed.
func TestSubsystemPartition_SocketListener_DefaultConstructsSubtree(t *testing.T) {
	t.Setenv("HARMONIK_DEBUG_WIRING", "1")

	projectDir, jsonlPath := sockpartProjectDir(t, sockpartBaseConfigYAML)

	socketSeen := make(chan bool, 1)
	go func() { socketSeen <- sockpartSocketAppeared(projectDir, 5*time.Second) }()

	logs := sockpartRunDaemon(t, projectDir, jsonlPath, 1500*time.Millisecond)

	if !<-socketSeen {
		t.Error("daemon.sock never appeared with no subsystems: block; the socket subtree must be constructed by default")
	}
	if strings.Contains(logs, "socket listener and its handler subtree not constructed") {
		t.Error("the socket-listener partition fired with no subsystems: block; absent config must not disable anything")
	}
	if !strings.Contains(logs, "composition-root wiring audit") {
		t.Error("daemon did not reach startBackgroundLoops in the default case; the test proves nothing about the disabled case unless both reach the same point")
	}
}

// Disabled state: the socket subtree is ABSENT — nothing beneath bindSocket is
// constructed, no socket is bound, and the daemon still reaches its work loop.
func TestSubsystemPartition_SocketListener_DisabledIsAbsent(t *testing.T) {
	t.Setenv("HARMONIK_DEBUG_WIRING", "1")

	projectDir, jsonlPath := sockpartProjectDir(t, sockpartDisabledConfigYAML)

	socketSeen := make(chan bool, 1)
	go func() { socketSeen <- sockpartSocketAppeared(projectDir, 1500*time.Millisecond) }()

	logs := sockpartRunDaemon(t, projectDir, jsonlPath, 1500*time.Millisecond)

	if <-socketSeen {
		t.Error("daemon.sock was bound with subsystems.socket_listener.enabled: false; off means never constructed, not constructed-and-inert")
	}
	if !strings.Contains(logs, "socket listener and its handler subtree not constructed") {
		t.Error("the daemon did not report the socket-listener partition at boot; a silent partition is indistinguishable from a config that did not take effect")
	}
	// The work loop is the real assertion: every consumer of the now-nil
	// bootState fields (branch reaper, crew handler, pause/concurrency
	// controllers, drain detector, queue handler adapter) is crossed on the way
	// here, and the branch reaper would have panicked a goroutine before this
	// line was written.
	if !strings.Contains(logs, "composition-root wiring audit") {
		t.Error("daemon did not reach startBackgroundLoops with the socket listener switched off; the core must run without the socket subtree")
	}
}

// The gate short-circuits BEFORE touching anything: a bootState with every
// field nil survives it. Any construction that leaked past the switch would
// dereference one of those nils and panic here.
func TestSubsystemPartition_SocketListener_GateConstructsNothing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".harmonik"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".harmonik", "config.yaml"), []byte(sockpartDisabledConfigYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	pc, err := projectconfig.LoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	bs := &bootState{cfg: Config{ProjectDir: root, ProjectCfg: pc, LogWriter: &sockpartSyncBuffer{}}}
	constructed, bindErr := bs.bindSocketIfEnabled(context.Background())
	if bindErr != nil {
		t.Fatalf("bindSocketIfEnabled returned %v; a disabled subsystem is not an error", bindErr)
	}
	if constructed {
		t.Fatal("bindSocketIfEnabled = true with subsystems.socket_listener.enabled: false; the subtree must be ABSENT")
	}
	if bs.crewHandler != nil || bs.branchReapWatcher != nil || bs.opPauseCtrl != nil ||
		bs.concurrencyCtrl != nil || bs.drainDet != nil || bs.queueHandlerAdapter != nil {
		t.Error("a bootState field was populated behind the disabled socket-listener switch")
	}
}
