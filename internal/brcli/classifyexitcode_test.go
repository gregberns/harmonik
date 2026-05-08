package brcli_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
)

// b87228FixtureMockBinary writes a shell script that exits with the given code
// and prints nothing. Used by exit-code classification tests.
func b87228FixtureMockBinary(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("b87228FixtureMockBinary: write mock: %v", err)
	}
	return path
}

// b87228FixtureAdapter returns an Adapter pointed at the given brPath.
func b87228FixtureAdapter(t *testing.T, brPath string) *brcli.Adapter {
	t.Helper()
	a, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("b87228FixtureAdapter: New: %v", err)
	}
	return a
}

// b87228FixtureSpecTable returns the §6.1a illustrative exit-code → BrError
// mapping entries. BrUnavailable is excluded because it is not reachable via
// exit code (it is assigned on timeout / exec error, not by the subprocess's
// exit code).
func b87228FixtureSpecTable() []struct {
	code  int
	want  brcli.BrError
	label string
} {
	return []struct {
		code  int
		want  brcli.BrError
		label string
	}{
		{0, brcli.BrOK, "exit-0-ok"},
		{1, brcli.BrNotFound, "exit-1-not-found"},
		{2, brcli.BrConflict, "exit-2-conflict"},
		{3, brcli.BrDbLocked, "exit-3-db-locked"},
		{4, brcli.BrSchemaMismatch, "exit-4-schema-mismatch"},
	}
}

// TestB87228ResultBrErrSpecTable verifies that Result.BrErr is populated with
// the correct BrError value for every §6.1a illustrative exit code when Run is
// called. This is the primary sensor for BI-025a exit-code classification.
//
// Spec ref: specs/beads-integration.md §6.1a, §4.8a BI-025a.
func TestB87228ResultBrErrSpecTable(t *testing.T) {
	for _, tc := range b87228FixtureSpecTable() {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			path := b87228FixtureMockBinary(t, tc.code)
			a := b87228FixtureAdapter(t, path)

			result, err := a.Run(context.Background())
			if err != nil {
				t.Fatalf("Run(exit %d): unexpected error: %v", tc.code, err)
			}
			if result.ExitCode != tc.code {
				t.Errorf("ExitCode = %d; want %d", result.ExitCode, tc.code)
			}
			if result.BrErr != tc.want {
				t.Errorf("BrErr = %q; want %q", result.BrErr, tc.want)
			}
		})
	}
}

// TestB87228ResultBrErrUnknownExitCodes verifies that unrecognized exit codes
// produce BrOther per BI-025a. Callers observing BrOther MUST emit
// divergence_inconclusive per event-model.md §8.6.10 with
// reason=authority_unavailable.
//
// Spec ref: specs/beads-integration.md §6.1a ("other → Other: unrecognized;
// emits store_divergence_detected per BI-025a"), §4.8a BI-025a.
func TestB87228ResultBrErrUnknownExitCodes(t *testing.T) {
	// Codes 5–8 and 127 are outside the §6.1a table (exit 127 is shell "command
	// not found"; exit 8 is harmonik's own beads-unavailable daemon exit, not a
	// br exit code). All must classify as BrOther.
	unknownCodes := []int{5, 6, 7, 8, 127}
	for _, code := range unknownCodes {
		code := code
		t.Run(fmt.Sprintf("exit-%d-other", code), func(t *testing.T) {
			path := b87228FixtureMockBinary(t, code)
			a := b87228FixtureAdapter(t, path)

			result, err := a.Run(context.Background())
			if err != nil {
				t.Fatalf("Run(exit %d): unexpected error: %v", code, err)
			}
			if result.BrErr != brcli.BrOther {
				t.Errorf("exit %d: BrErr = %q; want BrOther", code, result.BrErr)
			}
		})
	}
}

