package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

func brRunnableFixtureBootState(t *testing.T) (state *bootState, eventsJSONLPath string) {
	t.Helper()
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")

	writer, err := eventbus.OpenJSONLWriter(eventsPath)
	if err != nil {
		t.Fatalf("brRunnableFixtureBootState: OpenJSONLWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	return &bootState{bus: eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)}, eventsPath
}

func brRunnableFixtureMockBr(t *testing.T, stdout string, exitCode int) *brcli.Adapter {
	t.Helper()
	path := filepath.Join(t.TempDir(), "br")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %q\nexit %d\n", stdout, exitCode)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("brRunnableFixtureMockBr: write mock: %v", err)
	}
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("brRunnableFixtureMockBr: brcli.New: %v", err)
	}
	return adapter
}

func brRunnableFixtureStartupFailures(t *testing.T, eventsPath string) []core.DaemonStartupFailedPayload {
	t.Helper()
	raw, err := os.ReadFile(eventsPath) //nolint:gosec // G304: path is t.TempDir-derived
	if err != nil {
		t.Fatalf("brRunnableFixtureStartupFailures: read events: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	found := make([]core.DaemonStartupFailedPayload, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var envelope struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("brRunnableFixtureStartupFailures: unmarshal envelope %q: %v", line, err)
		}
		if envelope.Type != string(core.EventTypeDaemonStartupFailed) {
			continue
		}
		var payload core.DaemonStartupFailedPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatalf("brRunnableFixtureStartupFailures: unmarshal payload: %v", err)
		}
		found = append(found, payload)
	}
	return found
}

// TestEnsureBrRunnablePassesForAnyVersionOutput pins the rule the 2026-08-04
// amendment introduced: startup does not read the version. A `br` that exits
// zero passes even when its banner names no version at all, and nothing is
// emitted.
func TestEnsureBrRunnablePassesForAnyVersionOutput(t *testing.T) {
	bs, eventsPath := brRunnableFixtureBootState(t)
	adapter := brRunnableFixtureMockBr(t, "0.0.0", 0)

	if err := bs.ensureBrRunnable(context.Background(), adapter); err != nil {
		t.Fatalf("ensureBrRunnable: unexpected error: %v", err)
	}

	if failures := brRunnableFixtureStartupFailures(t, eventsPath); len(failures) != 0 {
		t.Errorf("emitted %d daemon_startup_failed events, want 0: %+v", len(failures), failures)
	}
}

// TestEnsureBrRunnableEmitsBrUnavailableOnNonZeroExit pins the normative
// payload of BI-024a's failure path: exit code 8 and failure_mode
// "br-unavailable". The old value was "br-version-incompatible", which named a
// condition the check no longer looks for.
func TestEnsureBrRunnableEmitsBrUnavailableOnNonZeroExit(t *testing.T) {
	bs, eventsPath := brRunnableFixtureBootState(t)
	adapter := brRunnableFixtureMockBr(t, "", 1)

	err := bs.ensureBrRunnable(context.Background(), adapter)
	if err == nil {
		t.Fatal("ensureBrRunnable: expected a startup-blocking error, got nil")
	}
	if !errors.Is(err, brcli.BrUnavailable) {
		t.Errorf("error should wrap the adapter's BrUnavailable sentinel; got %v", err)
	}

	failures := brRunnableFixtureStartupFailures(t, eventsPath)
	if len(failures) != 1 {
		t.Fatalf("emitted %d daemon_startup_failed events, want 1", len(failures))
	}
	if failures[0].ExitCode != 8 {
		t.Errorf("ExitCode = %d, want 8", failures[0].ExitCode)
	}
	if failures[0].FailureMode != "br-unavailable" {
		t.Errorf("FailureMode = %q, want %q", failures[0].FailureMode, "br-unavailable")
	}
}

// TestEnsureBrRunnableEmitsBrUnavailableWhenBrCannotRun covers the condition
// the check exists for: `br` is not there, so the daemon cannot reach the bead
// ledger at all.
func TestEnsureBrRunnableEmitsBrUnavailableWhenBrCannotRun(t *testing.T) {
	bs, eventsPath := brRunnableFixtureBootState(t)
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	if err := bs.ensureBrRunnable(context.Background(), adapter); err == nil {
		t.Fatal("ensureBrRunnable: expected a startup-blocking error, got nil")
	}

	failures := brRunnableFixtureStartupFailures(t, eventsPath)
	if len(failures) != 1 {
		t.Fatalf("emitted %d daemon_startup_failed events, want 1", len(failures))
	}
	if failures[0].FailureMode != "br-unavailable" {
		t.Errorf("FailureMode = %q, want %q", failures[0].FailureMode, "br-unavailable")
	}
}
