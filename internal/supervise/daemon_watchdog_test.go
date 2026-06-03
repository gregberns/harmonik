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
		ReviveWindow:  50 * time.Millisecond,
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
		ReviveWindow:  50 * time.Millisecond,
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
		ReviveWindow:  30 * time.Millisecond,
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
		ReviveWindow:  50 * time.Millisecond,
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

// TestDaemonWatchdog_ReviveCounterResets verifies that the revival counter
// resets to 0 when the daemon comes alive after a revival, so isolated clean
// revivals spread over days do not accumulate toward the cap (MaxRevives=2).
//
// Sequence:
//   - Socket absent → watchdog fires revive 1 → goroutine brings socket up
//     → pollUntilAlive succeeds → revives reset to 0
//   - Socket drops → watchdog fires revive 2 → goroutine brings socket up
//     → pollUntilAlive succeeds → revives reset to 0
//   - Socket drops → watchdog fires revive 3 (only possible if counter reset;
//     without fix revives=2 would equal MaxRevives=2 and watchdog would give up)
//
// The revival command appends a line to a counter file; we count lines at end.
func TestDaemonWatchdog_ReviveCounterResets(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "daemon.sock")
	counterFile := filepath.Join(tmpDir, "revive-count")

	spec := supervise.DaemonWatchdogSpec{
		SocketPath: sockPath,
		// Each invocation appends one line to counterFile.
		Command:       []string{"sh", "-c", "echo x >> " + counterFile},
		CheckInterval: 20 * time.Millisecond,
		DialTimeout:   10 * time.Millisecond,
		MaxRevives:    2, // without counter reset, a 3rd revive would be blocked
		ReviveBackoff: 5 * time.Millisecond,
		ReviveWindow:  150 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Goroutine cycles the socket: dead → alive → dead → alive → dead → alive
	// then stays alive so ctx-timeout (not cap) terminates Run.
	go func() {
		bindAndServe := func() (net.Listener, bool) {
			ln, err := net.Listen("unix", sockPath)
			if err != nil {
				return nil, false
			}
			go func() {
				for {
					c, e := ln.Accept()
					if e != nil {
						return
					}
					_ = c.Close()
				}
			}()
			return ln, true
		}

		// 3 cycles: let the socket be dead long enough for a revive, then recover.
		for range 3 {
			// Let watchdog detect the dead socket and call revive().
			time.Sleep(60 * time.Millisecond)

			// Bring socket up so pollUntilAlive resets the counter.
			ln, ok := bindAndServe()
			if !ok {
				return
			}
			// Hold it long enough for pollUntilAlive to probe successfully.
			time.Sleep(100 * time.Millisecond)

			// Drop the socket to trigger the next revival.
			_ = ln.Close()
			_ = os.Remove(sockPath)
		}

		// After 3 successful recoveries, bring the socket back up and hold it
		// until ctx expires so the watchdog exits via timeout (not cap).
		time.Sleep(60 * time.Millisecond)
		ln, ok := bindAndServe()
		if !ok {
			return
		}
		defer func() { _ = ln.Close() }()
		<-ctx.Done()
	}()

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	runErr := dw.Run(ctx)

	// Run should exit via ctx cancellation, not via the cap.
	if runErr != nil && ctx.Err() == nil {
		t.Errorf("Run returned early (cap hit?): %v — counter may not be resetting", runErr)
	}

	// Count actual revive() calls (each appended a line).
	data, _ := os.ReadFile(counterFile)
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	if count < 3 {
		t.Errorf("expected ≥3 revive() calls (counter reset proved), got %d", count)
	}
}

// TestDaemonWatchdog_PhantomReviveGuard verifies that a daemon which takes
// longer than ReviveBackoff (but less than ReviveWindow) to bind its socket
// does not consume a phantom revive slot. Models the applyBootBackoff scenario
// where the daemon sleeps before binding — without ReviveWindow the watchdog
// re-probes every ReviveBackoff interval and fires redundant revive calls.
//
// Sequence: socket absent → revive 1 → socket absent for 80ms (> ReviveBackoff
// 15ms, < ReviveWindow 250ms) → socket binds → pollUntilAlive resets counter.
// Test asserts the revival command ran exactly once.
func TestDaemonWatchdog_PhantomReviveGuard(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "daemon.sock")
	counterFile := filepath.Join(tmpDir, "revive-count")

	spec := supervise.DaemonWatchdogSpec{
		SocketPath:    sockPath,
		Command:       []string{"sh", "-c", "echo x >> " + counterFile},
		CheckInterval: 20 * time.Millisecond,
		DialTimeout:   10 * time.Millisecond,
		MaxRevives:    5,
		ReviveBackoff: 15 * time.Millisecond,
		// ReviveWindow covers the simulated 80ms boot-backoff delay.
		ReviveWindow: 250 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Simulate applyBootBackoff: socket stays absent for 80ms after revive(),
	// then binds and stays up for the test duration.
	go func() {
		// Let the watchdog detect the dead socket and call revive().
		time.Sleep(60 * time.Millisecond)
		// Simulate boot-backoff delay: socket still unbound for 80ms.
		time.Sleep(80 * time.Millisecond)
		// Now bind — pollUntilAlive should succeed on its next poll.
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			return
		}
		defer ln.Close()
		go func() {
			for {
				c, e := ln.Accept()
				if e != nil {
					return
				}
				_ = c.Close()
			}
		}()
		<-ctx.Done()
	}()

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	_ = dw.Run(ctx) // exits on ctx timeout (socket stays alive)

	data, _ := os.ReadFile(counterFile)
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 revive() call (boot-backoff covered by ReviveWindow), got %d", count)
	}
}
