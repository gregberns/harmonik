package supervise_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/supervise"
)

// writePidfile writes pid to path.
func writePidfile(t *testing.T, path string, pid int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSupervisorWatchdog_NoAlarmWhenAlive verifies that no alarm fires and
// no revival is triggered when the supervisor PID is live.
func TestSupervisorWatchdog_NoAlarmWhenAlive(t *testing.T) {
	tmpDir := t.TempDir()
	pidfile := filepath.Join(tmpDir, "supervisor.pid")
	markerPath := filepath.Join(tmpDir, "revived")

	// Write our own PID — guaranteed alive.
	writePidfile(t, pidfile, os.Getpid())

	alarmFired := false
	spec := supervise.SupervisorWatchdogSpec{
		PidfilePath:   pidfile,
		ReviveCmd:     []string{"sh", "-c", "touch " + markerPath},
		CheckInterval: 30 * time.Millisecond,
		MaxRevives:    1,
		ReviveBackoff: 5 * time.Millisecond,
		ReviveWindow:  50 * time.Millisecond,
		OnAlarm:       func() { alarmFired = true },
	}

	sw := supervise.NewSupervisorWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_ = sw.Run(ctx) // exits on ctx timeout

	if alarmFired {
		t.Error("supervisor was alive but alarm fired")
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("supervisor was alive but revival was triggered")
	}
}

// TestSupervisorWatchdog_AlarmsWhenDead verifies that when the pidfile
// records a dead PID, the alarm fires and the revival command is spawned.
func TestSupervisorWatchdog_AlarmsWhenDead(t *testing.T) {
	tmpDir := t.TempDir()
	pidfile := filepath.Join(tmpDir, "supervisor.pid")
	markerPath := filepath.Join(tmpDir, "revived")

	// Write a PID that is guaranteed dead: PID 1 is not ours; use a known-dead
	// PID from a short-lived child.
	cmd := mustStartAndWait(t)
	deadPID := cmd
	writePidfile(t, pidfile, deadPID)

	alarmCount := 0
	spec := supervise.SupervisorWatchdogSpec{
		PidfilePath:   pidfile,
		ReviveCmd:     []string{"sh", "-c", "touch " + markerPath},
		CheckInterval: 30 * time.Millisecond,
		MaxRevives:    1,
		ReviveBackoff: 10 * time.Millisecond,
		ReviveWindow:  100 * time.Millisecond,
		OnAlarm:       func() { alarmCount++ },
	}

	sw := supervise.NewSupervisorWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = sw.Run(ctx) // exits when revival cap reached

	if alarmCount == 0 {
		t.Error("expected alarm to fire for dead PID, but OnAlarm was never called")
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("expected revival command to be spawned, but marker file was not created")
	}
}

// TestSupervisorWatchdog_AlarmsWhenNoPidfile verifies that when the pidfile
// is absent, the alarm fires (supervisor not running / never started).
func TestSupervisorWatchdog_AlarmsWhenNoPidfile(t *testing.T) {
	tmpDir := t.TempDir()
	pidfile := filepath.Join(tmpDir, "supervisor.pid") // intentionally absent

	alarmFired := false
	spec := supervise.SupervisorWatchdogSpec{
		PidfilePath:   pidfile,
		CheckInterval: 20 * time.Millisecond,
		MaxRevives:    -1, // unlimited — exit only via ctx timeout
		ReviveBackoff: 5 * time.Millisecond,
		ReviveWindow:  30 * time.Millisecond,
		OnAlarm:       func() { alarmFired = true },
	}

	sw := supervise.NewSupervisorWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = sw.Run(ctx)

	if !alarmFired {
		t.Error("expected alarm to fire when pidfile absent, but OnAlarm was never called")
	}
}

// TestSupervisorWatchdog_StopsOnContextCancel verifies that Run returns promptly
// when the context is cancelled before any tick fires.
func TestSupervisorWatchdog_StopsOnContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	pidfile := filepath.Join(tmpDir, "supervisor.pid")

	spec := supervise.SupervisorWatchdogSpec{
		PidfilePath:   pidfile,
		CheckInterval: 10 * time.Second,
		MaxRevives:    1,
		ReviveBackoff: 5 * time.Millisecond,
		ReviveWindow:  50 * time.Millisecond,
	}

	sw := supervise.NewSupervisorWatchdog(spec, silentLogger())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sw.Run(ctx) }()

	cancel()

	select {
	case <-done:
		// OK — watchdog exited promptly
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s after context cancellation")
	}
}

