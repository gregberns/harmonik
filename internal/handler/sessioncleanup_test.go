package handler

// sessioncleanup_test.go — coverage for the error paths that the
// "stop losing the subprocess exit error" change newly propagates, and for the
// file-descriptor cleanup those paths owe.
//
// Helper prefix: sessionCleanupFixture (per implementer-protocol.md
// §Helper-prefix discipline).
//
// Three seams are exercised here:
//
//   - abandonStartedSession — the cleanup newSessionWithIDs runs when cmd.Start
//     has already succeeded but construction still fails (a failing close of the
//     parent write ends, or a rejected Spawning→Initializing transition). Those
//     returns originally reaped the child but left the stdin write end and the
//     stdout/stderr read ends open, leaking three descriptors per occurrence.
//   - bridgeStdout — a mid-stream read failure must reach the reader as an
//     error, not as a clean EOF that reads like a well-formed but truncated
//     progress stream.
//   - newSubstrateAdapter — now returns an error rather than silently handing
//     back an adapter whose lifecycle Machine is in the wrong state.
//
// These tests are in package handler (not handler_test) because every seam
// above is unexported; the failure conditions themselves are defect-only and
// cannot be provoked through the exported constructor.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"

	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// sessionCleanupFixturePipes mirrors the descriptor set newSessionWithIDs owns
// at the moment its two post-Start failure paths return.
type sessionCleanupFixturePipes struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdoutR *os.File
	stderrR *os.File
}

// sessionCleanupFixtureStartedChild builds and starts a long-lived child wired
// exactly the way newSessionWithIDs wires one — cmd.StdinPipe plus two
// self-owned os.Pipe pairs whose write ends are handed to the child and then
// closed in the parent. It returns the three parent-side handles that
// abandonStartedSession is responsible for.
func sessionCleanupFixtureStartedChild(t *testing.T) sessionCleanupFixturePipes {
	t.Helper()

	// A bare long-lived binary, no shell: the fixture only needs a child that
	// stays alive until abandonStartedSession reaps it.
	cmd := exec.CommandContext(t.Context(), "sleep", "30")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout Pipe: %v", err)
	}
	cmd.Stdout = stdoutW
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr Pipe: %v", err)
	}
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	// The constructor closes the parent write ends before it can reach either
	// abandonStartedSession call site, so the fixture does too.
	if err := closeAll(stdoutW, stderrW); err != nil {
		t.Fatalf("close parent write ends: %v", err)
	}

	return sessionCleanupFixturePipes{cmd: cmd, stdin: stdin, stdoutR: stdoutR, stderrR: stderrR}
}

// sessionCleanupFixtureOpenFDs counts the descriptors this process currently
// holds by fstat-ing every slot below fdScanLimit: a slot that stats is open.
// Counting is used rather than the cheaper "lowest descriptor open(2) hands out
// next" probe because the test binary's descriptor table has holes, and a probe
// that lands in a hole reports no change however many descriptors leaked above
// it.
func sessionCleanupFixtureOpenFDs(t *testing.T) int {
	t.Helper()
	open := 0
	var st syscall.Stat_t
	for fd := range fdScanLimit {
		if err := syscall.Fstat(fd, &st); err == nil {
			open++
		}
	}
	return open
}

// fdScanLimit bounds the descriptor scan. Nothing in this file opens anything
// like this many descriptors, and fstat on an unused slot is a cheap EBADF, so a
// fixed ceiling is preferable to reading RLIMIT_NOFILE (which can be effectively
// unbounded).
const fdScanLimit = 4096

// sessionCleanupFixtureAssertClosed asserts that a handle is no longer usable,
// i.e. that its descriptor was released rather than leaked.
func sessionCleanupFixtureAssertClosed(t *testing.T, name string, op func() error) {
	t.Helper()
	err := op()
	if err == nil {
		t.Errorf("%s: still usable after cleanup — the descriptor was leaked", name)
		return
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("%s: want os.ErrClosed after cleanup, got %v", name, err)
	}
}

// TestAbandonStartedSession_ClosesEveryParentHandle is the regression sensor for
// the descriptor leak on newSessionWithIDs' two post-Start failure returns.
// Before the fix those returns called abandonStartedCmd alone, which reaps the
// child but closes none of the handles the constructor opened.
func TestAbandonStartedSession_ClosesEveryParentHandle(t *testing.T) {
	p := sessionCleanupFixtureStartedChild(t)

	cause := fmt.Errorf("construction failed: %w", ErrStructural)
	if err := abandonStartedSession(p.cmd, p.stdin, p.stdoutR, p.stderrR, cause); !errors.Is(err, cause) {
		t.Fatalf("abandonStartedSession must return the cause it was handed; got %v", err)
	}

	buf := make([]byte, 1)
	sessionCleanupFixtureAssertClosed(t, "stdout read end", func() error {
		_, err := p.stdoutR.Read(buf)
		return err
	})
	sessionCleanupFixtureAssertClosed(t, "stderr read end", func() error {
		_, err := p.stderrR.Read(buf)
		return err
	})
	sessionCleanupFixtureAssertClosed(t, "stdin write end", func() error {
		_, err := p.stdin.Write([]byte("x"))
		return err
	})

	// The child must also have been reaped, not merely signalled.
	if p.cmd.ProcessState == nil {
		t.Error("abandoned child was not reaped: cmd.ProcessState is nil")
	}
}

