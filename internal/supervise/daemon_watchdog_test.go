package supervise_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/supervise"
)

func socketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hkwd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("socketDir cleanup: RemoveAll %s: %v", dir, err)
		}
	})
	return dir
}

func serveAccepts(t *testing.T, ln net.Listener) (join func()) {
	t.Helper()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if closeErr := c.Close(); closeErr != nil {
				t.Errorf("fixture: close accepted conn: %v", closeErr)
			}
		}
	}()
	return func() { <-stopped }
}

// TestDaemonWatchdog_NoReviveWhenAlive verifies that when the daemon socket is
// reachable, the watchdog does not attempt to spawn a revival process.
func TestDaemonWatchdog_NoReviveWhenAlive(t *testing.T) {
	tmpDir := socketDir(t)
	sockPath := filepath.Join(tmpDir, "daemon.sock")
	markerPath := filepath.Join(tmpDir, "revived")

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	joinAccepts := serveAccepts(t, ln)
	defer func() {
		if closeErr := ln.Close(); closeErr != nil {
			t.Errorf("close listener: %v", closeErr)
		}
		joinAccepts()
	}()

	spec := supervise.DaemonWatchdogSpec{
		SocketPath:    sockPath,
		Command:       []string{"sh", "-c", "touch " + markerPath},
		CheckInterval: 30 * time.Millisecond,
		// A LIVE local unix socket dials in well under a millisecond, so a
		// generous DialTimeout costs nothing on the happy path — but a tight one
		// false-reads "dead" when a saturated CI runner starves the accept
		// goroutine past the deadline, tripping a revival this test asserts must
		// NOT happen (observed on ubuntu-latest at 50ms). 2s absorbs any realistic
		// scheduler stall while a genuinely dead socket still fails instantly
		// (ENOENT/refused, not a timeout). (hk-me8ru)
		DialTimeout:   2 * time.Second,
		MaxRevives:    1,
		ReviveBackoff: 5 * time.Millisecond,
		ReviveWindow:  50 * time.Millisecond,
	}

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if runErr := dw.Run(ctx); !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("dw.Run: want context.DeadlineExceeded, got %v", runErr)
	}

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

	runErr := dw.Run(ctx)
	if runErr == nil || errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("dw.Run: want revival-cap error, got %v", runErr)
	}
	if !strings.Contains(runErr.Error(), "revival cap reached") {
		t.Fatalf("dw.Run: want revival-cap error, got %v", runErr)
	}

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
	tmpDir := socketDir(t)
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

	fixtureDone := make(chan struct{})
	defer func() {
		cancel()
		<-fixtureDone
	}()

	go func() {
		defer close(fixtureDone)

		bindAndServe := func() (net.Listener, func(), bool) {
			ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", sockPath)
			if err != nil {
				return nil, nil, false
			}
			return ln, serveAccepts(t, ln), true
		}

		for range 3 {
			time.Sleep(60 * time.Millisecond)

			ln, joinAccepts, ok := bindAndServe()
			if !ok {
				return
			}
			time.Sleep(100 * time.Millisecond)

			if closeErr := ln.Close(); closeErr != nil {
				t.Errorf("fixture: close listener: %v", closeErr)
			}
			joinAccepts()
		}

		time.Sleep(60 * time.Millisecond)
		ln, joinAccepts, ok := bindAndServe()
		if !ok {
			return
		}
		defer func() {
			if closeErr := ln.Close(); closeErr != nil {
				t.Errorf("fixture: close listener: %v", closeErr)
			}
			joinAccepts()
		}()
		<-ctx.Done()
	}()

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	runErr := dw.Run(ctx)

	if runErr != nil && ctx.Err() == nil {
		t.Errorf("Run returned early (cap hit?): %v — counter may not be resetting", runErr)
	}

	// Count actual revive() calls (each appended a line).
	//nolint:gosec // G304: counterFile is created beneath this test's private temp directory.
	data, readErr := os.ReadFile(counterFile)
	if readErr != nil {
		t.Fatalf("read revive counter %s: %v", counterFile, readErr)
	}
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
	tmpDir := socketDir(t)
	sockPath := filepath.Join(tmpDir, "daemon.sock")
	counterFile := filepath.Join(tmpDir, "revive-count")

	spec := supervise.DaemonWatchdogSpec{
		SocketPath:    sockPath,
		Command:       []string{"sh", "-c", "echo x >> " + counterFile},
		CheckInterval: 20 * time.Millisecond,
		DialTimeout:   10 * time.Millisecond,
		MaxRevives:    5,
		ReviveBackoff: 15 * time.Millisecond,
		// ReviveWindow is set >= the ctx timeout below on purpose. That makes a
		// phantom second revive structurally impossible under any scheduling
		// delay: pollUntilAlive can only exit by detecting the bind (returns
		// true, revives reset) or by ctx cancellation (returns false) — and a
		// cancelled ctx also breaks the outer Run loop before it can tick-dead
		// and re-revive. So count==1 holds regardless of CPU saturation, instead
		// of racing a tight wall-clock window against a starved bind goroutine.
		// The happy path still exercises a bind delayed past ReviveBackoff.
		ReviveWindow: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

	fixtureDone := make(chan struct{})
	defer func() {
		cancel()
		<-fixtureDone
	}()

	go func() {
		defer close(fixtureDone)

		time.Sleep(60 * time.Millisecond)
		time.Sleep(80 * time.Millisecond)
		ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", sockPath)
		if err != nil {
			return
		}
		joinAccepts := serveAccepts(t, ln)
		defer func() {
			if closeErr := ln.Close(); closeErr != nil {
				t.Errorf("close listener: %v", closeErr)
			}
			joinAccepts()
		}()
		<-ctx.Done()
	}()

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	if runErr := dw.Run(ctx); !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("dw.Run: want context.DeadlineExceeded, got %v", runErr)
	}

	//nolint:gosec // G304: counterFile is created beneath this test's private temp directory.
	data, readErr := os.ReadFile(counterFile)
	if readErr != nil {
		t.Fatalf("read revive counter %s: %v", counterFile, readErr)
	}
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

