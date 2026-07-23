// Package handler — Session wrapping exec.Cmd + WaitOwner (MVH_ROADMAP row #6).
//
// Session is the daemon's handle on a running subprocess (Claude Code or twin).
// It owns stdin write, stdout/stderr Reader exposure for the row-#7 watcher
// wire-up, signal-based termination, and reap via lifecycle.WaitOwner.
//
// # Composition with WaitOwner
//
// Exactly one goroutine (the one running runWait in the background) calls
// lifecycle.WaitOwner.WaitAndReap, and therefore cmd.Wait. This is the PL-014
// single-owner discipline. Callers that need the exit status receive it via
// Session.Wait — they never call cmd.Wait directly.
//
// WaitOwner caches the exit error and re-delivers it to every caller of its
// Wait, so Session.Wait can simply forward it; it blocks on outcomeDone first
// so Outcome() is fully populated by the time Wait returns. Kill needs a
// select-able exit edge rather than a blocking wait, and takes that from
// WaitOwner.Done() — reading Done never consumes the exit error (hk-qun49).
//
// # stdout/stderr exposure
//
// Session exposes Stdout() and Stderr() io.Reader values so row-#7's
// Handler.Launch can wire handlercontract.SpawnWatcher to the subprocess
// stdout pipe without duplicating pipe setup logic.
//
// Cite: MVH_ROADMAP.md row #6; specs/handler-contract.md §4.5, §4.6.
package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// closeAll closes every non-nil closer and returns the joined close errors, or
// nil when all closes succeeded. Used on NewSession's failure paths so a close
// failure during cleanup is reported alongside the failure that triggered it
// rather than being discarded.
func closeAll(closers ...io.Closer) error {
	errs := make([]error, 0, len(closers))
	for _, c := range closers {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Session is the daemon's handle on a running subprocess (Claude Code or twin).
//
// Acquire via NewSession; NewSession takes ownership of the *exec.Cmd and calls
// cmd.Start.  After NewSession returns, callers MUST NOT call cmd.Start,
// cmd.Wait, or modify cmd in any way — those are owned by Session.
//
// Thread safety: all exported methods are safe to call concurrently.
type Session interface {
	// SendInput writes line + '\n' to the child's stdin.
	// Returns an error if the subprocess has already exited or the write fails.
	SendInput(ctx context.Context, line string) error

	// Kill sends SIGTERM to the subprocess (positive PID, not the process group
	// — see the implementation note on session.Kill). If ctx expires before
	// Wait returns, it escalates to SIGKILL.
	//
	// Kill returns once the termination signal has been sent (not once the
	// subprocess exits).  Use Wait to block until the process is reaped.  Note
	// that Kill targets only the immediate handler process; grandchildren it
	// forked may survive briefly and are bounded by the caller's post-kill wait
	// and the daemon's orphan sweep.
	Kill(ctx context.Context) error

	// Wait blocks until the subprocess exits and has been reaped, then returns
	// the exit error (an *exec.ExitError for a non-zero or signalled exit, nil
	// for a clean one). Only one goroutine ever calls cmd.Wait per PL-014/PL-016.
	//
	// Every call returns the same error, and calls from different goroutines do
	// not race for it.
	Wait(ctx context.Context) error

	// Outcome returns exit metadata populated once Wait returns.  Calling
	// Outcome before Wait returns a zero-value Outcome.
	Outcome() Outcome

	// Stdout returns the io.Reader attached to the subprocess stdout pipe.
	// Callers MUST NOT read from Stdout after passing it to SpawnWatcher.
	// The pipe is opened by NewSession before cmd.Start; it is valid until the
	// subprocess exits and its stdout is EOF-drained.
	Stdout() io.Reader

	// Stderr returns the io.Reader attached to the subprocess stderr pipe.
	// Session captures up to ~4 KiB of stderr tail for Outcome.StderrTail; the
	// caller MAY also read from Stderr, but only before Wait returns.
	Stderr() io.Reader

	// CloseStdin closes the write end of the subprocess stdin pipe, signalling
	// EOF to the subprocess. Callers MUST call CloseStdin after delivering the
	// LaunchSpec JSON to stdin (HC-005) so the subprocess can detect
	// end-of-input. Calling CloseStdin more than once is safe (subsequent calls
	// return nil because the underlying pipe is already closed).
	CloseStdin() error

	// Machine returns the per-session lifecycle FSM (handler-contract.md §4.13
	// HC-064..HC-067). The machine starts in StateSpawning (set by NewSession on
	// successful cmd.Start) and transitions as the session progresses. Callers
	// MAY call Machine.Transition and Machine.RecordActivity; the watcher and
	// workloop own the canonical transition calls.
	//
	// Returns non-nil for all valid sessions.
	Machine() *hclifecycle.Machine
}

// Outcome carries exit metadata for a completed session.  Populated exactly
// once when Wait returns.
type Outcome struct {
	// ExitCode is the process exit code.  0 on clean exit, -1 if the process
	// was killed by a signal and the exit code is unavailable.
	ExitCode int

	// Signal is the signal that terminated the process, or -1 if the process
	// exited cleanly (no signal).
	Signal syscall.Signal

	// Duration is the wall-clock time from cmd.Start to cmd.Wait return.
	Duration time.Duration

	// StderrTail holds the last up to stderrRingCapBytes of stderr output.
	// Useful for surfacing error context when the subprocess exits non-zero.
	StderrTail []byte
}

// stderrRingCapBytes is the maximum number of stderr bytes captured for
// Outcome.StderrTail.
const stderrRingCapBytes = 4 * 1024 // 4 KiB

// stderrDrainGrace bounds how long runWait waits for the stderr drain goroutine
// to finish after the subprocess is reaped, before it force-closes the stderr
// read-end to unblock the drain.
//
// On normal exit the drain reaches EOF immediately (all fd-holders are gone), so
// this grace never elapses and stderr capture is unaffected.  It only matters on
// the cancel/kill path when a surviving grandchild keeps the stderr write-end
// open: without the bound, sess.Wait would wedge for the grandchild's full
// runtime (the hk-4c7kw slow-shutdown bug).  500 ms is generous headroom over
// the sub-millisecond normal drain while keeping shutdown well under the 10 s T3
// budget.
//
// Bead: hk-4c7kw.
const stderrDrainGrace = 500 * time.Millisecond

// session is the concrete implementation of Session.
type session struct {
	cmd       *exec.Cmd
	waitOwner *lifecycle.WaitOwner

	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader

	startedAt time.Time

	// outcome is populated atomically once wait goroutine completes.
	outcomeReady atomic.Bool
	outcome      Outcome

	// stderrBuf accumulates stderr bytes for the ring buffer.
	stderrBuf *ringBuffer

	// stderrDone is closed by the drainStderr goroutine when it finishes.
	// runWait waits for this before reading stderrBuf.Bytes() to avoid a race
	// between concurrent Write and Bytes calls on the unsynchronized ringBuffer.
	stderrDone chan struct{}

	// outcomeDone is closed by runWait after outcome is fully populated.
	// Wait() blocks on this so callers see a consistent Outcome() immediately
	// after Wait() returns.
	outcomeDone chan struct{}

	// machine is the per-session lifecycle FSM (HC-064..HC-067).
	// Constructed in NewSession and transitions to StateSpawning→StateInitializing
	// on successful cmd.Start. The Machine() accessor exposes it to the watcher
	// and workloop for subsequent transitions.
	machine *hclifecycle.Machine
}

// NewSession opens stdin/stdout/stderr pipes on cmd, starts the subprocess, and
// returns a Session that owns the lifecycle.
//
// The caller MUST have fully configured cmd (args, env, dir, SysProcAttr via
// lifecycle.SpawnChildSysProcAttr) before calling NewSession.  NewSession calls
// cmd.Start; callers MUST NOT call cmd.Start themselves.
//
// NewSession launches a background goroutine to drain stderr into the ring
// buffer and another to call WaitOwner.WaitAndReap once stdin/stdout EOF — the
// caller drives termination via Kill and observes completion via Wait.
//
// The per-session lifecycle Machine (HC-064) starts in StateSpawning (the
// Machine.New default). After cmd.Start succeeds, NewSession transitions the
// machine to StateInitializing (HC-065: "Spawning→Initializing on subprocess
// started"). The caller obtains the machine via Session.Machine().
//
// Returns ErrStructural-wrapping error on pipe-setup or start failure.
func NewSession(ctx context.Context, cmd *exec.Cmd) (Session, error) {
	return newSessionWithIDs(ctx, cmd, "", "")
}

// newSessionWithIDs is the internal constructor that accepts explicit sessID and
// runID for the lifecycle Machine. When sessID is empty a placeholder is used;
// callers that know the IDs at Session creation time (e.g. handler.Launch) SHOULD
// use newSessionWithIDs directly.
func newSessionWithIDs(ctx context.Context, cmd *exec.Cmd, sessID, runID string) (Session, error) {
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("handler: NewSession: StdinPipe: %w: %w", err, ErrStructural)
	}

	// Use os.Pipe directly instead of cmd.StdoutPipe / cmd.StderrPipe.
	// cmd.StdoutPipe adds the read-end to closeAfterWait, so cmd.Wait closes it
	// while callers (e.g. io.ReadAll) may still be reading — a data race under
	// parallel test load ("read |0: file already closed"). By owning the OS pipe
	// ourselves and setting cmd.Stdout/Stderr to the write ends, closeAfterWait
	// stays empty for stdout/stderr, and cmd.Wait never closes the read ends.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("handler: NewSession: stdout Pipe: %w: %w", err, ErrStructural)
	}
	cmd.Stdout = stdoutW

	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		if closeErr := closeAll(stdoutR, stdoutW); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("stdout pipe cleanup: %w", closeErr))
		}
		return nil, fmt.Errorf("handler: NewSession: stderr Pipe: %w: %w", err, ErrStructural)
	}
	cmd.Stderr = stderrW

	// Construct the lifecycle Machine in StateSpawning (HC-065 initial state).
	// sessID and runID default to placeholder values when not provided; they are
	// enriched by the caller after Launch returns.
	if sessID == "" {
		sessID = "unknown"
	}
	if runID == "" {
		runID = "unknown"
	}
	machine := hclifecycle.New(sessID, runID)

	if err := cmd.Start(); err != nil {
		// HC-065: Spawning→Failed when cmd.Start returns an error. The machine is
		// discarded along with the error path — no caller can observe this session,
		// so an invalid-transition error here can only mean an FSM defect. Join it
		// (and any pipe-cleanup failure) onto the returned error rather than
		// dropping it silently.
		if tErr := machine.Transition(hclifecycle.StateFailed, hclifecycle.ReasonError, "cmd_start_error", err.Error()); tErr != nil {
			err = errors.Join(err, fmt.Errorf("lifecycle transition to failed: %w", tErr))
		}
		if closeErr := closeAll(stdoutR, stdoutW, stderrR, stderrW); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("pipe cleanup: %w", closeErr))
		}
		return nil, fmt.Errorf("handler: NewSession: cmd.Start: %w: %w", err, ErrStructural)
	}

	// Close the parent's write ends — the subprocess inherited them; keeping
	// them open in the parent would prevent EOF from reaching the readers, so a
	// failure here would wedge every reader of this session. Reap the child we
	// just started and fail the construction rather than handing back a session
	// whose stdout/stderr can never reach EOF.
	if closeErr := closeAll(stdoutW, stderrW); closeErr != nil {
		return nil, abandonStartedSession(cmd, stdinPipe, stdoutR, stderrR,
			fmt.Errorf("handler: NewSession: close parent write ends: %w: %w", closeErr, ErrStructural))
	}

	// HC-065: Spawning→Initializing — subprocess started successfully. An error
	// here means the FSM rejected a statically-valid transition, i.e. a defect;
	// surface it instead of handing back a session whose machine is in the wrong
	// state (the watcher and workloop drive every later transition off it).
	if tErr := machine.Transition(hclifecycle.StateInitializing, hclifecycle.ReasonSpawnStarted, "", ""); tErr != nil {
		return nil, abandonStartedSession(cmd, stdinPipe, stdoutR, stderrR,
			fmt.Errorf("handler: NewSession: lifecycle transition to initializing: %w: %w", tErr, ErrStructural))
	}

	stdoutPR := bridgeStdout(stdoutR)

	ring := newRingBuffer(stderrRingCapBytes)

	s := &session{
		cmd:         cmd,
		waitOwner:   lifecycle.NewWaitOwner(cmd),
		stdin:       stdinPipe,
		stdout:      stdoutPR,
		stderr:      stderrR,
		startedAt:   time.Now(),
		stderrBuf:   ring,
		stderrDone:  make(chan struct{}),
		outcomeDone: make(chan struct{}),
		machine:     machine,
	}

	// Drain stderr into the ring buffer concurrently so it never blocks the
	// subprocess. Close stderrDone when finished so runWait can safely read the
	// ring buffer without racing the drain goroutine's writes.
	go func() {
		s.drainStderr(stderrR)
		close(s.stderrDone)
	}()

	// Run WaitAndReap in the single dedicated goroutine per PL-014/PL-016, then
	// populate outcome.
	go s.runWait(ctx)

	return s, nil
}

