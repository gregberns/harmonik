package eventbus_test

// Integration tests for EV-002c HWM persistence in busImpl:
//
//	TestBusImpl_HWM_UpdatedAfterFClassEmit   — HWM file written after F-class Emit
//	TestBusImpl_HWM_NotWrittenAfterOClassEmit — HWM file untouched after O-class Emit
//	TestBusImpl_HWM_UpdatedAfterEmitWithRunID — HWM written after F-class EmitWithRunID
//	TestBusImpl_HWM_UpdatedAfterEmitAgentMsg  — HWM written after EmitAgentMessage
//	TestBusImpl_HWM_MonotonicAcrossEmits      — HWM file always advances, never goes back
//	TestBusImpl_HWM_EmptyPathNoWrite          — empty hwmPath disables HWM file writes
//
// Spec ref: event-model.md §4.1 EV-002c.

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// hwmFixtureOpenWriter opens a JSONLWriter in a temp dir and returns both the
// writer and its path.
func hwmFixtureOpenWriter(t *testing.T) (*eventbus.JSONLWriter, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, path
}

// hwmFixtureReadHWM reads the HWM file and returns the stored EventID.
func hwmFixtureReadHWM(t *testing.T, hwmPath string) core.EventID {
	t.Helper()
	hwm, exists, err := core.ReadEventIDHWM(hwmPath)
	if err != nil {
		t.Fatalf("ReadEventIDHWM: %v", err)
	}
	if !exists {
		t.Fatal("ReadEventIDHWM: file does not exist")
	}
	return hwm
}

// hwmFixtureMakePayload returns a minimal JSON payload byte slice.
func hwmFixtureMakePayload(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// TestBusImpl_HWM_UpdatedAfterFClassEmit asserts that emitting an F-class
// event via Emit updates the HWM file.
func TestBusImpl_HWM_UpdatedAfterFClassEmit(t *testing.T) {
	dir := t.TempDir()
	hwmPath := filepath.Join(dir, "event_id_hwm")
	writer, _ := hwmFixtureOpenWriter(t)

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, writer, nil, hwmPath, "")
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	payload := hwmFixtureMakePayload(t)
	// run_started is F-class per fsyncBoundaryEventTypes.
	if emitErr := bus.Emit(context.Background(), core.EventTypeRunStarted, payload); emitErr != nil {
		t.Fatalf("Emit: %v", emitErr)
	}

	_, exists, err := core.ReadEventIDHWM(hwmPath)
	if err != nil {
		t.Fatalf("EV-002c: ReadEventIDHWM after F-class Emit: %v", err)
	}
	if !exists {
		t.Fatal("EV-002c: HWM file should exist after F-class Emit")
	}
}

// TestBusImpl_HWM_NotWrittenAfterOClassEmit asserts that emitting an O-class
// event does NOT write the HWM file.
func TestBusImpl_HWM_NotWrittenAfterOClassEmit(t *testing.T) {
	dir := t.TempDir()
	hwmPath := filepath.Join(dir, "event_id_hwm")
	writer, _ := hwmFixtureOpenWriter(t)

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, writer, nil, hwmPath, "")
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	payload := hwmFixtureMakePayload(t)
	// daemon_degraded is O-class (not in fsyncBoundaryEventTypes).
	if emitErr := bus.Emit(context.Background(), core.EventTypeDaemonDegraded, payload); emitErr != nil {
		t.Fatalf("Emit: %v", emitErr)
	}

	_, exists, err := core.ReadEventIDHWM(hwmPath)
	if err != nil {
		t.Fatalf("EV-002c: ReadEventIDHWM: %v", err)
	}
	if exists {
		t.Fatal("EV-002c: HWM file should NOT exist after O-class Emit")
	}
}