// TestSupervisorWatchdog_CrashLoopGuard verifies that Run returns an error
// after MaxRevives revival attempts when the supervisor pidfile stays absent.
func TestSupervisorWatchdog_CrashLoopGuard(t *testing.T) {
	tmpDir := t.TempDir()
	pidfile := filepath.Join(tmpDir, "supervisor.pid") // intentionally absent

	spec := supervise.SupervisorWatchdogSpec{
		PidfilePath:   pidfile,
		ReviveCmd:     []string{"true"}, // exits immediately; pidfile stays absent
		CheckInterval: 20 * time.Millisecond,
		MaxRevives:    2,
		ReviveBackoff: 5 * time.Millisecond,
		ReviveWindow:  30 * time.Millisecond,
	}

	sw := supervise.NewSupervisorWatchdog(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := sw.Run(ctx)
	if err == nil {
		t.Fatal("expected crash-loop error, got nil")
	}
}

// TestSupervisorWatchdog_ReviveCounterResets verifies that after a successful
// revival the counter resets, allowing additional revivals later.
func TestSupervisorWatchdog_ReviveCounterResets(t *testing.T) {
	tmpDir := t.TempDir()
	pidfile := filepath.Join(tmpDir, "supervisor.pid")
	counterFile := filepath.Join(tmpDir, "revive-count")

	spec := supervise.SupervisorWatchdogSpec{
		PidfilePath:   pidfile,
		ReviveCmd:     []string{"sh", "-c", "echo x >> " + counterFile},
		CheckInterval: 20 * time.Millisecond,
		MaxRevives:    2, // without counter reset, a 3rd revive would be blocked
		ReviveBackoff: 5 * time.Millisecond,
		ReviveWindow:  150 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Goroutine: cycles the pidfile (absent → live → absent → live → absent → live)
	// each cycle: let the watchdog see the absence and call revive, then bring it
	// back by writing a live PID so pollUntilAlive resets the counter.
	go func() {
		for range 3 {
			// Let watchdog detect absent pidfile and call revive().
			time.Sleep(60 * time.Millisecond)
			// Write our own PID so pollUntilAlive succeeds.
			_ = os.WriteFile(pidfile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
			time.Sleep(100 * time.Millisecond)
			// Remove pidfile to trigger the next revive cycle.
			_ = os.Remove(pidfile)
		}
		// Final recovery: write live PID and hold until ctx expires.
		time.Sleep(60 * time.Millisecond)
		_ = os.WriteFile(pidfile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
		<-ctx.Done()
	}()

	sw := supervise.NewSupervisorWatchdog(spec, silentLogger())
	runErr := sw.Run(ctx)

	if runErr != nil && ctx.Err() == nil {
		t.Errorf("Run returned early (cap hit?): %v — counter may not be resetting", runErr)
	}

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

// mustStartAndWait starts a short-lived child and waits for it to exit,
// returning its PID which is now guaranteed dead.
func mustStartAndWait(t *testing.T) int {
	t.Helper()
	cmd := &os.ProcessState{}
	_ = cmd // avoid unused import

	proc, err := os.StartProcess("/bin/sh", []string{"/bin/sh", "-c", "exit 0"}, &os.ProcAttr{})
	if err != nil {
		t.Fatalf("mustStartAndWait: start: %v", err)
	}
	pid := proc.Pid
	if _, err := proc.Wait(); err != nil {
		t.Logf("mustStartAndWait: wait: %v", err) // non-fatal
	}
	return pid
}