// TestB87228ResultBrErrNeverUnavailableFromExitCode confirms that BrUnavailable
// is NEVER produced by exit-code classification: it is assigned exclusively on
// timeout / SIGTERM-SIGKILL (RunWithTimeout) or exec error, not by any subprocess
// exit code.
//
// Spec ref: specs/beads-integration.md §6.1a (BrUnavailable row: "(timeout)" and
// "(exec error)", NOT a numeric exit code).
func TestB87228ResultBrErrNeverUnavailableFromExitCode(t *testing.T) {
	// Scan all spec-listed codes plus several unknown codes.
	codes := []int{0, 1, 2, 3, 4, 5, 127}
	for _, code := range codes {
		code := code
		t.Run(fmt.Sprintf("exit-%d-not-unavailable", code), func(t *testing.T) {
			path := b87228FixtureMockBinary(t, code)
			a := b87228FixtureAdapter(t, path)

			result, err := a.Run(context.Background())
			if err != nil {
				t.Fatalf("Run(exit %d): unexpected error: %v", code, err)
			}
			if result.BrErr == brcli.BrUnavailable {
				t.Errorf(
					"exit %d: BrErr = BrUnavailable; BrUnavailable must never be "+
						"produced by exit-code classification (only by timeout/exec-error)",
					code,
				)
			}
		})
	}
}

// TestB87228ExecErrorLeavesZeroBrErr confirms that when Run cannot launch the
// subprocess (exec error), it returns a non-nil error and a zero-value Result
// (BrErr is the zero value, not BrUnavailable or any other BrError constant).
// Exec-level failures are the caller's responsibility to classify; they are
// outside the exit-code taxonomy.
//
// Spec ref: specs/beads-integration.md §6.1a (exec error path is distinct from
// the exit-code table; classified by caller, not by BrErrorFromExitCode).
func TestB87228ExecErrorLeavesZeroBrErr(t *testing.T) {
	a := b87228FixtureAdapter(t, "/nonexistent/br")

	result, err := a.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	// BrErr must be the zero value — no subprocess ran so no exit-code
	// classification occurred.
	if result.BrErr != "" {
		t.Errorf("BrErr = %q on exec error; want zero value (empty string)", result.BrErr)
	}
}

// TestB87228RunWithTimeoutBrErrSpecTable verifies that RunWithTimeout also
// populates Result.BrErr correctly for every §6.1a illustrative exit code.
// RunWithTimeout delegates to classifyWaitResult for normal subprocess exits,
// so this test validates that the BrErr wire-up survives the timeout path.
//
// Spec ref: specs/beads-integration.md §6.1a, §4.8a BI-025a, BI-025c.
func TestB87228RunWithTimeoutBrErrSpecTable(t *testing.T) {
	for _, tc := range b87228FixtureSpecTable() {
		tc := tc
		t.Run("timeout-path-"+tc.label, func(t *testing.T) {
			path := b87228FixtureMockBinary(t, tc.code)
			a := b87228FixtureAdapter(t, path)

			// Use generous timeout so the subprocess exits normally (not via SIGTERM).
			cfg := brcli.TimeoutConfig{}
			result, err := a.RunWithTimeout(context.Background(), cfg, brcli.CommandKindRead)
			if err != nil {
				t.Fatalf("RunWithTimeout(exit %d): unexpected error: %v", tc.code, err)
			}
			if result.BrErr != tc.want {
				t.Errorf("BrErr = %q; want %q", result.BrErr, tc.want)
			}
		})
	}
}

// TestB87228BrErrIsValid verifies that every BrErr value in a Result returned
// by Run is a declared BrError constant (Valid() == true). An invalid BrErr
// would indicate a classification bug.
func TestB87228BrErrIsValid(t *testing.T) {
	for _, tc := range b87228FixtureSpecTable() {
		tc := tc
		t.Run("valid-"+tc.label, func(t *testing.T) {
			path := b87228FixtureMockBinary(t, tc.code)
			a := b87228FixtureAdapter(t, path)

			result, err := a.Run(context.Background())
			if err != nil {
				t.Fatalf("Run(exit %d): unexpected error: %v", tc.code, err)
			}
			if !result.BrErr.Valid() {
				t.Errorf("exit %d: BrErr = %q; BrErr.Valid() = false; must be a declared constant", tc.code, result.BrErr)
			}
		})
	}
}
