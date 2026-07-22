//go:build scenario

package daemon_test

// scenario_orphan_kill_hkbl2k6_test.go — §3 scenario-tier reproduction of
// hk-bl2k6: a keeper restart killed in-flight runs and leaked orphan agent
// processes that polluted the operator's box for 40+ minutes.
//
// Cases:
//   - TestScenario_Bl2k6_SubstrateKill_LeavesNoOrphanDescendant: spawns a REAL
//     tmux window through the production substrate seam
//     (daemon.NewTmuxSubstrate → SpawnWindow), hosting a process that ignores
//     SIGTERM and SIGHUP, terminates it through the production
//     handler.SubstrateSession.Kill path, and asserts no descendant survives.
//
// Why this is scenario-tier and not a unit test: the property is a kernel
// signal-delivery behaviour at the substrate boundary — whether the daemon's
// kill reaches the hosted agent or only the pane shell tmux setsid()'d. It
// depends on tmux really creating a new session/process-group per pane, on the
// real `#{pane_pid}` the production adapter resolves, and on the real
// production Kill sequence. A twin-driven harness asserts on the event stream,
// not on the process table, so it cannot express it. Nothing on the path under
// test is faked: real tmux server, real pane, real PIDs, real signals.
//
// Isolation (following internal/daemon/scenario_ssh_localhost_l2_hk8u2al_test.go):
// the tmux server runs on a private socket (`-L`) with a sandboxed HOME, so it
// never touches the operator's live tmux sessions or reads their ~/.tmux.conf.
// Skips when tmux is not on PATH.
//
// Helper prefix: bl2k6scn (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead ref: hk-bl2k6.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// bl2k6scnSentinelEnv names the file the hosted-process fixture writes its own
// PID to. When set, this test binary runs in "hosted process" mode: it ignores
// SIGTERM and SIGHUP and blocks, standing in for an agent that does not die
// when the pane it lives in is torn down. Re-executing the test binary is the
// house pattern for a real-process fixture
// (internal/lifecycle/orphansweepbr_bi014a_test.go).
//
// The fixture publishes its OWN pid, and only AFTER installing the ignores, so
// the parent cannot observe it before it is signal-immune. Taking the pid from
// the shell's `$!` instead is racy: the shell knows the pid the instant it
// forks, well before the child reaches signal.Ignore, and the test then probes
// a fixture that is still killable — observed as an intermittent self-check
// failure.
//
// A Go sentinel is used rather than a shell `trap "" TERM HUP` deliberately:
// signal disposition under macOS /bin/sh (bash 3.2 in sh mode) proved
// inconsistent across invocation forms, which would silently turn this into a
// test that passes for the wrong reason. signal.Ignore is unambiguous.
const bl2k6scnSentinelEnv = "GO_BL2K6_SCENARIO_HOSTED_PIDFILE"

// bl2k6scnHostedLifetime bounds the sentinel's life so a fixture that somehow
// escapes both the daemon's kill and the test's cleanup still dies on its own.
// This test must never be the thing that leaks a process.
const bl2k6scnHostedLifetime = 5 * time.Minute

// bl2k6scnRunner is a tmux.CommandRunner that redirects every `tmux` invocation
// onto a private server socket with a sandboxed HOME. It is NOT a fake of the
// path under test: the real tmux binary runs and the production OSAdapter
// builds every argv. It only keeps the test off the operator's live tmux server.
type bl2k6scnRunner struct {
	tmuxBin string
	socket  string
	home    string
}

func (r bl2k6scnRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	bin := name
	full := args
	if name == "tmux" {
		bin = r.tmuxBin
		full = append([]string{"-L", r.socket}, args...)
	}
	//nolint:gosec // G204: argv originates in the production OSAdapter, not user input
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Env = append(os.Environ(), "HOME="+r.home)
	return cmd
}

// bl2k6scnPidLive reports whether pid is still present in the process table.
func bl2k6scnPidLive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return !errors.Is(err, syscall.ESRCH)
}

// bl2k6scnReadPidFile polls path until it holds a parseable PID, up to timeout.
func bl2k6scnReadPidFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path) //nolint:gosec // G304: path is t.TempDir()-rooted
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && pid > 1 {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("bl2k6 scenario: pid file %s never held a parseable PID within %s — the tmux pane never ran the fixture", path, timeout)
	return 0
}

