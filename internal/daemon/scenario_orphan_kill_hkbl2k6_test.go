//go:build scenario

package daemon_test

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

const bl2k6scnSentinelEnv = "GO_BL2K6_SCENARIO_HOSTED_PIDFILE"

const bl2k6scnHostedLifetime = 5 * time.Minute

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

func bl2k6scnPidLive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return !errors.Is(err, syscall.ESRCH)
}

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

	// The default session gets an EXPLICIT command, and that argument is load-bearing.
	//
	// With no command tmux runs an interactive login shell in the default pane. On
	// macOS /etc/zshrc sets HISTFILE=$HOME/.zsh_history unconditionally — after the
	// environment, so clearing HISTFILE or SAVEHIST in the env does not stop it — and
	// that shell writes the file as it dies, which can be AFTER `tmux kill-server`
	// has returned and even after the server pid is gone. When the write lands
	// between t.TempDir's last readdir and its rmdir, rmdir gets ENOTEMPTY and Go
	// fails an already-green test with a message about a temp directory and nothing
	// about orphans. It cost the merge gate seven reds between 22 and 24 August
	// (hk-gt3ax).
	//
	// This session is pure scaffolding: it exists to keep the server alive. The path
	// under test is NewWindowIn, which is untouched by this argument.
	if out, startErr := runner.Command(ctx, "tmux", "new-session", "-d", "-s", sessionName,
		"sleep", "100000").CombinedOutput(); startErr != nil {
		t.Skipf("bl2k6 scenario: could not start tmux on private socket %q: %v: %s", socketName, startErr, out)
	}
	t.Cleanup(func() {
		killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer killCancel()
		_ = runner.Command(killCtx, "tmux", "kill-server").Run() //nolint:errcheck // cleanup error unactionable
	})

	pidFile := filepath.Join(workDir, "hosted.pid")
	paneScript := filepath.Join(workDir, "pane.sh")
	paneBody := "#!/bin/sh\n" +
		os.Args[0] + " -test.run=^TestScenario_Bl2k6_SubstrateKill_LeavesNoOrphanDescendant$ &\n" +
		"wait\n"
	//nolint:gosec // G306: must be executable
	if writeErr := os.WriteFile(paneScript, []byte(paneBody), 0o755); writeErr != nil {
		t.Fatalf("bl2k6 scenario: write pane script: %v", writeErr)
	}

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

	defer func() {
		_ = syscall.Kill(hostedPID, syscall.SIGKILL) //nolint:errcheck // cleanup error unactionable
	}()

	if !bl2k6scnPidLive(hostedPID) {
		t.Fatalf("bl2k6 scenario: hosted PID %d not live before the kill — the fixture never established the orphan scenario", hostedPID)
	}

	_ = syscall.Kill(hostedPID, syscall.SIGTERM) //nolint:errcheck // probe; liveness is the assertion
	_ = syscall.Kill(hostedPID, syscall.SIGHUP)  //nolint:errcheck // probe; liveness is the assertion
	time.Sleep(300 * time.Millisecond)
	if !bl2k6scnPidLive(hostedPID) {
		t.Fatalf("bl2k6 scenario: fixture PID %d died from a bare SIGTERM/SIGHUP; it must ignore both. "+
			"A fixture that dies on either signal is killed by tmux's window teardown rather than by the daemon's kill, "+
			"so the test would pass even with the orphan bug present", hostedPID)
	}

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