// TestAbandonStartedSession_LeaksNoDescriptors is the aggregate sensor: run the
// cleanup many times and assert the process is not holding descriptors
// afterwards. Every handle is retained in a slice for the duration of the test
// so os.File's runtime finaliser cannot quietly close a leaked descriptor and
// hide the regression — which is precisely the property that makes the leak
// dangerous in the daemon: it is bounded only by the GC, so a burst of failed
// constructions can exhaust the descriptor limit before any finaliser runs.
func TestAbandonStartedSession_LeaksNoDescriptors(t *testing.T) {
	const iterations = 15

	// Warm-up iteration first: the first child spawn can allocate descriptors
	// (runtime pipes for os/exec, /dev/null, …) that persist for the process.
	warm := sessionCleanupFixtureStartedChild(t)
	if err := abandonStartedSession(warm.cmd, warm.stdin, warm.stdoutR, warm.stderrR, nil); err != nil {
		t.Fatalf("abandonStartedSession (warm-up): unexpected cleanup error: %v", err)
	}

	before := sessionCleanupFixtureOpenFDs(t)
	retained := make([]sessionCleanupFixturePipes, 0, iterations)
	for i := range iterations {
		p := sessionCleanupFixtureStartedChild(t)
		retained = append(retained, p)
		if err := abandonStartedSession(p.cmd, p.stdin, p.stdoutR, p.stderrR, nil); err != nil {
			t.Fatalf("abandonStartedSession (iteration %d): unexpected cleanup error: %v", i, err)
		}
	}
	after := sessionCleanupFixtureOpenFDs(t)
	runtime.KeepAlive(retained)

	// Slack absorbs unrelated runtime descriptors; leaking the three handles per
	// iteration adds ~45.
	const slack = 4
	if after-before > slack {
		t.Errorf("open descriptors grew by %d over %d abandoned sessions (before=%d after=%d) — the construction path is leaking handles",
			after-before, iterations, before, after)
	}
}

// TestNewSession_StartFailure_IsStructural pins the pre-existing cmd.Start
// failure path that the two new post-Start returns are modelled on: it hands
// back no Session and a structural error, so daemon-level routing can classify
// it. This is the only construction failure reachable through the exported
// constructor, and it is the baseline the new paths must match.
func TestNewSession_StartFailure_IsStructural(t *testing.T) {
	sess, err := NewSession(t.Context(), exec.CommandContext(t.Context(), "/nonexistent/harmonik-test-binary"))
	if err == nil {
		t.Fatal("NewSession with a nonexistent binary: want an error, got nil")
	}
	if sess != nil {
		t.Error("NewSession returned a non-nil Session alongside an error")
	}
	if !errors.Is(err, ErrStructural) {
		t.Fatalf("NewSession start failure: want ErrStructural, got %v", err)
	}
}

// TestCloseAll_JoinsFailuresAndSkipsNil pins the cleanup helper both failure
// paths report through: nil closers are skipped, successful closes contribute
// nothing, and every failure is joined so none is silently dropped.
func TestCloseAll_JoinsFailuresAndSkipsNil(t *testing.T) {
	if err := closeAll(nil, nil); err != nil {
		t.Errorf("closeAll(nil, nil) = %v, want nil", err)
	}

	first := errors.New("first close failed")
	second := errors.New("second close failed")
	err := closeAll(
		sessionCleanupFixtureCloser{},
		nil,
		sessionCleanupFixtureCloser{err: first},
		sessionCleanupFixtureCloser{err: second},
	)
	if !errors.Is(err, first) {
		t.Errorf("closeAll dropped the first failure: %v", err)
	}
	if !errors.Is(err, second) {
		t.Errorf("closeAll dropped the second failure: %v", err)
	}

	ok := sessionCleanupFixtureCloser{}
	if err := closeAll(ok, ok); err != nil {
		t.Errorf("closeAll over successful closers = %v, want nil", err)
	}
}

// sessionCleanupFixtureCloser is an io.Closer returning a fixed error.
type sessionCleanupFixtureCloser struct{ err error }

func (c sessionCleanupFixtureCloser) Close() error { return c.err }

