package daemon

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

const sockpartBaseConfigYAML = `
schema_version: 1
sentinel:
  liveness_no_progress_n: 0
`

const sockpartDisabledConfigYAML = sockpartBaseConfigYAML + `
subsystems:
  socket_listener:
    enabled: false
`

func sockpartSockPathUnder(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "daemon.sock")
}

func sockpartProjectDir(t *testing.T, yamlContent string) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = t.TempDir()
	if lifecycle.ValidateSocketPathLength(sockpartSockPathUnder(projectDir)) != nil {
		dir, err := os.MkdirTemp("/tmp", "sockpart-")
		if err != nil {
			t.Fatalf("sockpartProjectDir: MkdirTemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // cleanup error unactionable
		projectDir = dir
	}
	if lenErr := lifecycle.ValidateSocketPathLength(sockpartSockPathUnder(projectDir)); lenErr != nil {
		t.Fatalf("sockpartProjectDir: no bindable socket path for this test: %v", lenErr)
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

const sockpartBootMilestone = "composition-root wiring audit"

const sockpartBootCeiling = 30 * time.Second

func sockpartRunDaemon(t *testing.T, projectDir, jsonlPath string) string {
	ceiling := sockpartBootCeiling
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
	startReturned := make(chan struct{})
	go func() {
		done <- StartForTesting(ctx, cfg)
		close(startReturned)
	}()

	if !sockpartAwaitBootMilestone(logBuf, ceiling, startReturned) {
		t.Logf("sockpartRunDaemon: %q not seen within %s; cancelling anyway and letting the caller assert",
			sockpartBootMilestone, ceiling)
	}
	cancel()
	select {
	case startErr := <-done:
		t.Logf("daemon.Start returned: %v", startErr)
	case <-time.After(daemonExitHangBudget):
		t.Fatalf("daemon.Start did not return within %s after context cancellation", daemonExitHangBudget)
	}
	return logBuf.String()
}

func sockpartAwaitBootMilestone(logBuf *sockpartSyncBuffer, ceiling time.Duration, startReturned <-chan struct{}) bool {
	deadline := time.Now().Add(ceiling)
	returned := false
	for {
		if strings.Contains(logBuf.String(), sockpartBootMilestone) {
			return true
		}
		if returned || !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-startReturned:
			returned = true
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func sockpartSocketAppeared(projectDir string, timeout time.Duration) bool {
	return sockpartSocketAppearedUntil(projectDir, timeout, nil)
}

func sockpartSocketAppearedUntil(projectDir string, timeout time.Duration, stop <-chan struct{}) bool {
	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	deadline := time.Now().Add(timeout)
	stopped := false
	for {
		if _, err := os.Stat(sockPath); err == nil {
			return true
		}
		if stopped || !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-stop:
			stopped = true
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Default state: no subsystems: block → the listener and its subtree are
// constructed and the socket is bound, exactly as before partitioning existed.
func TestSubsystemPartition_SocketListener_DefaultConstructsSubtree(t *testing.T) {
	t.Setenv("HARMONIK_DEBUG_WIRING", "1")

	projectDir, jsonlPath := sockpartProjectDir(t, sockpartBaseConfigYAML)

	socketSeen := make(chan bool, 1)
	go func() { socketSeen <- sockpartSocketAppeared(projectDir, sockpartBootCeiling) }()

	logs := sockpartRunDaemon(t, projectDir, jsonlPath)

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

	daemonStopped := make(chan struct{})
	socketSeen := make(chan bool, 1)
	go func() {
		socketSeen <- sockpartSocketAppearedUntil(projectDir, sockpartBootCeiling, daemonStopped)
	}()

	logs := sockpartRunDaemon(t, projectDir, jsonlPath)
	close(daemonStopped)

	if <-socketSeen {
		t.Error("daemon.sock was bound with subsystems.socket_listener.enabled: false; off means never constructed, not constructed-and-inert")
	}
	if !strings.Contains(logs, "socket listener and its handler subtree not constructed") {
		t.Error("the daemon did not report the socket-listener partition at boot; a silent partition is indistinguishable from a config that did not take effect")
	}
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

// The fixture must hand back a project root the daemon can bind a socket under,
// even when the calling test's own name is long. t.TempDir() embeds that name,
// truncated to 64 characters, then appends a random suffix that is sometimes 9
// digits and sometimes 10. Only one of the two draws used to cross the line, so
// a single run proves nothing. Each subtest draws a fresh random.
//
// This test's name is deliberately past the 64-character truncation point,
// which is the case that broke (hk-m3jai).
func TestSockpartProjectDir_HandsBackABindableSocketPathForALongTestName(t *testing.T) {
	for i := range 16 {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			projectDir, _ := sockpartProjectDir(t, sockpartBaseConfigYAML)
			sockPath := sockpartSockPathUnder(projectDir)
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
			if err != nil {
				t.Fatalf("the fixture returned a %d-byte socket path the kernel refuses: %v", len(sockPath), err)
			}
			_ = ln.Close()
		})
	}
}
