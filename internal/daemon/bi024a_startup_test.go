package daemon

// bi024a_startup_test.go — composition-root tests for the BI-024a explicit
// br --version handshake at daemon startup (hk-3pbox).
//
// BI-024a requires that daemon.Start invoke CheckBrVersion before accepting
// any queue-submit RPC and that a failure (exec error OR version mismatch)
// causes a fatal return with daemon_startup_failed{failure_mode=
// "br-version-incompatible"} emitted to the JSONL log.
//
// Spec ref: specs/beads-integration.md §4.8a BI-024a.
// Bead ref: hk-3pbox.

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// bi024aFixtureProjectDir creates a temp directory with the .harmonik/events
// sub-tree for JSONL logging.
func bi024aFixtureProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	projectDir = t.TempDir()
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("bi024aFixtureProjectDir: mkdir %s: %v", eventsDir, err)
	}
	jsonlPath = filepath.Join(eventsDir, "events.jsonl")
	return projectDir, jsonlPath
}

// bi024aFixtureReadJSONLLines reads all non-empty lines from the JSONL log
// at path, returning them as a slice of raw JSON strings.
func bi024aFixtureReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("bi024aFixtureReadJSONLLines: open %s: %v", path, err)
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

// bi024aSuccessAdapterFactory returns a br-adapter factory that constructs
// a real *brcli.Adapter pointing at a nonexistent binary path.  Construction
// succeeds (NewForProject does not verify the binary exists at construction
// time), but any subsequent Run call (including CheckBrVersion) will fail
// with an exec error, simulating an unavailable br binary.
func bi024aSuccessAdapterFactory() func(brPath, projectDir string) (*brcli.Adapter, error) {
	return func(_, _ string) (*brcli.Adapter, error) {
		// brcli.New succeeds for any non-empty path; the exec failure is
		// deferred to Run time, exercising CheckBrVersion's exec-failure branch.
		return brcli.New("/nonexistent/stub/br-bi024a")
	}
}

// TestDaemonStart_CheckBrVersion_ExecFail_FatalReturn verifies that when the
// br adapter factory returns a valid adapter but br is unavailable at exec
// time, daemon.Start returns a fatal (non-context-cancel) error and emits
// daemon_startup_failed{failure_mode="br-version-incompatible"} to the JSONL
// log.
//
// This exercises the BI-024a ordering guarantee: the handshake runs before
// any other bead operation or queue-submit acceptance.
//
// Spec ref: specs/beads-integration.md §4.8a BI-024a.
// Bead ref: hk-3pbox.
func TestDaemonStart_CheckBrVersion_ExecFail_FatalReturn(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := bi024aFixtureProjectDir(t)

	cfg := Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
		BrPath:       "/nonexistent/stub/br-bi024a", // non-empty so BrPath guard fires
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The adapter factory returns a valid *brcli.Adapter, so newBrAdapter
	// succeeds and the code reaches CheckBrVersion.  CheckBrVersion then tries
	// to exec the nonexistent binary, which fails → daemon.Start returns fatal.
	err := StartForTesting(ctx, cfg,
		WithBrAdapterFactory(bi024aSuccessAdapterFactory()),
	)

	// daemon.Start MUST return a non-nil error (fatal startup failure).
	if err == nil {
		t.Fatal("daemon.Start returned nil; want fatal error from CheckBrVersion exec failure (BI-024a)")
	}

	// The error must NOT be a context cancellation — it must be the BI-024a
	// fatal startup error.
	if strings.Contains(err.Error(), "context canceled") ||
		strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("daemon.Start returned context-cancel error %v; want explicit BI-024a fatal error", err)
	}

	// The JSONL log MUST contain daemon_startup_failed with the br-version-incompatible mode.
	lines := bi024aFixtureReadJSONLLines(t, jsonlPath)
	startupFailedType := string(core.EventTypeDaemonStartupFailed)
	found := false
	for _, line := range lines {
		if strings.Contains(line, startupFailedType) && strings.Contains(line, "br-version-incompatible") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("JSONL log does not contain %q event with failure_mode=br-version-incompatible; "+
			"BI-024a requires daemon_startup_failed emission on CheckBrVersion failure; log lines: %v",
			startupFailedType, lines)
	}
}