// abandonStartedCmd reaps a subprocess that cmd.Start already launched but that
// newSessionWithIDs has decided not to hand back. Without it the child would be
// left running with no Session handle and no WaitOwner, i.e. a zombie until the
// daemon's orphan sweep. Both failures are best-effort: the caller is already
// returning the error that made the session unusable.
//
// The direct cmd.Wait call is not a PL-014 violation. It is only reachable
// before the session's lifecycle.WaitOwner is constructed, so no WaitOwner
// exists for this cmd and no other goroutine can ever call cmd.Wait on it —
// this is the single Wait the discipline requires, not a second one.
func abandonStartedCmd(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		fmt.Fprintf(os.Stderr, "handler: NewSession: kill abandoned child %d: %v\n", cmd.Process.Pid, err)
	}
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			fmt.Fprintf(os.Stderr, "handler: NewSession: reap abandoned child: %v\n", err)
		}
	}
}

// abandonStartedSession releases everything newSessionWithIDs owns once
// cmd.Start has already succeeded but the constructor has decided not to hand
// the session back, and returns the error the constructor should return.
//
// It closes the three parent-side handles the constructor opened — the stdin
// write end and the stdout/stderr read ends — and then reaps the child via
// abandonStartedCmd. Without the closes those descriptors leak: once the
// constructor returns nil nothing in the process holds a reference to them, so
// they survive until the garbage collector happens to run a finaliser, and a
// burst of failed constructions can exhaust the descriptor limit first.
//
// The parent's stdout/stderr WRITE ends are deliberately not closed here. Every
// caller reaches this point only after closeAll(stdoutW, stderrW) has run.
//
// Order matters. The stdin write end is closed BEFORE the reap: exec.Cmd.Wait
// closes its own parentIOPipes list — which the StdinPipe write end is on — and
// discards the result, so closing after the reap would surface a spurious
// "file already closed" as a cleanup failure. Closing first also gives the
// child stdin EOF on its way out.
//
// cause is returned unchanged when cleanup succeeds, and joined with the close
// failures otherwise — the same reporting the cmd.Start failure path above uses,
// so a cleanup failure is never swallowed by the failure that triggered it.
func abandonStartedSession(cmd *exec.Cmd, stdinPipe io.Closer, stdoutR, stderrR *os.File, cause error) error {
	closeErr := closeAll(stdinPipe, stdoutR, stderrR)
	abandonStartedCmd(cmd)
	if closeErr != nil {
		return errors.Join(cause, fmt.Errorf("pipe cleanup: %w", closeErr))
	}
	return cause
}

