//go:build integration && darwin

package keeper

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/gregberns/harmonik/internal/core"
)

const (
	oaiTIOCPTYGRANT = 0x20007454 // TIOCPTYGRANT
	oaiTIOCPTYUNLK  = 0x20007452 // TIOCPTYUNLK
	oaiTIOCPTYGNAME = 0x40807453 // TIOCPTYGNAME
)

func oaiRequireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("oai: tmux not found on PATH; skipping real-tmux integration test")
	}
}

func oaiUniqueSessionName(t *testing.T) string {
	t.Helper()
	//nolint:gosec // G404: test-local session-name uniqueness, no security relevance
	return fmt.Sprintf("oa6qf-test-%d-%d", rand.Int64(), rand.Int64())
}

func oaiStartSession(t *testing.T, name string) {
	t.Helper()
	if out, err := exec.CommandContext(context.Background(), "tmux", "new-session", "-d", "-s", name, "sleep", "300").CombinedOutput(); err != nil {
		t.Fatalf("oai: failed to create throwaway session %q: %v (output: %s)", name, err, out)
	}
	t.Cleanup(func() {
		out, err := exec.CommandContext(context.Background(), "tmux", "kill-session", "-t", name).CombinedOutput()
		if err != nil {
			t.Logf("oai cleanup: kill-session %q returned %v (output: %s) — likely already gone", name, err, out)
		}
	})
}

func oaiOpenPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("oai: cannot open /dev/ptmx (%v); skipping", err)
	}
	fd := master.Fd()
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, oaiTIOCPTYGRANT, 0); e != 0 {
		_ = master.Close()
		t.Skipf("oai: TIOCPTYGRANT failed (%v); skipping", e)
	}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, oaiTIOCPTYUNLK, 0); e != 0 {
		_ = master.Close()
		t.Skipf("oai: TIOCPTYUNLK failed (%v); skipping", e)
	}
	var buf [128]byte
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, oaiTIOCPTYGNAME, uintptr(unsafe.Pointer(&buf[0]))); e != 0 {
		_ = master.Close()
		t.Skipf("oai: TIOCPTYGNAME failed (%v); skipping", e)
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return master, string(buf[:n])
}

func oaiAttachClient(t *testing.T, name string) (detach func()) {
	t.Helper()

	master, slaveName := oaiOpenPTY(t)
	slave, err := os.OpenFile(slaveName, os.O_RDWR, 0)
	if err != nil {
		_ = master.Close()
		t.Skipf("oai: cannot open pty slave %q (%v); skipping", slaveName, err)
	}

	cmd := exec.Command("tmux", "attach-session", "-t", name)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		_ = slave.Close()
		_ = master.Close()
		t.Fatalf("oai: failed to start tmux attach-session: %v", err)
	}
	_ = slave.Close()

	detached := false
	detach = func() {
		if detached {
			return
		}
		detached = true
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = master.Close()
	}
	t.Cleanup(detach)
	return detach
}