// TestScenario_Bl2k6_SubstrateKill_LeavesNoOrphanDescendant is the hk-bl2k6
// field regression, reproduced end-to-end.
//
// Field shape: the keeper restarted the daemon, the daemon killed each
// in-flight run's substrate session, and the hosted agents kept running —
// reparented to init, still burning CPU and holding provider slots, for 40+
// minutes.
//
// Want: after handler.SubstrateSession.Kill returns, NO descendant of the pane
// survives.
func TestScenario_Bl2k6_SubstrateKill_LeavesNoOrphanDescendant(t *testing.T) {
	// Hosted-process mode — see bl2k6scnSentinelEnv. Announce readiness only
	// after the ignores are installed.
	if pidPath := os.Getenv(bl2k6scnSentinelEnv); pidPath != "" {
		signal.Ignore(syscall.SIGTERM, syscall.SIGHUP)
		//nolint:gosec // G306: fixture handshake file, t.TempDir()-rooted
		if writeErr := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); writeErr != nil {
			return
		}
		time.Sleep(bl2k6scnHostedLifetime)
		return
	}

	t.Parallel()

	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("bl2k6 scenario requires tmux on PATH; skipping")
	}

	sandboxHome := t.TempDir()
	workDir := t.TempDir()
	const (
		socketName  = "bl2k6-orphan-test"
		sessionName = "bl2k6-orphan-sess"
		windowName  = "bl2k6-orphan-win"
	)
	runner := bl2k6scnRunner{tmuxBin: tmuxBin, socket: socketName, home: sandboxHome}

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	// ── Private tmux server. ────────────────────────────────────────────────
	if out, startErr := runner.Command(ctx, "tmux", "new-session", "-d", "-s", sessionName).CombinedOutput(); startErr != nil {
		t.Skipf("bl2k6 scenario: could not start tmux on private socket %q: %v: %s", socketName, startErr, out)
	}
	t.Cleanup(func() {
		killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer killCancel()
		_ = runner.Command(killCtx, "tmux", "kill-server").Run() //nolint:errcheck // cleanup error unactionable
	})

	// ── Fixture: pane shell → hosted process, the production topology. ──────
	// The pane shell backgrounds the hosted process and waits, mirroring how a
	// tmux pane hosts an agent: the pane PID the daemon knows about is the
	// SHELL, and the agent is its child.
	pidFile := filepath.Join(workDir, "hosted.pid")
	paneScript := filepath.Join(workDir, "pane.sh")
	paneBody := "#!/bin/sh\n" +
		os.Args[0] + " -test.run=^TestScenario_Bl2k6_SubstrateKill_LeavesNoOrphanDescendant$ &\n" +
		"wait\n"
	//nolint:gosec // G306: must be executable
	if writeErr := os.WriteFile(paneScript, []byte(paneBody), 0o755); writeErr != nil {
		t.Fatalf("bl2k6 scenario: write pane script: %v", writeErr)
	}

	// ── Production substrate seam. ──────────────────────────────────────────
	adapter := tmux.OSAdapter{}.WithRunner(runner)
	substrate := daemon.NewTmuxSubstrate(adapter, sessionName)

	sess, spawnErr := substrate.SpawnWindow(ctx, handler.SubstrateSpawn{
		WindowName: windowName,
		Cwd:        workDir,
		Env:        []string{bl2k6scnSentinelEnv + "=" + pidFile},
		Argv:       []string{"sh", paneScript},
	})
	if spawnErr != nil {
		t.Fatalf("bl2k6 scenario: SpawnWindow: %v", spawnErr)
	}

	hostedPID := bl2k6scnReadPidFile(t, pidFile, 30*time.Second)

	// Unconditional safety net: this test must never leak a process — that
	// would be the exact sin under repair.
	defer func() {
		_ = syscall.Kill(hostedPID, syscall.SIGKILL) //nolint:errcheck // cleanup error unactionable
	}()

	if !bl2k6scnPidLive(hostedPID) {
		t.Fatalf("bl2k6 scenario: hosted PID %d not live before the kill — the fixture never established the orphan scenario", hostedPID)
	}

	// ── Fixture self-check (load-bearing). ──────────────────────────────────
	// The whole test rests on the hosted process being immune to the signals
	// that tmux's own teardown delivers. Assert that rather than assume it: an
	// earlier revision used a shell `trap` that silently was NOT immune, and the
	// test passed against the verbatim pre-fix implementation — proving nothing.
	// If this ever regresses, fail here with a clear cause instead of reporting
	// a false green on the property under test.
	_ = syscall.Kill(hostedPID, syscall.SIGTERM) //nolint:errcheck // probe; liveness is the assertion
	_ = syscall.Kill(hostedPID, syscall.SIGHUP)  //nolint:errcheck // probe; liveness is the assertion
	time.Sleep(300 * time.Millisecond)
	if !bl2k6scnPidLive(hostedPID) {
		t.Fatalf("bl2k6 scenario: fixture PID %d died from a bare SIGTERM/SIGHUP; it must ignore both. "+
			"A fixture that dies on either signal is killed by tmux's window teardown rather than by the daemon's kill, "+
			"so the test would pass even with the orphan bug present", hostedPID)
	}

	// ── The production kill path. ───────────────────────────────────────────
	if killErr := sess.Kill(ctx); killErr != nil {
		t.Errorf("bl2k6 scenario: SubstrateSession.Kill: %v", killErr)
	}

	deadline := time.Now().Add(10 * time.Second)
	for bl2k6scnPidLive(hostedPID) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if bl2k6scnPidLive(hostedPID) {
		t.Errorf("bl2k6 scenario: ORPHAN LEAKED — the hosted process (PID %d) inside tmux window %q is STILL ALIVE 10s after SubstrateSession.Kill returned; want dead. "+
			"Regression shape: Kill signalled only the pane shell PID, so the hosted agent was reparented to init and survived the daemon's kill — the hk-bl2k6 field failure, where orphaned agents burned CPU and held provider slots for 40+ minutes after a keeper restart.",
			hostedPID, windowName)
	}
}
