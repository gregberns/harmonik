// Package apptap implements a transparent stdio splice for the codex app-server
// integration (codex-app-server T1, hk-893ct).
//
// A Tap wraps a child process and tees both directions of its stdio to capture
// writers verbatim — no parsing, no framing, no drops. The captured bytes are
// identical to the bytes that flow between the caller and the child: the capture
// is a passive side-channel, not a proxy buffer.
//
// Design invariants (T1 gate):
//
//  1. Transparent: every byte written to Tap.Stdin reaches the child unchanged;
//     every byte the child writes to stdout reaches Tap.Stdout unchanged.
//
//  2. Lossless: every byte that flows in either direction appears exactly once in
//     the corresponding capture writer (InCapture or OutCapture).
//
//  3. Verbatim: the tap never parses, reframes, or delays bytes for meaning. The
//     capture writers receive raw bytes as they flow, not decoded lines or objects.
//
// The target child is `codex app-server` (a long-lived bidirectional JSON-RPC 2.0
// process), but Tap is protocol-agnostic — any client or child drives it.
//
// Index/correlation (deriving which capture bytes belong to which RPC call) is a
// separate, read-only pass on the captured files. Tap does not perform it.
package apptap

import (
	"io"
	"os"
	"os/exec"
)

// Tap transparently splices stdio between a caller and a child process, tee-ing
// both directions to capture writers verbatim.
//
// Wire diagram:
//
//	Tap.Stdin ──MultiWriter──► child stdin
//	                │
//	                └──────────► InCapture   (capture: caller→child bytes)
//
//	child stdout ──MultiWriter──► Tap.Stdout
//	                  │
//	                  └──────────► OutCapture  (capture: child→caller bytes)
//
// Zero values are not valid. Construct with a non-empty Binary. Stdin, Stdout,
// and Stderr default to os.Stdin/Stdout/Stderr when nil.
type Tap struct {
	// Binary is the child executable path (e.g. "codex").
	Binary string

	// Args are the child arguments (e.g. []string{"app-server"}).
	Args []string

	// InCapture, when non-nil, receives a verbatim copy of every byte flowing
	// from Tap.Stdin to the child's stdin. Write errors on InCapture are
	// propagated and abort the tap.
	InCapture io.Writer

	// OutCapture, when non-nil, receives a verbatim copy of every byte flowing
	// from the child's stdout to Tap.Stdout. Write errors on OutCapture are
	// propagated and abort the tap.
	OutCapture io.Writer

	// Stdin is the source for the child's stdin. Defaults to os.Stdin when nil.
	Stdin io.Reader

	// Stdout is the destination for the child's stdout. Defaults to os.Stdout
	// when nil.
	Stdout io.Writer

	// Stderr is the destination for the child's stderr (passed through
	// unchanged; not captured). Defaults to os.Stderr when nil.
	Stderr io.Writer
}

// Run spawns the child process, wires the transparent stdio splice, and waits
// for the child to exit. Returns the child's exit error (nil on clean exit 0).
//
// The splice goroutines are set up before Start so no bytes are lost at the
// seam between Start and Wait. Run returns only after both goroutines drain.
func (t *Tap) Run() error {
	stdin := t.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := t.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := t.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	cmd := exec.Command(t.Binary, t.Args...)
	cmd.Stderr = stderr

	childIn, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	childOut, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// inDst: bytes from caller stdin go to child stdin and (optionally) InCapture.
	inDst := io.Writer(childIn)
	if t.InCapture != nil {
		inDst = io.MultiWriter(childIn, t.InCapture)
	}

	// outDst: bytes from child stdout go to caller stdout and (optionally) OutCapture.
	outDst := io.Writer(stdout)
	if t.OutCapture != nil {
		outDst = io.MultiWriter(stdout, t.OutCapture)
	}

	// inErr receives the result of the stdin→child goroutine.
	inErr := make(chan error, 1)
	go func() {
		_, err := io.Copy(inDst, stdin)
		// Close child stdin so the child sees EOF when the caller closes its
		// end. Ignore close errors (child may have already exited).
		_ = childIn.Close()
		inErr <- err
	}()

	// Drain child stdout → outDst. Blocks until child closes its stdout (exit).
	_, copyOutErr := io.Copy(outDst, childOut)

	// Wait for the stdin goroutine to finish. Its error is secondary — a broken
	// pipe (child exited before draining stdin) is not a tap failure.
	<-inErr

	if err := cmd.Wait(); err != nil {
		return err
	}
	return copyOutErr
}
