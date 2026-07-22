package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestDaemonStart_GracefulShutdownWrittenToEventLog proves the shutdown
// landmark reaches the durable JSONL log before the writer is closed.
// Bead ref: hk-msrpw.
func TestDaemonStart_GracefulShutdownWrittenToEventLog(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath := pidfileFixtureProjectDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	}
	if err := daemon.Start(ctx, cfg); err != nil {
		t.Fatalf("daemon.Start after graceful cancellation: %v", err)
	}

	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("open event log: %v", err)
	}
	defer f.Close() //nolint:errcheck // read-only test fixture

	var shutdownPayloads []core.DaemonShutdownPayload
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event core.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode event-log line: %v", err)
		}
		if core.EventType(event.Type) != core.EventTypeDaemonShutdown {
			continue
		}
		var payload core.DaemonShutdownPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode daemon_shutdown payload: %v", err)
		}
		shutdownPayloads = append(shutdownPayloads, payload)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan event log: %v", err)
	}

	if len(shutdownPayloads) != 1 {
		t.Fatalf("daemon_shutdown event count = %d, want exactly 1", len(shutdownPayloads))
	}
	payload := shutdownPayloads[0]
	if payload.Mode != core.ShutdownModeGraceful {
		t.Errorf("daemon_shutdown mode = %q, want %q", payload.Mode, core.ShutdownModeGraceful)
	}
	if !payload.Valid() {
		t.Errorf("daemon_shutdown payload is invalid: %+v", payload)
	}
}
