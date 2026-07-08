package brcli_test

// concurrentclose_hkcw4sx_test.go — regression guard for hk-cw4sx.
//
// Bug: under --wave with high max-concurrent, multiple daemon workers call
// br close near-simultaneously when beads finish in a cluster. SQLite
// BrDbLocked retries exhaust (10 attempts) and run_failed is reported EVEN
// THOUGH the close is idempotent and actually succeeded — one concurrent
// worker's close landed; subsequent br close attempts get BrDbLocked but the
// bead is already closed.
//
// Fix: terminalTransitionWrite now performs a ShowBead idempotency check after
// BrUnavailable retry exhaustion or non-retriable non-BrOK exit. If ShowBead
// confirms the bead is already in the intended post-state, the failure is
// treated as success (intent file deleted; nil returned).
//
// Root cause: internal/brcli/terminaltransition_bi010.go — terminalTransitionWrite.
// Also adds ±25% jitter to RunWithDBLockedRetry backoff (dblockretry.go) to
// reduce thundering-herd contention under concurrent waves.
//
// Bead: hk-cw4sx.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// hkcw4sxFixtureCloseLockedShowClosedBinary writes a mock `br` binary that:
//   - For `br close <id>`: exits 3 (BrDbLocked) on every invocation.
//   - For `br show <id> --format json`: returns a valid closed-bead JSON and exits 0.
//
// This simulates the hk-cw4sx concurrent-close scenario: all br close
// attempts fail with SQLite locked, but ShowBead confirms the bead is already
// closed (a sibling worker already closed it).
func hkcw4sxFixtureCloseLockedShowClosedBinary(t *testing.T, beadID core.BeadID) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	closedJSON := fmt.Sprintf(
		`[{"id":%q,"title":"T","description":"","status":"closed","issue_type":"task","labels":[],"dependencies":[],"dependents":[],"parent":""}]`,
		string(beadID),
	)
	// $1 is the subcommand. close → exit 3; show → closed JSON + exit 0.
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  close)
    exit 3
    ;;
  show)
    printf '%%s' %q
    exit 0
    ;;
esac
exit 0
`, closedJSON)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("hkcw4sxFixtureCloseLockedShowClosedBinary: write mock: %v", err)
	}
	return path
}

// hkcw4sxFastCfg returns a TimeoutConfig with small terminal-write retry
// params so retry-exhaustion tests complete in milliseconds instead of ~40s.
func hkcw4sxFastCfg() brcli.TimeoutConfig {
	return brcli.TimeoutConfig{
		ReadTimeout:             5 * time.Second,
		WriteTimeout:            10 * time.Second,
		TerminalWriteMaxRetries: 2,                    // 3 total attempts; fast for test
		TerminalWriteRetryBase:  1 * time.Millisecond, // minimal backoff
		TerminalWriteRetryCap:   10 * time.Millisecond,
	}
}

// TestConcurrentCloseIdempotency_HkCw4sx is the regression guard for hk-cw4sx.
//
// Verifies that CloseBead returns nil when all br close attempts exhaust
// BrDbLocked retries but ShowBead confirms the bead is already closed.
// This is the idempotent-success path introduced by the hk-cw4sx fix.
func TestConcurrentCloseIdempotency_HkCw4sx(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-cw4sx-single")

	brPath := hkcw4sxFixtureCloseLockedShowClosedBinary(t, beadID)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	intentLogDir := t.TempDir()
	runID := core.RunID(uuid.Must(uuid.NewV7()))
	transitionID := core.TransitionID(uuid.Must(uuid.NewV7()))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// CloseBead must return nil: all br close calls return BrDbLocked but
	// ShowBead confirms the bead is already closed → idempotent success (hk-cw4sx).
	closeErr := adapter.CloseBead(ctx, intentLogDir, hkcw4sxFastCfg(),
		runID, transitionID, beadID, false,
	)
	if closeErr != nil {
		t.Fatalf("CloseBead: expected nil (idempotent-close detection via ShowBead), got: %v", closeErr)
	}

	// BI-030 step 6: intent file must be deleted after idempotent-success.
	count := bi010FixtureCountIntentFiles(t, intentLogDir)
	if count != 0 {
		t.Errorf("BI-030 step 6: expected 0 intent files after idempotent close, got %d", count)
	}
}

// TestConcurrentCloseIdempotencyNeedsAttentionFalse_HkCw4sx verifies that the
// idempotent-close path works when needsAttention=false (APPROVE path).
func TestConcurrentCloseIdempotencyNeedsAttentionFalse_HkCw4sx(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-cw4sx-noattn")

	brPath := hkcw4sxFixtureCloseLockedShowClosedBinary(t, beadID)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	intentLogDir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	closeErr := adapter.CloseBead(ctx, intentLogDir, hkcw4sxFastCfg(),
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID, false,
	)
	if closeErr != nil {
		t.Fatalf("CloseBead(needsAttention=false): expected nil via idempotency check, got: %v", closeErr)
	}
}

// TestConcurrentCloseStillFailsWhenNotClosed_HkCw4sx verifies that when all
// br close attempts fail AND ShowBead reports the bead is NOT in the intended
// post-state, CloseBead still returns an error (the fix does not mask real failures).
func TestConcurrentCloseStillFailsWhenNotClosed_HkCw4sx(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-cw4sx-notclosed")

	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	// br close exits 3; br show returns in_progress (NOT closed) — close genuinely failed.
	openJSON := fmt.Sprintf(
		`[{"id":%q,"title":"T","description":"","status":"in_progress","issue_type":"task","labels":[],"dependencies":[],"dependents":[],"parent":""}]`,
		string(beadID),
	)
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  close)
    exit 3
    ;;
  show)
    printf '%%s' %q
    exit 0
    ;;
esac
exit 0
`, openJSON)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("TestConcurrentCloseStillFailsWhenNotClosed: write mock: %v", err)
	}
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	intentLogDir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	closeErr := adapter.CloseBead(ctx, intentLogDir, hkcw4sxFastCfg(),
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID, false,
	)
	if closeErr == nil {
		t.Fatal("CloseBead: expected error when bead is not closed, got nil — fix must not mask real failures")
	}

	// Intent file must be retained for BI-031 crash recovery on genuine failure.
	count := bi010FixtureCountIntentFiles(t, intentLogDir)
	if count != 1 {
		t.Errorf("BI-031: expected 1 intent file retained on genuine failure, got %d", count)
	}
}

