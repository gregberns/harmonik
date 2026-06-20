//go:build integration && darwin

package keeper

// cycle_operator_attached_integration_test.go — REAL-tmux integration test
// (build tags: integration && darwin) for the operator-attached guard (hk-6qf).
//
// # Why this file exists
//
// The -short unit tests (cycle_operator_attached_test.go) exercise the act-path
// guard via a FAKE OperatorAttachedFn, so the production probe — OperatorAttached
// shelling out to `tmux list-clients -t <target>` and interpreting its output —
// is unexercised there. This file drives the REAL OperatorAttached against a live
// tmux server with a REAL client attached and detached, mirroring the convention
// in tmuxresolve_integration_test.go (hk-2ojne).
//
// # How a real client is attached without a controlling terminal
//
// `tmux attach-session` needs a tty. The test allocates a pseudo-terminal via the
// stdlib (no external pty dependency — the keeper package's depguard allows only
// $gostd + core + eventbus + self), points `tmux attach-session`'s std fds at the
// pty slave with Setsid+Setctty, and lets a real client attach. Killing that
// process detaches the client. The pty ioctls (TIOCPTYGRANT/UNLK/GNAME) are
// darwin-specific, hence the `darwin` build constraint; the test is meaningless
// without them.
//
// # Safety contract (load-bearing)
//
// This test creates and destroys ONLY its own uniquely-named throwaway tmux
// session (name derived from a random suffix with an "oa6qf-test-" prefix that no
// harmonik machinery ever produces). Teardown kills BY EXACT NAME — there is NO
// kill-server, NO glob/pattern kill, and NO list-and-kill. It can never touch
// hk-daemon-supervise, harmonik-*, *-flywheel, or any pre-existing session. If
// tmux is not on PATH or the pty cannot be allocated the test t.Skip()s.
//
// Run with:
//
//	go test -tags=integration -run TestIntegration_OperatorAttached ./internal/keeper/...
//
// Bead: hk-6qf. Helper prefix: oai (operator-attached-integration).

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

// darwin /dev/ptmx ioctl request codes (sys/ttycom.h). Used to grant, unlock,
// and resolve the slave name of a freshly-opened master pty.
const (
	oaiTIOCPTYGRANT = 0x20007454 // TIOCPTYGRANT
	oaiTIOCPTYUNLK  = 0x20007452 // TIOCPTYUNLK
	oaiTIOCPTYGNAME = 0x40807453 // TIOCPTYGNAME
)

// oaiRequireTmux skips the test when tmux is not installed.
func oaiRequireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("oai: tmux not found on PATH; skipping real-tmux integration test")
	}
}

// oaiUniqueSessionName returns a throwaway session name guaranteed not to collide
// with any real harmonik/captain/crew session.
func oaiUniqueSessionName(t *testing.T) string {
	t.Helper()
	//nolint:gosec // G404: test-local session-name uniqueness, no security relevance
	return fmt.Sprintf("oa6qf-test-%d-%d", rand.Int64(), rand.Int64())
}

// oaiStartSession creates a detached tmux session by EXACT name and registers a
// t.Cleanup that kills THAT session (and only that session) by name.
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

// oaiOpenPTY allocates a master/slave pty pair via the darwin stdlib ioctl path
// and returns the open master file and the slave device path. It t.Skip()s the
// test if any step fails — pty allocation may be denied in sandboxed CI.
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

// oaiAttachClient starts a real `tmux attach-session -t name` client connected to
// a fresh pty and returns a detach func. The client appears in
// `tmux list-clients -t name` until detach is called.
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
	// The child holds the slave as its controlling tty; this side can close it.
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
	// Safety net so a failing test never leaves an attach client around.
	t.Cleanup(detach)
	return detach
}

// oaiWaitClients polls `tmux list-clients -t name` until OperatorAttached matches
// want, or the deadline elapses. Returns the final OperatorAttached reading.
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

	// (0) No session yet → OperatorAttached must be false (fail-open).
	if OperatorAttached(name) {
		t.Fatalf("oai: OperatorAttached(%q) true before the session exists", name)
	}

	oaiStartSession(t, name)

	// (1) Session exists but no client attached → false.
	if oaiWaitClients(name, false, 2*time.Second) {
		t.Fatalf("oai: OperatorAttached(%q) true with no client attached", name)
	}

	// (2) Attach a REAL client via a pty → true.
	detach := oaiAttachClient(t, name)
	if !oaiWaitClients(name, true, 3*time.Second) {
		t.Fatalf("oai: OperatorAttached(%q) false while a real client IS attached", name)
	}

	// (3) Detach → false again (live → absent client transition).
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

	// Handoff fake returns the nonce immediately; gauge flips to newSID after the
	// /clear so the cycle can complete on the detached run.
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

	cfg := CyclerConfig{
		AgentName:         agent,
		ProjectDir:        t.TempDir(),
		TmuxTarget:        name, // REAL session — OperatorAttached probes it for real.
		ActPct:            90.0,
		WarnPct:           80.0,
		HandoffTimeout:    1 * time.Second,
		ClearSettle:       200 * time.Millisecond,
		PollInterval:      10 * time.Millisecond,
		CycleIDGen:        func() string { return cycleID },
		IsManagedFn:       func(_, _ string) bool { return true },
		HandoffFilePath:   func(_, a string) string { return "/tmp/HANDOFF-" + a + ".md" },
		ReadHandoff:       readHandoff,
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          injectSpy,
		ReadGaugeFn:       readGauge,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    func(_ string, _ *CycleJournal) error { return nil },
		AppendHandoffFn:   func(_, _ string) error { return nil },
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
		// OperatorAttachedFn left nil → real OperatorAttached (tmux list-clients).
	}
	cycler := NewCycler(cfg, em)

	// Attach a REAL client and confirm the probe sees it before driving the cycle.
	detach := oaiAttachClient(t, name)
	if !oaiWaitClients(name, true, 3*time.Second) {
		t.Fatalf("oai: client not visible to OperatorAttached(%q) before suppress assertion", name)
	}

	// (1) Attached → MaybeRun must SUPPRESS: the destructive /clear injection is
	// withheld and the handoff never starts. (We no longer assert an
	// operator_attached event — emitOperatorAttached is a no-op since f46ad0bf /
	// hk-ubp1; see the function doc above. The real, load-bearing signal is the
	// *absence of injection and handoff* while the operator is attached.)
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

	// (2) Detach the real client; wait until the probe reports no client.
	detach()
	if oaiWaitClients(name, false, 3*time.Second) {
		t.Fatalf("oai: client still attached after detach(); cannot test resume")
	}

	// (3) Detached → MaybeRun proceeds and completes the cycle.
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
