package brcli

import (
	"context"
	"fmt"
	"strings"
)

// CheckBrRunnable invokes `br --version` and reports whether the `br` binary is
// present and runnable. It returns the trimmed stdout of that invocation, which
// callers MAY log so an operator can see which `br` the daemon found.
//
// The check asserts NOTHING about the version `br` reports. Any output at all is
// accepted as long as the binary executes and exits zero. The daemon needs `br`
// to exist; it does not need `br` to be a particular build.
//
// Spec ref: specs/beads-integration.md §4.8a BI-024a.
//
// Error semantics:
//   - Exec failure launching `br` → wrapped exec error (no sentinel)
//   - Non-zero `br` exit          → wraps BrUnavailable
//
// BrUnavailable is the sentinel because that is what the condition is: `br` is
// on disk but harmonik cannot get an answer out of it. The retired version of
// this check wrapped BrSchemaMismatch, which named a schema conclusion it had no
// evidence for. Real schema breakage still reaches BrSchemaMismatch, at call
// time, per §6.1a.
//
// The caller MUST treat either error as a startup-blocking failure: translate it
// to daemon startup exit code 8 and emit
// daemon_startup_failed{failure_mode="br-unavailable"}.
//
// # Why no version comparison
//
// This check used to parse the version out of the `br --version` banner and
// compare it against a pinned constant. Both the parse and the comparison are
// gone, by operator direction (2026-08-04).
//
// The comparison never earned its place. The fleet executed 754 runs on br 0.2.10
// while the pin said 0.1.45 (2026-05-19 through 2026-06-23), and no `br` call
// failed in that window. The pin was the sole cause of every daemon restart
// failure over the same period.
//
// The parse was worse than the comparison. A `br` build whose banner the regex
// did not like blocked daemon startup outright, and no downstream code consumed
// the parsed value once the comparison was demoted to a warning. A parse that can
// fail is a parse that can block, so the raw banner is returned for logging
// instead.
func (a *Adapter) CheckBrRunnable(ctx context.Context) (string, error) {
	result, err := a.Run(ctx, "--version")
	if err != nil {
		return "", fmt.Errorf("brcli.CheckBrRunnable: exec failed: %w", err)
	}

	if result.ExitCode != 0 {
		return "", fmt.Errorf(
			"brcli.CheckBrRunnable: br --version exited %d: %w",
			result.ExitCode,
			BrUnavailable,
		)
	}

	return strings.TrimSpace(string(result.Stdout)), nil
}
