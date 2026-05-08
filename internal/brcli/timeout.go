package brcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// CommandKind distinguishes read commands from write commands for the purpose
// of selecting the default wall-clock timeout budget per BI-025c.
//
// Spec ref: specs/beads-integration.md §4.8a BI-025c.
type CommandKind int

const (
	// CommandKindRead selects the 5s default timeout (operator-tunable per
	// operator-nfr.md §4.9). Applies to br list, br show, br ready, and any
	// other read-only br invocation.
	CommandKindRead CommandKind = iota

	// CommandKindWrite selects the 10s default timeout (operator-tunable per
	// operator-nfr.md §4.9). Applies to br create, br update, br label, and any
	// other state-mutating br invocation.
	CommandKindWrite
)

// defaultReadTimeout is the BI-025c default wall-clock timeout for read commands.
const defaultReadTimeout = 5 * time.Second

// defaultWriteTimeout is the BI-025c default wall-clock timeout for write commands.
const defaultWriteTimeout = 10 * time.Second

// sigtermGrace is the HC-018 grace period: after SIGTERM, wait up to this
// duration before escalating to SIGKILL.
const sigtermGrace = 5 * time.Second

// TimeoutConfig holds operator-tunable timeout values for br subprocess
// invocations per BI-025c and operator-nfr.md §4.9.
//
// Zero values default to the BI-025c defaults (5s read / 10s write). The
// operator configures this struct at daemon startup; it is then passed to
// RunWithTimeout on every br invocation.
type TimeoutConfig struct {
	// ReadTimeout is the wall-clock budget for read commands (br list, br show,
	// br ready, etc.). Default: 5s per BI-025c.
	ReadTimeout time.Duration

	// WriteTimeout is the wall-clock budget for write commands (br create,
	// br update, br label, etc.). Default: 10s per BI-025c.
	WriteTimeout time.Duration
}

// effectiveTimeout returns the resolved timeout for the given CommandKind,
// applying BI-025c defaults when the field is zero.
func (c TimeoutConfig) effectiveTimeout(kind CommandKind) time.Duration {
	switch kind {
	case CommandKindWrite:
		if c.WriteTimeout > 0 {
			return c.WriteTimeout
		}
		return defaultWriteTimeout
	default: // CommandKindRead
		if c.ReadTimeout > 0 {
			return c.ReadTimeout
		}
		return defaultReadTimeout
	}
}

// RunWithTimeout invokes `<brPath> <args...>` under a bounded wall-clock
// timeout per BI-025c. kind selects the timeout budget (5s read / 10s write
// by default; operator-tunable via cfg). The outer ctx is also honoured: if
// ctx is canceled before the timeout fires, the subprocess receives the same
// SIGTERM-then-SIGKILL treatment.
//
// On timeout (or ctx cancellation), RunWithTimeout:
//  1. Sends SIGTERM to the subprocess.
//  2. Waits up to sigtermGrace (5s) for the subprocess to exit per HC-018.
//  3. Sends SIGKILL if the subprocess is still running.
//  4. Calls cmd.Wait() to reap the subprocess per PL-014.
//
// A subprocess terminated via this path returns an error wrapping BrUnavailable
// (per BI-025c; NOT BrOther). Callers test with errors.Is(err, BrUnavailable).
//
// On success or non-zero subprocess exit (before the timeout fires),
// RunWithTimeout returns the same Result semantics as Run.
//
// Spec refs: specs/beads-integration.md §4.8a BI-025c;
// specs/handler-contract.md §4.3 HC-018;
// specs/process-lifecycle.md §4.5 PL-014.
func (a *Adapter) RunWithTimeout(ctx context.Context, cfg TimeoutConfig, kind CommandKind, args ...string) (Result, error) {
	budget := cfg.effectiveTimeout(kind)

	// Use context.Background() for CommandContext so that Go's built-in
	// automatic-SIGKILL-on-cancel behaviour does not fire; we implement the
	// SIGTERM-then-SIGKILL sequence ourselves per HC-018.
	//
	//nolint:gosec,contextcheck // G204: brPath is resolved from PATH at startup by the production caller; args are typed harmonik-internal values, not user input. context.Background() is intentional: prevents Go's auto-SIGKILL-on-cancel so the HC-018 manual SIGTERM→SIGKILL grace path (BI-025c) owns termination.
	cmd := exec.CommandContext(context.Background(), a.brPath, args...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("brcli: exec failed: %w", err)
	}

	// waitCh carries the cmd.Wait() result once the subprocess exits.
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	// Budget timer and outer-ctx cancellation both drive the termination path.
	budgetTimer := time.NewTimer(budget)
	defer budgetTimer.Stop()

	select {
	case waitErr := <-waitCh:
		// Subprocess exited before the timeout fired.
		return classifyWaitResult(waitErr, stdoutBuf.Bytes(), stderrBuf.Bytes())

	case <-budgetTimer.C:
		return terminateAndClassify(cmd, waitCh,
			fmt.Errorf("brcli: br subprocess wall-clock timeout (%s): %w", budget, BrUnavailable))

	case <-ctx.Done():
		return terminateAndClassify(cmd, waitCh,
			fmt.Errorf("brcli: br subprocess killed by context cancellation: %w", BrUnavailable))
	}
}

// terminateAndClassify implements the HC-018 SIGTERM-then-SIGKILL sequence:
//  1. Send SIGTERM to the subprocess.
//  2. Wait up to sigtermGrace for the subprocess to exit.
//  3. Send SIGKILL if still running.
//  4. Call cmd.Wait() via waitCh to reap per PL-014.
//
// The returned error is always baseErr (BrUnavailable-wrapped), regardless of
// what the subprocess exited with after being signalled.
func terminateAndClassify(cmd *exec.Cmd, waitCh <-chan error, baseErr error) (Result, error) {
	// Step 1: SIGTERM.
	if cmd.Process != nil {
		//nolint:errcheck // SIGTERM on already-exited process returns syscall.ESRCH; intentionally discarded per HC-018 grace handling.
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}

	// Step 2: Wait up to sigtermGrace for the subprocess to exit.
	graceTimer := time.NewTimer(sigtermGrace)
	defer graceTimer.Stop()

	select {
	case <-waitCh:
		// Exited cleanly after SIGTERM; reap is done (waitCh consumed cmd.Wait()).
	case <-graceTimer.C:
		// Step 3: SIGKILL — subprocess did not exit within the grace period.
		if cmd.Process != nil {
			//nolint:errcheck // SIGKILL on already-exited process returns syscall.ESRCH; intentionally discarded per HC-018 grace handling.
			_ = cmd.Process.Signal(os.Kill)
		}
		// Step 4: reap per PL-014.
		<-waitCh
	}

	return Result{}, baseErr
}

// classifyWaitResult converts a cmd.Wait() result into a (Result, error) pair
// using the same semantics as Adapter.Run: non-zero exit is a Result (not an
// error); exec failure is an error. Result.BrErr is populated via
// BrErrorFromExitCode per BI-025a for every subprocess outcome.
func classifyWaitResult(waitErr error, stdout, stderr []byte) (Result, error) {
	if waitErr == nil {
		return Result{Stdout: stdout, Stderr: stderr, ExitCode: 0, BrErr: BrOK}, nil
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		code := exitErr.ExitCode()
		return Result{
			Stdout:   stdout,
			Stderr:   stderr,
			ExitCode: code,
			BrErr:    BrErrorFromExitCode(code),
		}, nil
	}

	return Result{}, fmt.Errorf("brcli: exec failed: %w", waitErr)
}
