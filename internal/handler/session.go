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
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

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
// Returns ErrStructural-wrapping error on pipe-setup or start failure.
func NewSession(ctx context.Context, cmd *exec.Cmd) (Session, error) {
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("handler: NewSession: StdinPipe: %w: %w", err, ErrStructural)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("handler: NewSession: StdoutPipe: %w: %w", err, ErrStructural)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("handler: NewSession: StderrPipe: %w: %w", err, ErrStructural)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("handler: NewSession: cmd.Start: %w: %w", err, ErrStructural)
	}

	ring := newRingBuffer(stderrRingCapBytes)

	s := &session{
		cmd:       cmd,
		waitOwner: lifecycle.NewWaitOwner(cmd),
		stdin:     stdinPipe,
		stdout:    stdoutPipe,
		stderr:    stderrPipe,
		startedAt: time.Now(),
		stderrBuf: ring,
	}

	// Drain stderr into the ring buffer concurrently so it never blocks the
	// subprocess.
	go s.drainStderr(stderrPipe)

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

// Wait blocks until the subprocess has exited and been reaped.  Delegates to
// lifecycle.WaitOwner.Wait; the WaitOwner holds the PL-014 single-owner
// discipline so callers never race on cmd.Wait.
func (s *session) Wait(_ context.Context) error {
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
