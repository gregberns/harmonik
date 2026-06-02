package supervise_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/supervise"
)

// TestDaemonWatchdog_NoReviveWhenAlive verifies that when the daemon socket is
// reachable, the watchdog does not attempt to spawn a revival process.
func TestDaemonWatchdog_NoReviveWhenAlive(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "daemon.sock")
	markerPath := filepath.Join(tmpDir, "revived")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	spec := supervise.DaemonWatchdogSpec{
		SocketPath:    sockPath,
		Command:       []string{"sh", "-c", "touch " + markerPath},
		CheckInterval: 30 * time.Millisecond,
		DialTimeout:   50 * time.Millisecond,
		MaxRevives:    1,
		ReviveBackoff: 5 * time.Millisecond,
	}

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_ = dw.Run(ctx) // exits on ctx timeout

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("daemon was alive but revival was triggered")
	}
}

// TestDaemonWatchdog_RevivesOnDeadSocket verifies that when the daemon socket
// is absent, the watchdog spawns the revival command.
func TestDaemonWatchdog_RevivesOnDeadSocket(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "daemon.sock") // intentionally absent
	markerPath := filepath.Join(tmpDir, "revived")

	spec := supervise.DaemonWatchdogSpec{
		SocketPath:    sockPath,
		Command:       []string{"sh", "-c", "touch " + markerPath},
		CheckInterval: 30 * time.Millisecond,
		DialTimeout:   20 * time.Millisecond,
		MaxRevives:    1,
		ReviveBackoff: 10 * time.Millisecond,
	}

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = dw.Run(ctx) // exits when revival cap reached

	// Give the detached `touch` process time to complete.
	time.Sleep(150 * time.Millisecond)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("expected revival command to be spawned, but marker file was not created")
	}
}

// TestDaemonWatchdog_CrashLoopGuard verifies that the watchdog stops after
// MaxRevives attempts when the daemon socket remains unreachable.
func TestDaemonWatchdog_CrashLoopGuard(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "daemon.sock") // intentionally absent

	spec := supervise.DaemonWatchdogSpec{
		SocketPath:    sockPath,
		Command:       []string{"true"}, // completes immediately; socket stays absent
		CheckInterval: 20 * time.Millisecond,
		DialTimeout:   10 * time.Millisecond,
		MaxRevives:    2,
		ReviveBackoff: 5 * time.Millisecond,
	}

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := dw.Run(ctx)
	if err == nil {
		t.Fatal("expected crash-loop error, got nil")
	}
}

// TestDaemonWatchdog_StopsOnContextCancel verifies that Run returns promptly
// when the context is cancelled, even if no tick has fired yet.
func TestDaemonWatchdog_StopsOnContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "daemon.sock") // absent

	spec := supervise.DaemonWatchdogSpec{
		SocketPath:    sockPath,
		Command:       []string{"true"},
		CheckInterval: 10 * time.Second, // long interval — cancel should fire first
		DialTimeout:   10 * time.Millisecond,
		MaxRevives:    1,
		ReviveBackoff: 5 * time.Millisecond,
	}

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- dw.Run(ctx) }()

	cancel() // cancel immediately

	select {
	case <-done:
		// OK — watchdog exited promptly
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s after context cancellation")
	}
}