// TestBridgeStdout_ReadFailureReachesReader verifies that a mid-stream read
// failure on the subprocess stdout pipe is delivered to the caller as an error.
// The bridge used to close its io.Pipe cleanly on any copy failure, so the
// watcher saw a well-formed-but-truncated progress stream and reported success
// on a stream that had in fact been cut short.
func TestBridgeStdout_ReadFailureReachesReader(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := pw.Close(); closeErr != nil {
			t.Errorf("closing the bridge source write end: %v", closeErr)
		}
	})

	bridged := bridgeStdout(pr)

	// The OS pipe buffer absorbs this write immediately; the bridge's io.Copy
	// picks it up and blocks writing it into the io.Pipe until the read below.
	if _, err := pw.WriteString("partial"); err != nil {
		t.Fatalf("writing the first chunk into the bridge source: %v", err)
	}

	buf := make([]byte, len("partial"))
	if _, err := io.ReadFull(bridged, buf); err != nil {
		t.Fatalf("reading the first chunk through the bridge: %v", err)
	}
	if string(buf) != "partial" {
		t.Fatalf("first chunk = %q, want %q", buf, "partial")
	}

	// Break the source mid-stream: the bridge's io.Copy read now fails.
	if err := pr.Close(); err != nil {
		t.Fatalf("closing the bridge source: %v", err)
	}

	_, readErr := bridged.Read(buf)
	if readErr == nil {
		t.Fatal("read after a mid-stream source failure: got nil error, want the read failure")
	}
	if errors.Is(readErr, io.EOF) {
		t.Fatalf("read after a mid-stream source failure surfaced as a clean EOF: %v", readErr)
	}
	if !errors.Is(readErr, os.ErrClosed) {
		t.Errorf("read error should wrap the underlying failure; got %v", readErr)
	}
	if !strings.Contains(readErr.Error(), "stdout bridge") {
		t.Errorf("read error should name the bridge; got %v", readErr)
	}
}

// TestBridgeStdout_CleanEOFStaysClean is the companion sensor: a source that
// ends normally must still present a plain io.EOF, not a synthesised error.
func TestBridgeStdout_CleanEOFStaysClean(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	bridged := bridgeStdout(pr)

	if _, err := pw.WriteString("hello"); err != nil {
		t.Fatalf("writing into the bridge source: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("closing the bridge source write end: %v", err)
	}

	got, err := io.ReadAll(bridged)
	if err != nil {
		t.Fatalf("io.ReadAll through the bridge: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("bridged bytes = %q, want %q", got, "hello")
	}
	if _, err := bridged.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("read past a clean end = %v, want io.EOF", err)
	}
}

// TestNewSubstrateAdapter_InitialisesMachine covers newSubstrateAdapter's new
// (adapter, error) signature: the success path reports no error and leaves the
// machine in StateInitializing, which is what the propagated error exists to
// guarantee.
func TestNewSubstrateAdapter_InitialisesMachine(t *testing.T) {
	a, err := newSubstrateAdapter(fakeSubSession{}, "sess-42", "run-7")
	if err != nil {
		t.Fatalf("newSubstrateAdapter: %v", err)
	}
	if a == nil {
		t.Fatal("newSubstrateAdapter returned a nil adapter with a nil error")
	}
	m := a.Machine()
	if m == nil {
		t.Fatal("adapter Machine() is nil")
	}
	if got := m.Current(); got != hclifecycle.StateInitializing {
		t.Errorf("machine state = %v, want %v", got, hclifecycle.StateInitializing)
	}
	if got := m.SessionID(); got != "sess-42" {
		t.Errorf("machine session id = %q, want %q", got, "sess-42")
	}
	if got := m.RunID(); got != "run-7" {
		t.Errorf("machine run id = %q, want %q", got, "run-7")
	}
}

// TestNewSubstrateAdapter_DefaultsBlankIDs pins the placeholder identifiers so
// the error return cannot be confused with the blank-ID case.
func TestNewSubstrateAdapter_DefaultsBlankIDs(t *testing.T) {
	a, err := newSubstrateAdapter(fakeSubSession{}, "", "")
	if err != nil {
		t.Fatalf("newSubstrateAdapter: %v", err)
	}
	if got := a.Machine().SessionID(); got != "substrate-unknown" {
		t.Errorf("blank session id defaulted to %q, want %q", got, "substrate-unknown")
	}
	if got := a.Machine().RunID(); got != "unknown" {
		t.Errorf("blank run id defaulted to %q, want %q", got, "unknown")
	}
}

// TestNewSubstrateAdapter_ErrorPathGuardsTheStateTable pins the invariant the
// new error return exists to catch. newSubstrateAdapter always builds a machine
// in StateSpawning, so its error is reachable only if the state table stops
// accepting Spawning→Initializing — at which point every substrate launch would
// fail construction. This sensor names that consequence.
func TestNewSubstrateAdapter_ErrorPathGuardsTheStateTable(t *testing.T) {
	m := hclifecycle.New("sess-1", "run-1")
	if got := m.Current(); got != hclifecycle.StateSpawning {
		t.Fatalf("a fresh machine starts in %v, want %v", got, hclifecycle.StateSpawning)
	}
	if err := m.Transition(hclifecycle.StateInitializing, hclifecycle.ReasonSpawnStarted, "", ""); err != nil {
		t.Fatalf("Spawning→Initializing must stay valid or newSubstrateAdapter fails every substrate launch: %v", err)
	}
}
