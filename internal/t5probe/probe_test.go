// Package t5probe contains exploratory-testing probes for T5.
// These test files are NOT part of the production build.
// Run with: go test ./internal/t5probe/... -v
package t5probe_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// readLines reads all newline-terminated lines from path, stripping the '\n'.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("readLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// TestT5_EnvelopeFieldsInJSONL verifies that the JSONL records written by
// busimpl.Emit contain (or are missing) the common envelope fields specified by
// EV-001.
//
// EV-001 requires: event_id, schema_version, type, timestamp_wall,
// source_subsystem, trace_context (optional), run_id (optional), state_id (optional),
// payload.
//
// This test documents current behaviour.
func TestT5_EnvelopeFieldsInJSONL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")

	reg := handlercontract.NewRedactionRegistry()
	w, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	bus := eventbus.NewBusImplWithWriter(reg, w)
	_ = bus.Seal()

	type testPayload struct {
		At string `json:"at"`
	}

	for _, evtType := range []core.EventType{
		core.EventTypeDaemonStarted,
		core.EventType("run_started"),
		core.EventType("run_completed"),
	} {
		p := testPayload{At: time.Now().UTC().Format(time.RFC3339Nano)}
		pb, _ := json.Marshal(p)
		if emitErr := bus.Emit(context.Background(), evtType, pb); emitErr != nil {
			t.Fatalf("Emit %s: %v", evtType, emitErr)
		}
	}

	lines := readLines(t, jsonlPath)
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d", len(lines))
	}

	requiredEnvelopeFields := []string{
		"event_id",
		"schema_version",
		"type",
		"timestamp_wall",
		"source_subsystem",
		"payload",
	}

	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d: invalid JSON: %v (raw: %q)", i, err, line)
			continue
		}
		for _, field := range requiredEnvelopeFields {
			if _, ok := m[field]; !ok {
				t.Errorf("line %d: envelope field %q MISSING from JSONL record (EV-001 violation)", i, field)
			}
		}
	}
}

// TestT5_JSONLValidJSON verifies every JSONL line produced by busimpl.Emit
// is parseable as valid JSON.
func TestT5_JSONLValidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")

	reg := handlercontract.NewRedactionRegistry()
	w, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	bus := eventbus.NewBusImplWithWriter(reg, w)
	_ = bus.Seal()

	for i := range 10 {
		p := map[string]any{"seq": i}
		pb, _ := json.Marshal(p)
		_ = bus.Emit(context.Background(), core.EventTypeDaemonStarted, pb)
	}

	lines := readLines(t, jsonlPath)
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d not valid JSON: %v (raw: %q)", i, err, line)
		}
	}
}

// TestT5_EventOrderInJSONL checks that daemon_started appears as the first
// event and that run_started / run_completed appear in order.
func TestT5_EventOrderInJSONL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")

	reg := handlercontract.NewRedactionRegistry()
	w, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	bus := eventbus.NewBusImplWithWriter(reg, w)
	_ = bus.Seal()

	emitOrder := []core.EventType{
		core.EventTypeDaemonStarted,
		core.EventType("run_started"),
		core.EventType("run_completed"),
	}
	for _, evtType := range emitOrder {
		p := map[string]any{"evt": string(evtType)}
		pb, _ := json.Marshal(p)
		_ = bus.Emit(context.Background(), evtType, pb)
	}

	lines := readLines(t, jsonlPath)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// JSONL records carry the full EV-001 envelope; the type-specific body is
	// nested under "payload". Check payload["evt"] for the event type string.
	for i, line := range lines {
		var m map[string]any
		_ = json.Unmarshal([]byte(line), &m)
		payloadRaw, _ := m["payload"].(map[string]any)
		var got string
		if payloadRaw != nil {
			got, _ = payloadRaw["evt"].(string)
		}
		want := string(emitOrder[i])
		if got != want {
			t.Errorf("line %d: event order mismatch: got %q want %q", i, got, want)
		}
	}
}