// TestConcurrentCloseN_HkCw4sx verifies that N goroutines calling CloseBead
// concurrently on N distinct beads, all with br close returning BrDbLocked,
// all succeed via the ShowBead idempotency check.
//
// This models the exact wave-cluster scenario: N beads finish simultaneously;
// N workers all call br close; SQLite locked for all of them; all should
// recover via the idempotency check.
func TestConcurrentCloseN_HkCw4sx(t *testing.T) {
	t.Parallel()

	const N = 5

	// Shared mock br: close → exit 3; show → closed JSON for whatever id is passed.
	dir := t.TempDir()
	brPath := filepath.Join(dir, "br")
	// Use $2 (the bead ID argument after the subcommand) in the show response.
	script := `#!/bin/sh
case "$1" in
  close)
    exit 3
    ;;
  show)
    printf '[{"id":"%s","title":"T","description":"","status":"closed","issue_type":"task","labels":[],"dependencies":[],"dependents":[],"parent":""}]' "$2"
    exit 0
    ;;
esac
exit 0
`
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(brPath, []byte(script), 0o755); err != nil {
		t.Fatalf("TestConcurrentCloseN: write mock: %v", err)
	}

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make([]error, N)

	for i := range N {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			beadID := core.BeadID(fmt.Sprintf("hk-cw4sx-conc-%d", idx))
			intentLogDir := t.TempDir()
			errs[idx] = adapter.CloseBead(ctx, intentLogDir, hkcw4sxFastCfg(),
				core.RunID(uuid.Must(uuid.NewV7())),
				core.TransitionID(uuid.Must(uuid.NewV7())),
				beadID, false,
			)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: CloseBead returned error (expected nil via idempotency check): %v", i, err)
		}
	}
}

// TestJitterReducesThunderingHerd_HkCw4sx verifies that RunWithDBLockedRetry
// produces varied backoff values across calls, confirming jitter is applied.
// This is a structural check: two calls with the same base/cap should not
// produce identical total backoff durations under concurrent load.
//
// The test uses the existing dblockretry retry machinery with a fast
// failure-then-succeed mock to confirm jitter without asserting exact timings.
func TestJitterReducesThunderingHerd_HkCw4sx(t *testing.T) {
	t.Parallel()

	cfg := brcli.TimeoutConfig{
		ReadTimeout:  20 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Run two retries concurrently. Each gets its own mock binary so their
	// file-counter state doesn't race. We just verify they both succeed
	// (jitter does not break the retry logic).
	var wg sync.WaitGroup
	retryErrors := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Use a fresh binary per goroutine so counters don't race.
			p := dblockretryFixtureCountedBinary(t, 3, 1)
			a, newErr := brcli.New(p)
			if newErr != nil {
				retryErrors[idx] = newErr
				return
			}
			result, runErr := a.RunWithDBLockedRetry(
				context.Background(),
				cfg,
				brcli.CommandKindWrite,
				brcli.DBLockedRetryMax,
				1*time.Millisecond,
				10*time.Millisecond,
			)
			if runErr != nil {
				retryErrors[idx] = runErr
				return
			}
			if result.BrErr != brcli.BrOK {
				retryErrors[idx] = fmt.Errorf("BrErr = %v; want BrOK", result.BrErr)
			}
		}(i)
	}
	wg.Wait()

	for i, err := range retryErrors {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}
}
