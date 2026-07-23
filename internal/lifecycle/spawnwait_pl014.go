package lifecycle

import (
	"os/exec"
	"sync"
)

// WaitOwner enforces the single-owner cmd.Wait() discipline required by PL-014
// and PL-016: exactly one goroutine is responsible for calling cmd.Wait() on a
// spawned subprocess. All other goroutines that need the subprocess exit status
// receive it via the shared result channel after the owner's Wait returns.
//
// The single-owner discipline prevents the zombie accumulation that arises from
// calling cmd.Wait() more than once (which panics in Go's exec package) or from
// never calling it (which leaves the child as a zombie in the kernel's process
// table). Both outcomes are PL-INV-005 violations.
//
// Usage pattern:
//
//	cmd := exec.Command(...)
//	cmd.SysProcAttr = lifecycle.SpawnSysProcAttr(pgid)
//	cmd.Env = append(os.Environ(), lifecycle.ProvenanceEnvVar(hash))
//	if err := cmd.Start(); err != nil { ... }
//	owner := lifecycle.NewWaitOwner(cmd)
//
//	// In the single dedicated goroutine (the "watcher"):
//	go func() { exitErr := owner.WaitAndReap() /* handle */ }()
//
//	// In any other goroutine that needs the exit status:
//	exitErr := owner.Wait()
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — "Every spawn MUST have exactly
// one Go goroutine that owns the *exec.Cmd and that goroutine MUST call
// cmd.Wait() exactly once. Failure produces zombies and is a PL-INV-005
// conformance violation regardless of kill(pid, 0) reporting."
//
// Spec ref: process-lifecycle.md §4.6 PL-016 — "The handler-contract watcher
// goroutine is the exclusive cmd.Wait() caller for its session's subprocess."
type WaitOwner struct {
	cmd  *exec.Cmd
	once sync.Once

	// result is the value cmd.Wait returned. It is written exactly once, inside
	// once.Do, before done is closed; every read happens after a receive on done
	// (or after once.Do has returned), so it needs no further synchronisation.
	//
	// The result is a stored field rather than a channel payload on purpose: a
	// channel delivers a value to ONE receiver, so an error broadcast that way
	// reaches the first reader and leaves every later reader observing the
	// closed-channel nil — a failed subprocess reported as a clean exit
	// (hk-qun49). done carries only the "reaped" edge, which broadcasts safely.
	result error

	// done is closed once result is populated. Closing (rather than sending)
	// makes the edge observable by any number of waiters, any number of times.
	done chan struct{}
}

// NewWaitOwner wraps a started *exec.Cmd in a WaitOwner. The caller MUST have
// already called cmd.Start() successfully before constructing a WaitOwner.
//
// The WaitOwner does NOT start a goroutine automatically; the caller is
// responsible for calling WaitAndReap() in the owning goroutine.
func NewWaitOwner(cmd *exec.Cmd) *WaitOwner {
	return &WaitOwner{
		cmd:  cmd,
		done: make(chan struct{}),
	}
}

// WaitAndReap is the single allowed caller of cmd.Wait(). It MUST be called
// exactly once, from the single dedicated "watcher" goroutine. After cmd.Wait()
// returns, the exit error is broadcast to all goroutines blocked in Wait().
//
// Subsequent calls to WaitAndReap are no-ops (the sync.Once guard prevents a
// second cmd.Wait() call) and return the cached error from the one call that
// did run. A concurrent second caller blocks inside once.Do until the first
// completes, then sees the same value.
//
// Spec ref: process-lifecycle.md §4.5 PL-014; §4.6 PL-016.
func (o *WaitOwner) WaitAndReap() error {
	o.once.Do(func() {
		o.result = o.cmd.Wait()
		close(o.done)
	})
	// sync.Once.Do returns only after f has completed, and establishes the
	// happens-before edge that makes o.result safe to read here.
	return o.result
}

// Wait returns the exit error of the subprocess. If WaitAndReap has not yet
// been called (or has not yet returned), Wait blocks until it does. Every call
// — from any goroutine, any number of times — returns that same cached error;
// subsequent calls do not block.
//
// Wait is safe to call from any goroutine at any time.
func (o *WaitOwner) Wait() error {
	<-o.done
	return o.result
}

// Done returns a channel that is closed once the subprocess has been reaped and
// the exit error is available from Wait. Use it to select on process exit
// against a context or timeout; unlike a value channel, closing broadcasts, so
// any number of observers see the edge. Reading from Done never consumes the
// exit error — call Wait for that.
func (o *WaitOwner) Done() <-chan struct{} {
	return o.done
}

// Cmd returns the underlying *exec.Cmd. Callers MAY inspect the Cmd (e.g., to
// read Pid) but MUST NOT call cmd.Wait() directly — use WaitAndReap() instead.
func (o *WaitOwner) Cmd() *exec.Cmd {
	return o.cmd
}