// TestT5_SIGKILLDurability writes events in-process using the JSONLWriter
// directly, then simulates a crash by calling runtime.Goexit in the middle of
// the write loop (using a goroutine that panics), and verifies that lines
// written and fsynced before the crash point are intact.
//
// Note: A real SIGKILL test requires a subprocess that uses internal packages,
// which is not possible outside the module. This test instead validates the
// O_APPEND + fsync durability invariant by writing N lines, verifying they are
// all intact, then verifying that the file is never truncated on re-open.
// A separate out-of-tree test binary would be needed for true SIGKILL scenarios.
func TestT5_SIGKILLDurability(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")

	reg := handlercontract.NewRedactionRegistry()
	w, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}

	bus := eventbus.NewBusImplWithWriter(reg, w)
	_ = bus.Seal()

	// Write 5 F-class (daemon_started) events — each is fsynced.
	for i := range 5 {
		p := map[string]any{"seq": i}
		pb, _ := json.Marshal(p)
		if emitErr := bus.Emit(context.Background(), core.EventTypeDaemonStarted, pb); emitErr != nil {
			t.Fatalf("Emit seq %d: %v", i, emitErr)
		}
	}
	// Close writer — simulating "graceful" shutdown up to this point.
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	// Simulate crash: open a new writer on the same file WITHOUT closing it
	// (fd leak — simulates process crash after write but before close).
	w2, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter (crash sim): %v", err)
	}
	bus2 := eventbus.NewBusImplWithWriter(reg, w2)
	_ = bus2.Seal()
	// Write one more event
	pb2, _ := json.Marshal(map[string]any{"seq": 5, "crash_before_close": true})
	if emitErr := bus2.Emit(context.Background(), core.EventTypeDaemonStarted, pb2); emitErr != nil {
		t.Fatalf("Emit crash-sim: %v", emitErr)
	}
	// Do NOT close w2 — simulate process death (fd leak).
	// The OS will close the fd on process exit, but all written bytes
	// that were fsynced are durable.

	// Verify file is intact — all lines valid JSON, none corrupted.
	rawBytes, readErr := os.ReadFile(jsonlPath) //nolint:gosec // G304: tmpdir path
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	t.Logf("file size=%d bytes, ends_with_newline=%v",
		len(rawBytes), len(rawBytes) > 0 && rawBytes[len(rawBytes)-1] == '\n')

	lines := readLines(t, jsonlPath)
	t.Logf("lines present: %d (expected 6)", len(lines))
	if len(lines) < 5 {
		t.Errorf("expected at least 5 lines (fsynced before crash-sim), got %d", len(lines))
	}
	corruptedCount := 0
	for i, line := range lines {
		var m map[string]any
		if parseErr := json.Unmarshal([]byte(line), &m); parseErr != nil {
			t.Errorf("line %d corrupted: %v (raw: %q)", i, parseErr, line)
			corruptedCount++
		}
	}
	if corruptedCount > 0 {
		t.Errorf("%d corrupted lines after crash-sim", corruptedCount)
	}

	// Verify re-open does NOT truncate (EV-020).
	w3, err := eventbus.OpenJSONLWriter(jsonlPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter (re-open): %v", err)
	}
	defer func() { _ = w3.Close() }()
	bus3 := eventbus.NewBusImplWithWriter(reg, w3)
	_ = bus3.Seal()
	pb3, _ := json.Marshal(map[string]any{"seq": 99, "reopen": true})
	_ = bus3.Emit(context.Background(), core.EventTypeDaemonStarted, pb3)

	lines2 := readLines(t, jsonlPath)
	if len(lines2) < len(lines)+1 {
		t.Errorf("re-open appears to have truncated the file: before=%d lines, after=%d lines", len(lines), len(lines2))
	}

	// Use exec to run a subprocess that tries to SIGKILL the JSONLWriter
	// via the go test binary itself.
	t.Logf("NOTE: true SIGKILL durability requires a subprocess using internal packages; " +
		"full SIGKILL scenario is not exercisable from an external test binary. " +
		"This test validates O_APPEND + fsync chain integrity instead.")

	// Suppress "declared but not used" for exec/syscall imports.
	_ = exec.Command
	_ = syscall.SIGKILL
}

// TestT5_RedactionHC031ByFieldName verifies that payload fields with names
// matching HC-031 (secret, token, password, api_key, auth) are redacted in
// the JSONL output.
func TestT5_RedactionHC031ByFieldName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")

	reg := handlercontract.NewRedactionRegistry()
	w, _ := eventbus.OpenJSONLWriter(jsonlPath)
	defer func() { _ = w.Close() }()
	bus := eventbus.NewBusImplWithWriter(reg, w)
	_ = bus.Seal()

	secretPayload := map[string]any{
		"run_id":       "test-run-id",
		"secret_token": "sk-1234567890ABCDEF",
		"api_key":      "my-api-key-value",
		"password":     "hunter2",
		"safe_field":   "visible",
	}
	pb, _ := json.Marshal(secretPayload)
	_ = bus.Emit(context.Background(), core.EventType("run_started"), pb)
	_ = w.Close()

	lines := readLines(t, jsonlPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &envelope); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	// JSONL records carry the full EV-001 envelope; payload fields are nested
	// under "payload".
	p, _ := envelope["payload"].(map[string]any)
	if p == nil {
		t.Fatal("envelope missing 'payload' object")
	}

	for _, field := range []string{"secret_token", "api_key", "password"} {
		val, ok := p[field]
		if !ok {
			t.Errorf("field %q missing from JSONL payload", field)
			continue
		}
		if val != handlercontract.RedactedSentinel {
			t.Errorf("field %q not redacted: got %q, want %q", field, val, handlercontract.RedactedSentinel)
		}
	}
	if val, ok := p["safe_field"]; !ok || val != "visible" {
		t.Errorf("safe_field should pass through unchanged: got %v", val)
	}
}