// bridgeStdout copies stdoutR into an io.Pipe and returns the read end.
//
// Callers receive a clean io.Reader whose lifetime is independent of the OS file
// descriptor. The bridge goroutine copies until EOF (subprocess exit closes the
// write end), then closes both ends so callers see EOF. cmd.Wait has no
// closeAfterWait entry for stdout, so it cannot race with ongoing reads.
//
// A copy or close error is delivered to the reader via CloseWithError rather
// than dropped: closing the pipe cleanly would present a truncated stdout as a
// clean EOF, and the watcher would then report a well-formed-but-short progress
// stream instead of the read failure that actually ended it.
func bridgeStdout(stdoutR *os.File) *io.PipeReader {
	stdoutPR, stdoutPW := io.Pipe()
	go func() {
		_, copyErr := io.Copy(stdoutPW, stdoutR)
		if closeErr := stdoutR.Close(); closeErr != nil && copyErr == nil {
			copyErr = fmt.Errorf("close stdout read end: %w", closeErr)
		}
		if copyErr != nil {
			copyErr = fmt.Errorf("handler: session: stdout bridge: %w", copyErr)
		}
		// CloseWithError(nil) is exactly Close(); both propagate copyErr (or clean
		// EOF) to every reader of stdoutPR.
		if closeErr := stdoutPW.CloseWithError(copyErr); closeErr != nil {
			fmt.Fprintf(os.Stderr, "handler: session: close stdout bridge: %v\n", closeErr)
		}
	}()
	return stdoutPR
}

