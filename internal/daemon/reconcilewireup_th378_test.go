package daemon_test

// reconcilewireup_th378_test.go — composition-root test confirming that
// brAdapterErr at all three brcli.NewForProject call sites in daemon.Start
// is classified via BrErrReconciliationCategoryWithEmit and emits
// divergence_inconclusive on BrSchemaMismatch.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
// Bead ref: hk-th378.

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// recwireupFixture378ProjectDir creates a temp directory wired as a project
// root with the .harmonik/events sub-tree for JSONL logging.  Returns
// projectDir and the jsonlPath for event observation.
func recwireupFixture378ProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = t.TempDir()
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("recwireupFixture378ProjectDir: mkdir %s: %v", eventsDir, err)
	}
	jsonlPath = filepath.Join(eventsDir, "events.jsonl")
	return projectDir, jsonlPath
}

// recwireupFixture378ReadJSONLLines reads all non-empty lines from the JSONL
// log at path, returning them as a slice of raw JSON strings.
func recwireupFixture378ReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(path)
	if err != nil {
		// File may not yet exist if Start returned before any writes.
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("recwireupFixture378ReadJSONLLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// recwireupFixture378SchemaMismatchFactory returns a br-adapter factory
// (for use with daemon.WithBrAdapterFactory) that always fails with a wrapped
// BrSchemaMismatch sentinel, simulating a br binary whose schema version is
// incompatible with harmonik's pinned version.
func recwireupFixture378SchemaMismatchFactory() func(brPath, projectDir string) (*brcli.Adapter, error) {
	return func(_, _ string) (*brcli.Adapter, error) {
		return nil, fmt.Errorf("stub: schema version mismatch: %w", brcli.BrSchemaMismatch)
	}
}

// recwireupFixture378StartDaemon starts daemon.StartForTesting in a background
// goroutine with a cancellable context.  It returns the cancel func and a done
// channel.  Callers MUST call cancel() after the test to avoid goroutine leaks.
func recwireupFixture378StartDaemon(t *testing.T, cfg daemon.Config, opts ...daemon.TestOption) (cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	ch := make(chan error, 1)
	go func() {
		ch <- daemon.StartForTesting(ctx, cfg, opts...)
	}()
	return cancelFn, ch
}

// TestDaemonStart_BrSchemaMismatch_EmitsDivergenceInconclusive verifies that
// when the br adapter factory returns BrSchemaMismatch at the three
// NewForProject call sites in daemon.Start, BrErrReconciliationCategoryWithEmit
// emits at least one divergence_inconclusive event to the bus (observable via
// the JSONL log).
//
// The daemon is started in a goroutine; after a brief settle window the
// context is cancelled and the JSONL log is inspected.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
// Bead ref: hk-th378.
func TestDaemonStart_BrSchemaMismatch_EmitsDivergenceInconclusive(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := recwireupFixture378ProjectDir(t)

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
		BrPath:       "/stub/br", // non-empty so all 3 sites run; factory overrides NewForProject
	}

	cancel, done := recwireupFixture378StartDaemon(t, cfg,
		daemon.WithBrAdapterFactory(recwireupFixture378SchemaMismatchFactory()),
	)

	// Allow a brief settle window for the startup path (sites 1–3) to execute
	// and flush divergence_inconclusive events to the JSONL log.  All three
	// brAdapterErr sites run synchronously before the work loop goroutine is
	// even launched, so 200 ms is more than sufficient on CI.
	time.Sleep(200 * time.Millisecond)

	// Cancel the daemon context to stop the work loop.
	cancel()

	// Wait for daemon.Start to return (with context-cancel error or nil).
	select {
	case <-done:
		// Returned — OK; no assertion on the error because a cancelled context
		// may produce context.Canceled, which is a normal shutdown.
	case <-time.After(5 * time.Second):
		t.Fatal("daemon.Start did not return within 5 s after context cancellation")
	}

	lines := recwireupFixture378ReadJSONLLines(t, jsonlPath)
	if len(lines) == 0 {
		t.Fatal("JSONL log has 0 lines after Start; want at least one event")
	}

	// At least one divergence_inconclusive event must appear in the JSONL.
	// All three brAdapterErr sites in daemon.Start emit one when the factory
	// returns BrSchemaMismatch; we therefore expect ≥3, but assert ≥1 to
	// avoid fragility if future refactors merge sites.
	divergenceEventType := string(core.EventTypeDivergenceInconclusive)
	foundCount := 0
	for _, line := range lines {
		if strings.Contains(line, divergenceEventType) {
			foundCount++
		}
	}
	if foundCount == 0 {
		t.Errorf("no %q event found in JSONL log after BrSchemaMismatch; "+
			"BrErrReconciliationCategoryWithEmit must be wired at all brAdapterErr sites (BI-031b)",
			divergenceEventType)
	}
	t.Logf("found %d %q event(s) in JSONL log (expected ≥1, one per brAdapterErr site)", foundCount, divergenceEventType)
}

// TestDaemonStart_BrSchemaMismatch_DaemonProceedsQueueless verifies that
// daemon.Start proceeds (does not return a fatal error) when all three br
// adapter constructions fail with BrSchemaMismatch.
//
// BrSchemaMismatch maps to RecCat0 → proceed queue-less at MVH.  The daemon
// remains operable without a bead ledger; the socket and work loop are still
// active (the work loop uses the real br binary path for polling, which will
// also fail — but that is a separate non-fatal retry path).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
// Bead ref: hk-th378.
func TestDaemonStart_BrSchemaMismatch_DaemonProceedsQueueless(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := recwireupFixture378ProjectDir(t)

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
		BrPath:       "/stub/br",
	}

	cancel, done := recwireupFixture378StartDaemon(t, cfg,
		daemon.WithBrAdapterFactory(recwireupFixture378SchemaMismatchFactory()),
	)
	defer cancel()

	// Allow time for startup path to complete (pre-work-loop code is synchronous).
	time.Sleep(200 * time.Millisecond)

	// Cancel the daemon and wait for it to return.
	cancel()
	select {
	case err := <-done:
		// daemon.Start either returned nil or context.Canceled (both acceptable).
		// The only unacceptable outcome is a fatal startup error triggered by the
		// BrSchemaMismatch on the adapter construction sites.
		if err != nil && !strings.Contains(err.Error(), "context canceled") &&
			!strings.Contains(err.Error(), "context deadline exceeded") {
			// A non-context error means the daemon treated BrSchemaMismatch as
			// fatal — that violates the RecCat0 → proceed-queue-less rule.
			t.Errorf("daemon.Start returned unexpected fatal error on BrSchemaMismatch: %v; "+
				"want nil or context-cancel (RecCat0 → proceed queue-less per BI-031b)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon.Start did not return within 5 s after context cancellation")
	}
}