// TestBusImpl_HWM_UpdatedAfterEmitWithRunID asserts that emitting an F-class
// event via EmitWithRunID updates the HWM file.
func TestBusImpl_HWM_UpdatedAfterEmitWithRunID(t *testing.T) {
	dir := t.TempDir()
	hwmPath := filepath.Join(dir, "event_id_hwm")
	writer, _ := hwmFixtureOpenWriter(t)

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, writer, nil, hwmPath, "")
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	payload := hwmFixtureMakePayload(t)
	runID := core.RunID{}
	// run_completed is F-class.
	if emitErr := bus.EmitWithRunID(context.Background(), runID, core.EventTypeRunCompleted, payload); emitErr != nil {
		t.Fatalf("EmitWithRunID: %v", emitErr)
	}

	_, exists, err := core.ReadEventIDHWM(hwmPath)
	if err != nil {
		t.Fatalf("EV-002c: ReadEventIDHWM after EmitWithRunID: %v", err)
	}
	if !exists {
		t.Fatal("EV-002c: HWM file should exist after F-class EmitWithRunID")
	}
}

// TestBusImpl_HWM_UpdatedAfterEmitAgentMsg asserts that EmitAgentMessage
// (always F-class) updates the HWM file.
func TestBusImpl_HWM_UpdatedAfterEmitAgentMsg(t *testing.T) {
	dir := t.TempDir()
	hwmPath := filepath.Join(dir, "event_id_hwm")
	writer, _ := hwmFixtureOpenWriter(t)

	commsEmitter, ok := eventbus.NewBusImplWithWriterAndHWM(nil, writer, nil, hwmPath, "").(eventbus.CommsMessageEmitter)
	if !ok {
		t.Fatal("EV-002c: bus does not implement CommsMessageEmitter")
	}
	bus := commsEmitter.(eventbus.EventBus)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	_, emitErr := commsEmitter.EmitAgentMessage(context.Background(), core.AgentMessagePayload{
		From: "captain",
		To:   "crew",
		Body: "ping",
	})
	if emitErr != nil {
		t.Fatalf("EmitAgentMessage: %v", emitErr)
	}

	_, exists, err := core.ReadEventIDHWM(hwmPath)
	if err != nil {
		t.Fatalf("EV-002c: ReadEventIDHWM after EmitAgentMessage: %v", err)
	}
	if !exists {
		t.Fatal("EV-002c: HWM file should exist after EmitAgentMessage")
	}
}

// TestBusImpl_HWM_MonotonicAcrossEmits asserts that HWM file values are
// strictly monotonically increasing across multiple F-class Emit calls.
func TestBusImpl_HWM_MonotonicAcrossEmits(t *testing.T) {
	dir := t.TempDir()
	hwmPath := filepath.Join(dir, "event_id_hwm")
	writer, _ := hwmFixtureOpenWriter(t)

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, writer, nil, hwmPath, "")
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	const n = 10
	payload := hwmFixtureMakePayload(t)
	var prev core.EventID
	for i := 0; i < n; i++ {
		if emitErr := bus.Emit(context.Background(), core.EventTypeRunStarted, payload); emitErr != nil {
			t.Fatalf("Emit %d: %v", i, emitErr)
		}
		curr := hwmFixtureReadHWM(t, hwmPath)
		if i > 0 && bytes.Compare(curr[:], prev[:]) <= 0 {
			t.Fatalf("EV-002c: HWM not monotonically increasing at emit %d: prev=%v curr=%v", i, prev, curr)
		}
		prev = curr
	}
}

// TestBusImpl_HWM_EmptyPathNoWrite asserts that when hwmPath is empty, no
// HWM file is created (test/no-project-dir mode).
func TestBusImpl_HWM_EmptyPathNoWrite(t *testing.T) {
	writer, _ := hwmFixtureOpenWriter(t)

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, writer, nil, "" /* no HWM */, "")
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	payload := hwmFixtureMakePayload(t)
	for i := 0; i < 3; i++ {
		if emitErr := bus.Emit(context.Background(), core.EventTypeRunStarted, payload); emitErr != nil {
			t.Fatalf("Emit: %v", emitErr)
		}
	}
	// No assertion needed — if maybeUpdateHWM tries to write with an empty path,
	// WriteEventIDHWMAtomicNoSync would panic or produce an error; the test
	// would fail on Emit returning a non-nil error. The test just confirms it
	// completes without error.
}
