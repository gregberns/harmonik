// Package handler — Session wrapping exec.Cmd + WaitOwner (MVH_ROADMAP row #6).
//
// Session is the daemon's handle on a running subprocess (Claude Code or twin).
// It owns stdin write, stdout/stderr Reader exposure for the row-#7 watcher
// wire-up, signal-based termination, and reap via lifecycle.WaitOwner.
//
// # Composition with WaitOwner
//
// Session.Wait delegates to lifecycle.WaitOwner.Wait so that exactly one
// goroutine (the one that calls runWait in the background) calls cmd.Wait.
// This is the PL-014 single-owner discipline.  callers that need the exit
// status receive it via Session.Wait — they never call cmd.Wait directly.
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

	// Kill sends SIGTERM to the subprocess process group. If ctx expires before
	// Wait returns, it escalates to SIGKILL.
	//
	// Kill returns once the termination signal has been sent (not once the
	// subprocess exits).  Use Wait to block until the process is reaped.
	Kill(ctx context.Context) error

	// Wait blocks until the subprocess exits and has been reaped. Delegates to
	// lifecycle.WaitOwner.Wait — only one goroutine ever calls cmd.Wait per
	// PL-014/PL-016.
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
		_ = stdoutR.Close()
		_ = stdoutW.Close()
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
		// discarded along with the error path — no caller can observe this session.
		_ = machine.Transition(hclifecycle.StateFailed, hclifecycle.ReasonError, "cmd_start_error", err.Error())
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
		return nil, fmt.Errorf("handler: NewSession: cmd.Start: %w: %w", err, ErrStructural)
	}

	// Close the parent's write ends — the subprocess inherited them; keeping
	// them open in the parent would prevent EOF from reaching the readers.
	_ = stdoutW.Close()
	_ = stderrW.Close()

	// HC-065: Spawning→Initializing — subprocess started successfully.
	_ = machine.Transition(hclifecycle.StateInitializing, hclifecycle.ReasonSpawnStarted, "", "")

	// Bridge stdoutR through an io.Pipe so callers receive a clean io.Reader
	// whose lifetime is independent of the OS file descriptor. The bridge
	// goroutine copies until EOF (subprocess exit closes write end), then closes
	// both ends so callers see EOF. cmd.Wait has no closeAfterWait entry for
	// stdout, so it cannot race with ongoing reads.
	stdoutPR, stdoutPW := io.Pipe()
	go func() {
		_, _ = io.Copy(stdoutPW, stdoutR)
		_ = stdoutR.Close()
		_ = stdoutPW.Close()
	}()

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
	waitErr := s.waitOwner.WaitAndReap()
	// Wait for drainStderr to finish before reading stderrBuf so that concurrent
	// ringBuffer.Write and ringBuffer.Bytes calls don't race.
	<-s.stderrDone
	duration := time.Since(startedAt)

	o := Outcome{
		ExitCode:   0,
		Signal:     -1,
		Duration:   duration,
		StderrTail: s.stderrBuf.Bytes(),
	}

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
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
func (s *session) SendInput(_ context.Context, line string) error {
	data := line + "\n"
	_, err := io.WriteString(s.stdin, data)
	if err != nil {
		return fmt.Errorf("handler: Session.SendInput: %w", err)
	}
	return nil
}

// Kill sends SIGTERM to the subprocess process group.  If ctx expires before
// the subprocess exits, it escalates to SIGKILL.
func (s *session) Kill(ctx context.Context) error {
	pgid := s.cmd.Process.Pid // when Setpgid=true, pgid == pid of group leader

	// SIGTERM the process group.
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("handler: Session.Kill: SIGTERM pgid %d: %w", pgid, err)
	}

	// Wait for process exit or ctx deadline; on deadline, escalate to SIGKILL.
	waitDone := make(chan struct{})
	go func() {
		_ = s.waitOwner.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		// Process exited cleanly after SIGTERM.
		return nil
	case <-ctx.Done():
		// ctx expired — escalate to SIGKILL.
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
			return fmt.Errorf("handler: Session.Kill: SIGKILL pgid %d: %w", pgid, err)
		}
		return nil
	}
}

// Wait blocks until the subprocess has exited, been reaped, and the outcome has
// been fully populated (including stderr tail).  After Wait returns, Outcome()
// is guaranteed to reflect the final process state.
func (s *session) Wait(_ context.Context) error {
	err := s.waitOwner.Wait()
	// Block until runWait has populated s.outcome so callers can call Outcome()
	// immediately after Wait without racing the drain goroutine.
	<-s.outcomeDone
	return err
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