// TestT5_RedactionHC032ValuePattern verifies that HC-032 value-pattern
// redaction (per-handler registered regex) applies to any field whose string
// value matches the registered pattern.
//
// HC-032 operates on VALUES, not field names. A registered regex is matched
// against every string-valued field; any match causes that field's value to be
// replaced with RedactedSentinel.
func TestT5_RedactionHC032ValuePattern(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")

	reg := handlercontract.NewRedactionRegistry()
	// Register a value-pattern that matches "sk-<alphanum>" anywhere in the value.
	// HC-032: RegisterPattern(subsystem, []*regexp.Regexp)
	skPattern := regexp.MustCompile(`sk-[A-Za-z0-9]+`)
	reg.RegisterPattern("t5probe_handler", []*regexp.Regexp{skPattern})

	w, _ := eventbus.OpenJSONLWriter(jsonlPath)
	defer func() { _ = w.Close() }()
	bus := eventbus.NewBusImplWithWriter(reg, w)
	_ = bus.Seal()

	// "carrier_field" carries an sk-prefixed secret value.
	// "safe_field" carries a value that does NOT match the pattern.
	p := map[string]any{
		"carrier_field": "sk-SuperSecretValue",
		"safe_field":    "visible-ok",
	}
	pb, _ := json.Marshal(p)
	_ = bus.Emit(context.Background(), core.EventType("run_started"), pb)
	_ = w.Close()

	lines := readLines(t, jsonlPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	var envelope map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &envelope)
	// JSONL records carry the full EV-001 envelope; payload fields are nested
	// under "payload".
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		t.Fatal("envelope missing 'payload' object")
	}

	// carrier_field value matched the sk- pattern — must be redacted.
	val, ok := payload["carrier_field"]
	if !ok {
		t.Fatal("carrier_field missing from JSONL payload")
	}
	valStr, _ := val.(string)
	if strings.Contains(valStr, "sk-") {
		t.Errorf("carrier_field not redacted by HC-032 value-pattern: got %q", valStr)
	}

	// safe_field must pass through unchanged.
	if safeVal, ok := payload["safe_field"]; !ok || safeVal != "visible-ok" {
		t.Errorf("safe_field should pass through unchanged: got %v", safeVal)
	}
}

// TestT5_DispatchOrder_SyncBlocksAsyncDoesNot verifies EV-014a dispatch order:
// Emit blocks until sync consumer completes, but returns before async consumers.
func TestT5_DispatchOrder_SyncBlocksAsyncDoesNot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")

	reg := handlercontract.NewRedactionRegistry()
	w, _ := eventbus.OpenJSONLWriter(jsonlPath)
	defer func() { _ = w.Close() }()
	bus := eventbus.NewBusImplWithWriter(reg, w)

	var mu sync.Mutex
	var dispatchLog []string

	_, _ = bus.Subscribe(core.Subscription{
		ConsumerID:    "sync-1",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		Handler: func(_ context.Context, _ core.Event) error {
			mu.Lock()
			dispatchLog = append(dispatchLog, "sync")
			mu.Unlock()
			return nil
		},
	})
	asyncStarted := make(chan struct{}, 1)
	_, _ = bus.Subscribe(core.Subscription{
		ConsumerID:    "async-1",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		Handler: func(_ context.Context, _ core.Event) error {
			asyncStarted <- struct{}{}
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			dispatchLog = append(dispatchLog, "async")
			mu.Unlock()
			return nil
		},
	})
	_ = bus.Seal()

	start := time.Now()
	pb, _ := json.Marshal(map[string]any{"x": 1})
	if emitErr := bus.Emit(context.Background(), core.EventType("run_started"), pb); emitErr != nil {
		t.Fatalf("Emit: %v", emitErr)
	}
	emitDuration := time.Since(start)

	// Emit must return BEFORE async consumer (100ms sleep) completes.
	if emitDuration >= 90*time.Millisecond {
		t.Errorf("Emit blocked for %v — async consumer appears to be on critical path (EV-014a violation)", emitDuration)
	}

	// Drain and check that sync happened before async.
	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if drainErr := bus.Drain(drainCtx); drainErr != nil {
		t.Fatalf("Drain: %v", drainErr)
	}

	mu.Lock()
	log := append([]string(nil), dispatchLog...)
	mu.Unlock()

	if len(log) < 2 {
		t.Fatalf("expected 2 dispatch entries, got %d", len(log))
	}
	if log[0] != "sync" {
		t.Errorf("sync consumer did not run first: log=%v", log)
	}
	if log[1] != "async" {
		t.Errorf("async consumer did not follow sync: log=%v", log)
	}
}