// drainStderr reads all bytes from r into the ring buffer.  It runs as a
// background goroutine so that a subprocess with voluminous stderr output
// never blocks on a full pipe buffer.
func (s *session) drainStderr(r io.Reader) {
	buf := make([]byte, 512)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.stderrBuf.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// runWait is the single dedicated goroutine that calls WaitOwner.WaitAndReap
// per PL-014.  It populates s.outcome once Wait returns.
func (s *session) runWait(_ context.Context) {
	startedAt := s.startedAt
	// WaitAndReap closes waitOwner.Done() before it returns, so Kill's exit edge
	// fires the instant the child is reaped and is not delayed by the stderr
	// drain join below.
	waitErr := s.waitOwner.WaitAndReap()

	// Wait for drainStderr to finish before reading stderrBuf so that concurrent
	// ringBuffer.Write and ringBuffer.Bytes calls don't race.
	//
	// Normal exit: the subprocess and all its descendants are gone, so the
	// stderr write-end is fully closed; drainStderr hits EOF and closes
	// stderrDone essentially immediately — the grace below never elapses and
	// stderr capture is unaffected.
	//
	// Cancel/kill path (hk-4c7kw): a grandchild the handler forked (e.g. a
	// `sleep` left by a shell handler) inherited the stderr write-end and may
	// still be alive after the immediate process was reaped, so drainStderr's
	// Read stays blocked and stderrDone would never close — wedging sess.Wait
	// (and therefore daemon shutdown) for the full grandchild runtime.  When the
	// grace elapses, close the read-end to force drainStderr to return so
	// stderrDone closes and the outcome is finalised promptly.  Closing the
	// read-end makes the blocked Read return an error, ending drainStderr; we
	// only read stderrBuf.Bytes() after stderrDone closes, so the no-race
	// invariant on the unsynchronised ringBuffer is preserved either way.
	select {
	case <-s.stderrDone:
	case <-time.After(stderrDrainGrace):
		if c, ok := s.stderr.(io.Closer); ok {
			_ = c.Close()
		}
		<-s.stderrDone
	}
	duration := time.Since(startedAt)

	o := Outcome{
		ExitCode:   0,
		Signal:     -1,
		Duration:   duration,
		StderrTail: s.stderrBuf.Bytes(),
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			o.ExitCode = exitErr.ExitCode()
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					o.Signal = status.Signal()
					o.ExitCode = -1
				}
			}
		} else {
			o.ExitCode = -1
		}
	}

	s.outcome = o
	s.outcomeReady.Store(true)
	close(s.outcomeDone)
}

