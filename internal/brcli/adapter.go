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
// BrErr is the BI-025a classification of ExitCode into the harmonik-internal
// BrError taxonomy. It is populated by Run and RunWithTimeout for every
// subprocess outcome (both zero and non-zero exits). BrErr is NOT set when the
// subprocess could not be launched (exec error); in that case Run/RunWithTimeout
// return a non-nil error instead.
//
// Callers MUST check the returned error first. When error is nil, BrErr carries
// the authoritative classification; callers MUST NOT re-invoke BrErrorFromExitCode
// on ExitCode — BrErr is the canonical result.
//
// When BrErr == BrOther, the caller MUST emit divergence_inconclusive per
// [event-model.md §8.6.10] with reason=authority_unavailable (BI-025a). The
// event bus required for that emission is tracked by hk-872.57; callers
// observing BrOther MUST record a structured-log entry at level=warn per
// [operator-nfr.md §4.9 ON-035] with subsystem=beads-adapter until the event
// bus is available.
//
// TODO(hk-872.31): stderr 1 MiB cap + truncation marker attaches to this type.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	// BrErr is the BI-025a taxonomy classification of ExitCode.
	// Populated on every subprocess outcome (zero or non-zero exit).
	// Zero value (empty string) indicates the subprocess was not launched
	// (exec error); callers check the returned error in that case.
	BrErr BrError
}

// Run invokes `<brPath> <args...>` with the supplied context. It blocks until
// the subprocess exits OR the context is canceled. On success or non-zero
// exit, Run returns a Result with the captured outputs. On exec failure
// (binary not found, fork failed, etc.) Run returns Result zero-value plus
// the underlying error.
//
// Run is the LOW-LEVEL PRIMITIVE; it does not implement:
//   - BI-025b --format json flag → use runFormatJSON for commands that support --format json.
//     Commands that use the global --json flag (br audit log) or lack JSON support (br --version)
//     call Run directly; see BI-025b carve-out notes in audit.go and version.go.
//   - BI-025c timeout discipline → implemented in RunWithTimeout (timeout.go)
//   - BI-025d stderr cap + classification → hk-872.31
//   - BI-025e concurrency discipline → hk-872.32
//
// BI-025a exit-code taxonomy IS implemented here: Result.BrErr is populated
// via BrErrorFromExitCode on every subprocess outcome (zero and non-zero exits).
//
// Higher-level methods built on Run will add the remaining layers.
func (a *Adapter) Run(ctx context.Context, args ...string) (Result, error) {
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
			// failure. Classify per BI-025a and return with BrErr populated.
			code := exitErr.ExitCode()
			return Result{
				Stdout:   stdoutBuf.Bytes(),
				Stderr:   stderrBuf.Bytes(),
				ExitCode: code,
				BrErr:    BrErrorFromExitCode(code),
			}, nil
		}
		// Exec failure: binary not found, fork failed, context canceled before
		// start, etc. Return zero-value Result + the original error so the
		// caller can distinguish "br ran and failed" from "br could not be
		// launched". BrErr is not set (zero value) because no subprocess ran.
		return Result{}, fmt.Errorf("brcli: exec failed: %w", err)
	}

	// Exit code 0 — success.
	return Result{
		Stdout:   stdoutBuf.Bytes(),
		Stderr:   stderrBuf.Bytes(),
		ExitCode: 0,
		BrErr:    BrOK,
	}, nil
}

// runFormatJSON invokes `<brPath> <args...> --format json` and returns the
// subprocess result. It is the BI-025b JSON-mode wrapper for commands that
// expose a --format flag (br show, br dep list). Callers that use the global
// --json flag (br audit log) or that require text parsing (br --version) MUST
// call Run directly; see BI-025b carve-out notes in audit.go and version.go.
//
// Parse failures of the structured output returned by runFormatJSON callers
// MUST classify as BrSchemaMismatch per BI-025b. Enforcement is in each
// higher-level method (ShowBead, ListDependencies).
//
// Spec ref: specs/beads-integration.md §4.8a BI-025b.
func (a *Adapter) runFormatJSON(ctx context.Context, args ...string) (Result, error) {
	jsonArgs := make([]string, 0, len(args)+2)
	jsonArgs = append(jsonArgs, args...)
	jsonArgs = append(jsonArgs, "--format", "json")
	return a.Run(ctx, jsonArgs...)
}
