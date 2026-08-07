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

// sockpartSockPathUnder returns the daemon socket path for a project root.
func sockpartSockPathUnder(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "daemon.sock")
}

// sockpartProjectDir creates a project root with the .harmonik/events sub-tree
// and, when yamlContent is non-empty, a .harmonik/config.yaml holding it.
//
// The daemon binds <projectDir>/.harmonik/daemon.sock, so the project root must
// leave room for that inside sun_path — 104 bytes on darwin, one of which the
// kernel keeps for the NUL terminator. t.TempDir() embeds the TEST'S OWN NAME
// and a suffix that is sometimes 9 digits and sometimes 10, so a long test name
// crosses the line on some runs and not on others. The check below therefore
// calls lifecycle.ValidateSocketPathLength, the same check the daemon runs
// before it binds. Do not re-spell the limit here (hk-m3jai).
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
	// Say it here, in one line. A too-long path otherwise reaches the test as a
	// ten-second dial timeout ending in `connect: invalid argument`, which reads
	// as a fault in the subsystem under test.
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

// sockpartBootMilestone is the log line every caller of sockpartRunDaemon
// asserts on. It is written by logCompositionRoot inside startBackgroundLoops,
// which runs strictly AFTER bindSocketIfEnabled — so its arrival proves the
// daemon is past the socket CONSTRUCTION SEAM. Say it that way and not "past the
// bind": startSocketListener hands the actual net.Listen to a goroutine, so the
// milestone proves the call site was crossed, not that the file exists. That is
// still the right synchronisation point for the tests that assert the socket is
// ABSENT — nothing beneath the gate was constructed, so nothing can bind later —
// and the tests that assert it is PRESENT poll for the file itself.
const sockpartBootMilestone = "composition-root wiring audit"

// sockpartBootCeiling is the upper bound on how long a caller waits for the boot
// milestone. It is deliberately far above any healthy boot: a healthy boot exits
// the poll in a couple of hundred milliseconds and never approaches this, so
// raising it costs nothing on a green run. It exists only so a genuinely wedged
// boot still fails rather than hanging.
const sockpartBootCeiling = 30 * time.Second

// sockpartRunDaemon boots the daemon against projectDir in a goroutine, waits
// for it to REACH ITS BOOT MILESTONE (or for ceiling to expire), cancels, and
// returns the captured log output. A panic in any daemon goroutine (the branch
// reaper is the live hazard) takes the test binary down here, which is the point.
//
// WAIT FOR THE MILESTONE, NOT FOR A DURATION (hk-fr7ht). This helper used to
// sleep a flat 1500 ms and then cancel, whether boot had got anywhere or not.
// That made every caller a bet on a wall clock. When the bet lost, the cancel
// landed while `br --version` was still in flight inside CheckBrRunnable, and the
// test reported
//
//	daemon.Start: br is not runnable (BI-024a, exit code 8):
//	brcli: subprocess killed by context: context canceled
//
// which names br and the reconcile adapters and has nothing to do with the
// subsystem under test. Measured on this box 2026-08-07 over six full runs of the
// core package set: two went red, and BOTH reds were this fixture and nothing
// else. Every failure landed at 1.51-1.53 s — the budget expiring, not a machine
// drifting. The failing SET reshuffles between runs, which is why this has read
// as three separate problems. The four tests are non-parallel and run late in the
// sequential batch behind about a hundred daemon-booting neighbours, so the boot
// they were timing is the slowest one in the package.
//
// Polling is strictly better on both sides. A boot that reaches the milestone in
// 200 ms no longer costs 1500 ms, and a boot that needs 4 s is no longer reported
// as a wiring defect.
//
// The budget is no longer a PARAMETER. It used to be, because each caller was
// choosing how long to sleep, and choosing was the defect. What is left is an
// upper bound that no healthy boot reaches, and there is no reason for one caller
// to want a different one — a per-caller ceiling would only be an invitation to
// start tuning wall clocks again.
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
	// startReturned is a SEPARATE signal from done, not a second reader of it.
	// The milestone poll must be able to notice that Start has answered without
	// consuming the answer — the block below still has to read the error and log
	// it, and a poller that took the value would leave that read to time out.
	startReturned := make(chan struct{})
	go func() {
		done <- StartForTesting(ctx, cfg)
		close(startReturned)
	}()

	if !sockpartAwaitBootMilestone(logBuf, ceiling, startReturned) {
		// Do NOT fail here. The caller owns the assertion, and its message says
		// which subsystem it was proving something about. Say what was seen so a
		// genuine boot hang is still distinguishable from a slow one.
		t.Logf("sockpartRunDaemon: %q not seen within %s; cancelling anyway and letting the caller assert",
			sockpartBootMilestone, ceiling)
	}
	cancel()
	select {
	case startErr := <-done:
		t.Logf("daemon.Start returned: %v", startErr)
	case <-time.After(10 * time.Second):
		t.Fatal("daemon.Start did not return within 10 s after context cancellation")
	}
	return logBuf.String()
}

// sockpartAwaitBootMilestone polls the daemon's log buffer for
// sockpartBootMilestone, up to ceiling. Reports whether it arrived.
//
// startReturned is closed once daemon.Start has answered. Watching it matters:
// the failure this fixture exists to catch is a boot that ERRORS, and a poller
// that only watches the clock would sit out the whole ceiling on an answer that
// arrived in the first few hundred milliseconds. One last look at the buffer
// after it closes, because Start can log the milestone and then return.
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

// sockpartSocketAppeared polls for the daemon socket for up to timeout.
func sockpartSocketAppeared(projectDir string, timeout time.Duration) bool {
	return sockpartSocketAppearedUntil(projectDir, timeout, nil)
}

// sockpartSocketAppearedUntil polls for the daemon socket until it appears, until
// stop is closed, or until timeout — whichever comes first.
//
// stop is what makes a NEGATIVE assertion sound. "The socket never appeared" is
// only evidence if the poll covered the daemon's whole life; a poll on a fixed
// timer can end while the daemon is still booting, and then it has proved
// nothing. The caller closes stop once the daemon has been cancelled and
// daemon.Start has returned, so the window polled always contains the window in
// which a bind could have happened.
func sockpartSocketAppearedUntil(projectDir string, timeout time.Duration, stop <-chan struct{}) bool {
	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	deadline := time.Now().Add(timeout)
	stopped := false
	for {
		if _, err := os.Stat(sockPath); err == nil {
			return true
		}
		// One further sweep AFTER stop closes, so a socket bound in the last
		// instant before shutdown is not missed.
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

	// Same ceiling the daemon gets. A fixed 5 s here would fail with "daemon.sock
	// never appeared" on any boot between 5 s and the daemon's own ceiling — the
	// same false negative this change removes everywhere else. It costs nothing:
	// the poll returns on first sighting, so a healthy run still leaves in
	// milliseconds.
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

	// The poll runs for the daemon's WHOLE life, not for a fixed 1500 ms. See
	// sockpartSocketAppearedUntil: a negative that stops polling while the subject
	// is still booting is not a negative.
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
