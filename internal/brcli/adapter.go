package brcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
)

// Adapter is the sole translation layer between harmonik and the Beads `br`
// CLI. All Beads interactions from harmonik code MUST route through Adapter.
//
// Spec ref: specs/beads-integration.md §4.8 BI-025.
//
// BI-005 — no parallel authoritative cache: Beads is the source of truth for
// bead content (title, description, type). Adapter MUST NOT maintain an
// in-memory or on-disk cache of bead content that could be treated as
// authoritative. Every call to ShowBead invokes `br show` unconditionally;
// there is no memoisation layer. If a caller detects disagreement between a
// previously observed value and a fresh ShowBead result during a §4.5 read,
// the fresh `br` output is authoritative and the stale value MUST be
// discarded before any harmonik decision consumes it.
// Spec ref: specs/beads-integration.md §4.3 BI-005.
//
// Production callers MUST NOT inject a custom binary path; they MUST resolve
// `br` from PATH at startup and pass that resolved path to New. The
// constructor parameter is for testability only — unit tests MAY substitute
// a mock `br` binary at the injected path.
//
// projectDir pins the working directory of every `br` subprocess.  `br`
// discovers the .beads database by walking up from its CWD; if projectDir is
// empty the subprocess inherits the harmonik process's CWD, which is almost
// certainly wrong in production (the operator launched harmonik from an
// arbitrary directory, not the project root).  Production callers MUST supply
// projectDir via NewForProject; test callers that use mock binaries may use
// New (mock binaries ignore CWD).
type Adapter struct {
	brPath     string
	projectDir string // cmd.Dir for every br subprocess; empty = inherit process CWD

	// terminalMu serializes all terminal-transition writes (claim/close/reopen/reset/sweep-close).
	// Concurrent `br` writes from multiple daemon goroutines can exhaust the SQLite .write.lock
	// (30s busy timeout) when N completions fire simultaneously, causing "OpenWrite could not open
	// storage cursor root page" failures and beads stuck in_progress (hk-hdbls).
	// Reads (ShowBead, ListByStatus, etc.) are NOT gated — only writes are serialized.
	terminalMu sync.Mutex
}

// New returns an Adapter that invokes the `br` binary at brPath with no fixed
// working directory (subprocess inherits the process CWD).
//
// Test callers that use mock `br` binaries (which ignore CWD) SHOULD use New.
// Production callers that need `br` to discover the correct .beads database
// MUST use NewForProject instead.
//
// brPath MUST be a non-empty absolute path; New does NOT resolve from PATH.
// Returns nil and an error if brPath is empty.
func New(brPath string) (*Adapter, error) {
	if brPath == "" {
		return nil, errors.New("brcli.New: brPath must be non-empty; production callers must resolve br from PATH at startup")
	}
	return &Adapter{brPath: brPath}, nil
}

// NewForProject returns an Adapter that invokes the `br` binary at brPath
// with cmd.Dir set to projectDir for every subprocess.
//
// `br` discovers the .beads database by walking up from its working directory.
// Without a pinned CWD, `br` walks from whatever directory harmonik was
// launched from (often /, /tmp, or a developer's home — never the project
// root). NewForProject pins cmd.Dir so that `br` always finds the .beads
// database under projectDir, regardless of where the operator started harmonik.
//
// Production callers MUST use NewForProject.  Test callers that use mock `br`
// binaries (which do not walk for .beads) MAY use New.
//
// brPath MUST be a non-empty absolute path.  projectDir MUST be a non-empty
// absolute path to the harmonik project root.
// Returns nil and an error if either argument is empty.
func NewForProject(brPath, projectDir string) (*Adapter, error) {
	if brPath == "" {
		return nil, errors.New("brcli.NewForProject: brPath must be non-empty; production callers must resolve br from PATH at startup")
	}
	if projectDir == "" {
		return nil, errors.New("brcli.NewForProject: projectDir must be non-empty; production callers must supply the project root directory")
	}
	return &Adapter{brPath: brPath, projectDir: projectDir}, nil
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
//   - BI-025e concurrency discipline → RunWithDBLockedRetry (dblockretry.go, hk-872.32)
//
// BI-025a exit-code taxonomy IS implemented here: Result.BrErr is populated
// via BrErrorFromExitCode on every subprocess outcome (zero and non-zero exits).
//
// Higher-level methods built on Run will add the remaining layers.
func (a *Adapter) Run(ctx context.Context, args ...string) (Result, error) {
	// NOTE(hk-872.30): timeout discipline is in RunWithTimeout (timeout.go); Run is the
	// low-level primitive used by higher-level methods that add their own timeout wrapping.
	// NOTE(hk-872.32): terminal-transition writes are serialized via Adapter.terminalMu
	// (hk-hdbls); reads are concurrent. On BrDbLocked, callers MUST use
	// RunWithDBLockedRetry (dblockretry.go) which implements the BI-025c retry policy.

	//nolint:gosec // G204: brPath is resolved from PATH at startup by the production caller; args are typed harmonik-internal values, not user input.
	cmd := exec.CommandContext(ctx, a.brPath, args...)

	// Pin the working directory to the project root so that `br` discovers the
	// .beads database under projectDir regardless of where the harmonik process
	// was launched.  When projectDir is empty (test callers using New) the field
	// is left unset and the subprocess inherits the process CWD.
	if a.projectDir != "" {
		cmd.Dir = a.projectDir
	}

	var stdoutBuf bytes.Buffer
	stderrCap := newStderrCapWriter()
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = stderrCap

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
				Stderr:   stderrCap.Result().Bytes,
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
		Stderr:   stderrCap.Result().Bytes,
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