// TestDaemonWatchdog_CrashLogCapture verifies that daemon stdout/stderr are
// written to the crash log file when CrashLogPath is configured.
func TestDaemonWatchdog_CrashLogCapture(t *testing.T) {
	tmpDir := socketDir(t)
	sockPath := filepath.Join(tmpDir, "daemon.sock")
	crashLog := filepath.Join(tmpDir, "state", "daemon.crash.log")

	spec := supervise.DaemonWatchdogSpec{
		SocketPath:    sockPath,
		Command:       []string{"sh", "-c", "echo daemon-output-sentinel"},
		CheckInterval: 30 * time.Millisecond,
		DialTimeout:   20 * time.Millisecond,
		MaxRevives:    1,
		ReviveBackoff: 5 * time.Millisecond,
		ReviveWindow:  100 * time.Millisecond,
		CrashLogPath:  crashLog,
		CrashLogKeep:  3,
	}

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runErr := dw.Run(ctx)
	if runErr == nil || errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("dw.Run: want revival-cap error, got %v", runErr)
	}
	if !strings.Contains(runErr.Error(), "revival cap reached") {
		t.Fatalf("dw.Run: want revival-cap error, got %v", runErr)
	}
	time.Sleep(200 * time.Millisecond)

	//nolint:gosec // G304: crashLog is created beneath this test's private temp directory.
	data, err := os.ReadFile(crashLog)
	if err != nil {
		t.Fatalf("crash log not created at %s: %v", crashLog, err)
	}
	if !strings.Contains(string(data), "daemon-output-sentinel") {
		t.Errorf("crash log does not contain expected sentinel; got:\n%s", string(data))
	}
}

// TestDaemonWatchdog_CrashLogRotation verifies that crash logs rotate and that
// the total number of retained logs does not exceed CrashLogKeep.
func TestDaemonWatchdog_CrashLogRotation(t *testing.T) {
	tmpDir := socketDir(t)
	sockPath := filepath.Join(tmpDir, "daemon.sock")
	crashLog := filepath.Join(tmpDir, "state", "daemon.crash.log")

	const keep = 3
	spec := supervise.DaemonWatchdogSpec{
		SocketPath:    sockPath,
		Command:       []string{"sh", "-c", "echo x"},
		CheckInterval: 20 * time.Millisecond,
		DialTimeout:   10 * time.Millisecond,
		MaxRevives:    keep + 1, // more revives than keep to exercise discard of oldest
		ReviveBackoff: 5 * time.Millisecond,
		ReviveWindow:  30 * time.Millisecond,
		CrashLogPath:  crashLog,
		CrashLogKeep:  keep,
	}

	dw := supervise.NewDaemonWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runErr := dw.Run(ctx)
	if runErr == nil || errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("dw.Run: want revival-cap error, got %v", runErr)
	}
	if !strings.Contains(runErr.Error(), "revival cap reached") {
		t.Fatalf("dw.Run: want revival-cap error, got %v", runErr)
	}
	time.Sleep(200 * time.Millisecond)

	for _, name := range []string{crashLog, crashLog + ".1", crashLog + ".2"} {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("expected crash log %s to exist: %v", name, err)
		}
	}
	if _, err := os.Stat(crashLog + ".3"); !os.IsNotExist(err) {
		t.Errorf("crash log .3 should not exist (keep=%d), but Stat returned: %v", keep, err)
	}
}
