package daemon_test

// reconciliationcadence_rc020a_test.go — tests for the RC-020a scheduled
// detector cadence (dispatch point (c) per reconciliation/spec.md §4.3).
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a — "Scheduled cadence."
// Bead ref: hk-63oh.21.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// rc020aStubEmitter is a minimal Emitter implementation for RC-020a cadence testing.
type rc020aStubEmitter struct {
	mu     sync.Mutex
	events []rc020aEmittedEvent
}

type rc020aEmittedEvent struct {
	eventType core.EventType
	payload   []byte
}

func (e *rc020aStubEmitter) Emit(_ context.Context, t core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, rc020aEmittedEvent{eventType: t, payload: payload})
	return nil
}

func (e *rc020aStubEmitter) count(t core.EventType) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.events {
		if ev.eventType == t {
			n++
		}
	}
	return n
}

// TestRC020a_ReconciliationScanCadenceDefaultIsHourly verifies that
// ReconciliationScanCadenceDefault equals 1 hour (3600 seconds) per
// RC-020a and operator-nfr.md §4.3 (knob: reconciliation_scan_cadence).
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a.
// Spec ref: specs/operator-nfr/config-inventory.md §2.18.
// Bead ref: hk-63oh.21.
func TestRC020a_ReconciliationScanCadenceDefaultIsHourly(t *testing.T) {
	t.Parallel()

	const wantHourly = time.Hour
	if daemon.ReconciliationScanCadenceDefault != wantHourly {
		t.Errorf("ReconciliationScanCadenceDefault = %v, want %v (1 hour per RC-020a)", daemon.ReconciliationScanCadenceDefault, wantHourly)
	}
}

// TestRC020a_StartReconciliationScheduler_EmitsReconciliationStarted verifies
// that StartReconciliationScheduler emits reconciliation_started with
// trigger="scheduled-hourly" on each cadence tick.
//
// The test uses a very short interval (1 ms) so the first tick fires
// immediately. It cancels the context after observing the first event.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a.
// Bead ref: hk-63oh.21.
func TestRC020a_StartReconciliationScheduler_EmitsReconciliationStarted(t *testing.T) {
	t.Parallel()

	emitter := &rc020aStubEmitter{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	daemon.StartReconciliationScheduler(ctx, daemon.ReconciliationSchedulerConfig{
		ProjectDir: "", // no bead-ledger ops (BrPath empty)
		BrPath:     "",
		Interval:   1 * time.Millisecond, // fast tick for test
		Emitter:    emitter,
		LogWriter:  nil,
	})

	// Wait until at least one reconciliation_started event is emitted or timeout.
	deadline := time.After(3 * time.Second)
	for {
		if emitter.count(core.EventTypeReconciliationStarted) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("RC-020a: timed out waiting for reconciliation_started from scheduler")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Verify the payload contains trigger="scheduled-hourly".
	emitter.mu.Lock()
	var payloadRaw []byte
	for _, ev := range emitter.events {
		if ev.eventType == core.EventTypeReconciliationStarted {
			payloadRaw = ev.payload
			break
		}
	}
	emitter.mu.Unlock()

	var p core.ReconciliationStartedPayload
	if err := json.Unmarshal(payloadRaw, &p); err != nil {
		t.Fatalf("RC-020a: unmarshal reconciliation_started payload: %v", err)
	}
	if p.Trigger != core.ReconciliationTriggerScheduled {
		t.Errorf("RC-020a: scheduler emitted trigger=%q, want %q", p.Trigger, core.ReconciliationTriggerScheduled)
	}
}

// TestRC020a_DaemonStartEmitsStartupReconciliationStarted verifies that
// daemon.Start emits reconciliation_started{trigger:"startup"} after the
// orphan sweep (RC-020a dispatch point (a)).
//
// This test uses a JSONL log and checks for the event after daemon startup
// completes (context-cancelled path for unit-test mode without a real
// project directory).
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a.
// Bead ref: hk-63oh.21.
func TestRC020a_DaemonStartEmitsStartupReconciliationStarted(t *testing.T) {
	t.Parallel()

	// Use a temp project dir with .harmonik/events/ so the JSONL writer can open.
	projectDir := t.TempDir()
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	jsonlPath := filepath.Join(eventsDir, "events.jsonl")

	cfg := daemon.Config{
		ProjectDir:   projectDir,
		JSONLLogPath: jsonlPath,
		BrPath:       "", // no bead ledger → orphan sweep runs without bead ops
		// ReconciliationScanCadence: 0 → default (hourly, won't tick in 200ms test window)
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Start(ctx, cfg)
	}()

	// Allow the daemon startup path (pre-work-loop) to run and emit events.
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon.Start did not return within 5s after cancel")
	}

	// Read the JSONL log and look for reconciliation_started{trigger:"startup"}.
	//nolint:gosec // G304: jsonlPath is t.TempDir()-based
	f, err := os.Open(jsonlPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatal("JSONL log does not exist; daemon startup did not write any events")
		}
		t.Fatalf("open JSONL: %v", err)
	}
	defer func() { _ = f.Close() }()

	var foundStartup bool
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.Contains(line, string(core.EventTypeReconciliationStarted)) {
			continue
		}
		// Parse the envelope to check the trigger field.
		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		var payload core.ReconciliationStartedPayload
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			continue
		}
		if payload.Trigger == core.ReconciliationTriggerStartup {
			foundStartup = true
			break
		}
	}

	if !foundStartup {
		t.Errorf("RC-020a: no reconciliation_started{trigger:\"startup\"} event in JSONL log; " +
			"daemon.Start must emit this event after orphan sweep (dispatch point (a))")
	}
}
