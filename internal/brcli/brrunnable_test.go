package brcli_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
)

func TestCheckBrRunnableAcceptsAWorkingBr(t *testing.T) {
	// Trailing whitespace is real here. brcliFixtureMockBinary quotes its stdout
	// through `sh printf '%s'`, which passes an escape like \n through as two
	// literal characters, so a space is the way to exercise the trim.
	path := brcliFixtureMockBinary(t, "br 0.5.2 ", "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	banner, err := adapter.CheckBrRunnable(context.Background())
	if err != nil {
		t.Fatalf("CheckBrRunnable: unexpected error: %v", err)
	}
	if banner != "br 0.5.2" {
		t.Errorf("banner = %q, want %q", banner, "br 0.5.2")
	}
}

// TestCheckBrRunnableAcceptsOutputTheOldVersionRegexRejected is the point of the
// change. The retired check parsed `br --version` with
// `br\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?` and blocked daemon startup
// when the banner did not match. A live `br` build reporting a banner the regex
// disliked bricked startup on 2026-08-04. Startup MUST now succeed on ANY output
// as long as `br` exits zero.
func TestCheckBrRunnableAcceptsOutputTheOldVersionRegexRejected(t *testing.T) {
	// Each of these fails the retired regex: no dotted triple, no `br` prefix,
	// or empty output altogether.
	cases := map[string]string{
		// The banner from the 2026-08-04 incident. Note that `br 0.0.0` WOULD
		// have satisfied the old regex — the build blocked startup because it
		// dropped the `br ` prefix, not because of the digits.
		"the 2026-08-04 incident banner": "0.0.0",

		"no version at all":     "not a version string at all",
		"bare word":             "beads",
		"two-component version": "br 0.2",
		"empty output":          "",
		"json banner":           `{"tool":"br","build":"dev"}`,
	}

	for name, output := range cases {
		t.Run(name, func(t *testing.T) {
			path := brcliFixtureMockBinary(t, output, "", 0)

			adapter, err := brcli.New(path)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			if _, err := adapter.CheckBrRunnable(context.Background()); err != nil {
				t.Fatalf("CheckBrRunnable must accept any output from a br that exits 0; got %v", err)
			}
		})
	}
}

func TestCheckBrRunnableRejectsNonZeroExit(t *testing.T) {
	path := brcliFixtureMockBinary(t, "", "error: unknown flag --version", 1)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.CheckBrRunnable(context.Background())
	if err == nil {
		t.Fatal("expected BrUnavailable for non-zero exit, got nil")
	}
	// BrUnavailable, not BrSchemaMismatch: `br` is on disk but harmonik cannot
	// get an answer out of it. Nothing here observed a schema.
	if !errors.Is(err, brcli.BrUnavailable) {
		t.Errorf("errors.Is(err, BrUnavailable) = false; got %v", err)
	}
	if errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("an unrunnable br MUST NOT claim a schema mismatch; got %v", err)
	}
}

// TestCheckBrRunnableRejectsAnUnexecutableBr covers the case the check exists
// for: `br` is absent from the configured path, so the daemon cannot reach the
// bead ledger at all.
func TestCheckBrRunnableRejectsAnUnexecutableBr(t *testing.T) {
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.CheckBrRunnable(context.Background())
	if err == nil {
		t.Fatal("expected error for exec failure, got nil")
	}
	// Exec failure wraps no BrError sentinel — the binary could not be launched,
	// so there is no invocation to classify.
	if errors.Is(err, brcli.BrSchemaMismatch) {
		t.Error("exec failure should NOT wrap BrSchemaMismatch")
	}
}