// SendInput writes line + '\n' to the subprocess stdin.
//
// The write is bounded by ctx: a wedged child that never drains its stdin
// (the OS pipe buffer is ~64 KiB) would otherwise block the caller forever.
// The write runs in a goroutine; on ctx expiry SendInput returns
// ctx.Err()-wrapping ErrCanceled immediately. The writer goroutine itself
// remains blocked only until the pipe unblocks — CloseStdin or subprocess
// exit (Kill) releases it — so it is bounded by the session lifetime, not
// leaked indefinitely.
func (s *session) SendInput(ctx context.Context, line string) error {
	data := line + "\n"
	done := make(chan error, 1)
	go func() {
		_, err := io.WriteString(s.stdin, data)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("handler: Session.SendInput: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("handler: Session.SendInput: stdin write not completed: %w: %w", ctx.Err(), ErrCanceled)
	}
}

// Kill sends SIGTERM to the subprocess.  If ctx expires before the subprocess
// exits, it escalates to SIGKILL.
//
// Signalling targets the subprocess PID directly (positive PID), NOT the
// process group (-pgid).  The handler is spawned with SysProcAttr{Setpgid:
// true, Pgid: <daemon_pgid>} (lifecycle.SpawnChildSysProcAttr per HC-044 /
// PL-006a), so the child joins the DAEMON's process group rather than becoming
// its own group leader.  A syscall.Kill(-childPid, …) therefore addresses a
// process group whose ID equals the child's PID — which does not exist — so the
// signal returns ESRCH and never reaches the subprocess (this was the hk-4c7kw
// slow-shutdown bug: the in-flight handler ran to natural completion).  Using
// -daemonPgid would be worse: it would signal the daemon itself and every
// sibling handler.  Signalling the positive child PID reaps the immediate
// handler process promptly; any grandchildren it forked are bounded by the
// caller's post-kill wait (waitWithSocketGrace) plus the daemon's orphan sweep.
//
// Bead: hk-4c7kw.
func (s *session) Kill(ctx context.Context) error {
	pid := s.cmd.Process.Pid

	// SIGTERM the subprocess.  ESRCH (process already exited and been reaped) is
	// not an error — the reap below observes the exit either way.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("handler: Session.Kill: SIGTERM pid %d: %w", pid, err)
	}

	// Wait for process exit or ctx deadline; on deadline, escalate to SIGKILL.
	// waitOwner.Done() is closed the moment the child is reaped, so repeated Kill
	// calls all observe the same edge; unlike a Wait, observing Done consumes
	// nothing, so a later Wait still reports the child's exit error.
	select {
	case <-s.waitOwner.Done():
		// Process exited cleanly after SIGTERM.
		return nil
	case <-ctx.Done():
		// ctx expired (or was already cancelled when Kill was called) —
		// escalate to SIGKILL.
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("handler: Session.Kill: SIGKILL pid %d: %w", pid, err)
		}
		return nil
	}
}