func oaiWaitClients(name string, want bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		got := OperatorAttached(name)
		if got == want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestIntegration_OperatorAttached_RealClient exercises the REAL OperatorAttached
// probe end-to-end: absent client → false; attached client → true; detached →
// false.
func TestIntegration_OperatorAttached_RealClient(t *testing.T) {
	oaiRequireTmux(t)

	name := oaiUniqueSessionName(t)

	if OperatorAttached(name) {
		t.Fatalf("oai: OperatorAttached(%q) true before the session exists", name)
	}

	oaiStartSession(t, name)

	if oaiWaitClients(name, false, 2*time.Second) {
		t.Fatalf("oai: OperatorAttached(%q) true with no client attached", name)
	}

	detach := oaiAttachClient(t, name)
	if !oaiWaitClients(name, true, 3*time.Second) {
		t.Fatalf("oai: OperatorAttached(%q) false while a real client IS attached", name)
	}

	detach()
	if oaiWaitClients(name, false, 3*time.Second) {
		t.Fatalf("oai: OperatorAttached(%q) still true after the client detached", name)
	}
}

// TestIntegration_OperatorAttached_SuppressesAndResumes drives the FULL act-path
// (Cycler.MaybeRun) against the real tmux probe: while a real client is attached
// the cycle is SUPPRESSED (warn-only, NO destructive /clear injection); after the
// client detaches the cycle proceeds and completes.
//
// Note: this test no longer asserts a session_keeper_operator_attached event.
// That event was deliberately dropped — emitOperatorAttached was made a no-op on
// 2026-06-17 (commit f46ad0bf, hk-ubp1 monitor-noise cut); the event is no longer
// persisted to events.jsonl. The behavior that MATTERS — and is asserted here —
// is the *effect* of the operator-attached guard: zero injections while attached,
// and a completed cycle after detach. Refs: hk-6qf, hk-ubp1, f46ad0bf.
//
// InjectFn is a spy (we do NOT paste into the real pane — the point is to assert
// suppression vs. proceed, not to drive a real Claude REPL). Only OperatorAttached
// and the tmux session are real.
func TestIntegration_OperatorAttached_SuppressesAndResumes(t *testing.T) {
	oaiRequireTmux(t)

	const (
		agent   = "oa6qf-cycle-agent"
		cycleID = "cyc-oa6qf-int"
		prevSID = "sess-oa6qf-before"
		newSID  = "sess-oa6qf-after"
	)

	name := oaiUniqueSessionName(t)
	oaiStartSession(t, name)

	em := &RecordingEmitter{}

	var injectMu struct {
		texts []string
		ch    chan struct{}
	}
	injectMu.ch = make(chan struct{}, 8)
	injectSpy := func(_ context.Context, _, text string) error {
		injectMu.texts = append(injectMu.texts, text)
		return nil
	}
	injectCount := func() int { return len(injectMu.texts) }

	nonce := nonceMarker(cycleID)
	readHandoff := func(_ string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }
	var gaugeCalls int
	readGauge := func(_, _ string) (*CtxFile, time.Time, error) {
		gaugeCalls++
		sid := prevSID
		if gaugeCalls > 1 {
			sid = newSID
		}
		return &CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	overrides := configTestOverrides{
		cycleIDs: func() string { return cycleID },
		path:     func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		read:     readHandoff, scrub: func(_ string) error { return nil },
		inject: injectSpy, gauge: readGauge,
		journal: func(_ string, _ *CycleJournal) error { return nil },
	}
	cfg := CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     name, // REAL session — OperatorAttached probes it for real.
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 1 * time.Second,
		ClearSettle:    200 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	}
	cycler := mustNewCyclerWithConfigOverrides(cfg, em, overrides)

	detach := oaiAttachClient(t, name)
	if !oaiWaitClients(name, true, 3*time.Second) {
		t.Fatalf("oai: client not visible to OperatorAttached(%q) before suppress assertion", name)
	}

	if err := cycler.MaybeRun(context.Background(), &CtxFile{Pct: 95.0, SessionID: prevSID}); err != nil {
		t.Fatalf("MaybeRun(attached): %v", err)
	}
	if n := injectCount(); n != 0 {
		t.Fatalf("oai: want 0 injections while operator attached; got %d (%v)", n, injectMu.texts)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); got != 0 {
		t.Fatalf("oai: want 0 handoff_started while attached; got %d", got)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); got != 0 {
		t.Fatalf("oai: want 0 cycle_complete while attached (cycle must be suppressed); got %d", got)
	}

	detach()
	if oaiWaitClients(name, false, 3*time.Second) {
		t.Fatalf("oai: client still attached after detach(); cannot test resume")
	}

	if err := cycler.MaybeRun(context.Background(), &CtxFile{Pct: 95.0, SessionID: prevSID}); err != nil {
		t.Fatalf("MaybeRun(detached): %v", err)
	}
	if n := injectCount(); n < 3 {
		t.Fatalf("oai: want >=3 injections after detach (handoff/clear/resume); got %d (%v)", n, injectMu.texts)
	}
	if got := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); got != 1 {
		t.Fatalf("oai: want 1 cycle_complete after detach; got %d", got)
	}
}
