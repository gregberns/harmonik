package daemon_test

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

func recwireupFixture378ReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(path)
	if err != nil {
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

func recwireupFixture378SchemaMismatchFactory() func(brPath, projectDir string) (*brcli.Adapter, error) {
	return func(_, _ string) (*brcli.Adapter, error) {
		return nil, fmt.Errorf("stub: schema version mismatch: %w", brcli.BrSchemaMismatch)
	}
}

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
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              "/stub/br", // non-empty so all 3 sites run; factory overrides NewForProject
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	cancel, done := recwireupFixture378StartDaemon(t, cfg,
		daemon.WithBrAdapterFactory(recwireupFixture378SchemaMismatchFactory()),
	)

	time.Sleep(200 * time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Fatalf("daemon.Start did not return within %s after context cancellation", daemon.ExportedDaemonExitHangBudget)
	}

	lines := recwireupFixture378ReadJSONLLines(t, jsonlPath)
	if len(lines) == 0 {
		t.Fatal("JSONL log has 0 lines after Start; want at least one event")
	}

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
// BrSchemaMismatch maps to RecCat0 → proceed queue-less.  The daemon
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
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              "/stub/br",
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	cancel, done := recwireupFixture378StartDaemon(t, cfg,
		daemon.WithBrAdapterFactory(recwireupFixture378SchemaMismatchFactory()),
	)
	defer cancel()

	time.Sleep(200 * time.Millisecond)

	cancel()
	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "context canceled") &&
			!strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("daemon.Start returned unexpected fatal error on BrSchemaMismatch: %v; "+
				"want nil or context-cancel (RecCat0 → proceed queue-less per BI-031b)", err)
		}
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Fatalf("daemon.Start did not return within %s after context cancellation", daemon.ExportedDaemonExitHangBudget)
	}
}