// Wait blocks until the subprocess has exited, been reaped, and the outcome has
// been fully populated (including stderr tail).  After Wait returns, Outcome()
// is guaranteed to reflect the final process state.
func (s *session) Wait(_ context.Context) error {
	// Block until runWait has populated s.outcome so callers can call Outcome()
	// immediately after Wait without racing the drain goroutine. waitOwner.Wait
	// then returns the cached exit error, identically for every caller.
	<-s.outcomeDone
	return s.waitOwner.Wait()
}

// Outcome returns the exit metadata populated once Wait returns.  Before Wait
// returns, Outcome returns a zero-value Outcome.
func (s *session) Outcome() Outcome {
	if !s.outcomeReady.Load() {
		return Outcome{}
	}
	return s.outcome
}

// Stdout returns the io.Reader attached to the subprocess stdout pipe.
func (s *session) Stdout() io.Reader {
	return s.stdout
}

// Stderr returns the io.Reader attached to the subprocess stderr pipe.
// Note: the background drainStderr goroutine also reads from this pipe; callers
// should not read Stderr directly after NewSession returns — the pipe is
// consumed by the drain goroutine.  Access StderrTail via Outcome() instead.
func (s *session) Stderr() io.Reader {
	return s.stderr
}

// CloseStdin closes the write end of the subprocess stdin pipe so the
// subprocess sees EOF after LaunchSpec delivery per HC-005.
func (s *session) CloseStdin() error {
	return s.stdin.Close()
}

// Machine returns the per-session lifecycle FSM (HC-064..HC-067).
func (s *session) Machine() *hclifecycle.Machine {
	return s.machine
}

// ─────────────────────────────────────────────────────────────────────────────
// ringBuffer — fixed-capacity byte ring that retains the last N bytes written.
// ─────────────────────────────────────────────────────────────────────────────

type ringBuffer struct {
	buf  []byte
	cap  int
	pos  int
	full bool
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{
		buf: make([]byte, capacity),
		cap: capacity,
	}
}

// Write appends p into the ring, overwriting oldest bytes when full.
func (r *ringBuffer) Write(p []byte) {
	for _, b := range p {
		r.buf[r.pos] = b
		r.pos = (r.pos + 1) % r.cap
		if r.pos == 0 {
			r.full = true
		}
	}
}

// Bytes returns a copy of the buffered bytes in chronological order.
func (r *ringBuffer) Bytes() []byte {
	if !r.full {
		out := make([]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return out
	}
	out := make([]byte, r.cap)
	copy(out, r.buf[r.pos:])
	copy(out[r.cap-r.pos:], r.buf[:r.pos])
	return out
}
