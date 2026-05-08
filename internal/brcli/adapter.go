package brcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// Adapter is the sole translation layer between harmonik and the Beads `br`
// CLI. All Beads interactions from harmonik code MUST route through Adapter.
//
// Spec ref: specs/beads-integration.md §4.8 BI-025.
//
// Production callers MUST NOT inject a custom binary path; they MUST resolve
// `br` from PATH at startup and pass that resolved path to New. The
// constructor parameter is for testability only — unit tests MAY substitute
// a mock `br` binary at the injected path.
type Adapter struct {
	brPath string
}

// New returns an Adapter that invokes the `br` binary at brPath. brPath MUST
// be a non-empty absolute path; New does NOT resolve from PATH (that is the
// production caller's responsibility per BI-025 — see daemon startup
// integration in process-lifecycle.md §4.2).
//
// Returns nil and an error if brPath is empty.
func New(brPath string) (*Adapter, error) {
	if brPath == "" {
		return nil, errors.New("brcli.New: brPath must be non-empty; production callers must resolve br from PATH at startup")
	}
	return &Adapter{brPath: brPath}, nil
}

// Result carries the captured outputs of a single `br` invocation.
//
// The fields are the raw subprocess outcome; classification of ExitCode into
// the BrError taxonomy (BI-025a) is NOT performed here — that is the
// responsibility of hk-872.28. Callers receiving a Result MUST consume both
// Stdout and ExitCode in tandem when interpreting outcome.
//
// TODO(hk-872.28): BrError enum classification of ExitCode goes here.
// TODO(hk-872.31): stderr 1 MiB cap + truncation marker attaches to this type.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Run invokes `<brPath> <args...>` with the supplied context. It blocks until
// the subprocess exits OR the context is canceled. On success or non-zero
// exit, Run returns a Result with the captured outputs. On exec failure
// (binary not found, fork failed, etc.) Run returns Result zero-value plus
// the underlying error.
//
// Run is the LOW-LEVEL PRIMITIVE; it does not implement:
//   - BI-025a exit-code taxonomy → hk-872.28
//   - BI-025b --format json flag → hk-872.29
//   - BI-025c timeout discipline → implemented in RunWithTimeout (timeout.go)
//   - BI-025d stderr cap + classification → hk-872.31
//   - BI-025e concurrency discipline → hk-872.32
//
// Higher-level methods built on Run will add those layers.
func (a *Adapter) Run(ctx context.Context, args ...string) (Result, error) {
	// TODO(hk-872.29): prepend --format json to args.
	// NOTE(hk-872.30): timeout discipline is in RunWithTimeout (timeout.go); Run is the
	// low-level primitive used by higher-level methods that add their own timeout wrapping.
	// TODO(hk-872.31): enforce 1 MiB stderr cap and classify captured stderr.
	// TODO(hk-872.32): no adapter-side mutex; SQLite WAL handles concurrent writes.

	//nolint:gosec // G204: brPath is resolved from PATH at startup by the production caller; args are typed harmonik-internal values, not user input.
	cmd := exec.CommandContext(ctx, a.brPath, args...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// ExitCode -1 means the process was killed by a signal. When the
			// context has an error it means exec.CommandContext sent the kill
			// (cancellation or timeout); treat this as an exec-level failure so
			// callers can observe context.Canceled / context.DeadlineExceeded.
			// NOTE(hk-872.30): RunWithTimeout handles the timeout path; when it
			// fires SIGTERM/SIGKILL the context is a non-canceling Background()
			// so this branch is not taken from that path — timeout errors are
			// classified as BrUnavailable directly in RunWithTimeout.
			if exitErr.ExitCode() == -1 && ctx.Err() != nil {
				return Result{}, fmt.Errorf("brcli: subprocess killed by context: %w", ctx.Err())
			}
			// Non-zero exit (>0) is a normal subprocess outcome, not an exec
			// failure. Return the captured output with the exit code; err is nil
			// so the caller can inspect Result.ExitCode and defer to BI-025a
			// taxonomy (hk-872.28).
			return Result{
				Stdout:   stdoutBuf.Bytes(),
				Stderr:   stderrBuf.Bytes(),
				ExitCode: exitErr.ExitCode(),
			}, nil
		}
		// Exec failure: binary not found, fork failed, context canceled before
		// start, etc. Return zero-value Result + the original error so the
		// caller can distinguish "br ran and failed" from "br could not be
		// launched".
		return Result{}, fmt.Errorf("brcli: exec failed: %w", err)
	}

	return Result{
		Stdout:   stdoutBuf.Bytes(),
		Stderr:   stderrBuf.Bytes(),
		ExitCode: 0,
	}, nil
}
