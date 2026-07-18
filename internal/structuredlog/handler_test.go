package structuredlog_test

// handler_test.go — binding tests for hk-sx9r.51 (ON-035 structured logs).
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/structuredlog"
)

// on035HandlerFixtureCfg builds a Config suitable for a single test.
func on035HandlerFixtureCfg(t *testing.T) structuredlog.Config {
	t.Helper()
	return structuredlog.Config{
		Subsystem:  "test_subsystem",
		ProjectDir: t.TempDir(),
		MinLevel:   slog.LevelDebug,
	}
}

// on035HandlerFixtureReadLines returns the lines written to the active log file.
func on035HandlerFixtureReadLines(t *testing.T, cfg structuredlog.Config) []map[string]any {
	t.Helper()
	active := filepath.Join(cfg.ProjectDir, ".harmonik", "logs", cfg.Subsystem+"-active.jsonl")
	//nolint:gosec // test helper; path from t.TempDir()
	f, err := os.Open(active)
	if err != nil {
		t.Fatalf("on035HandlerFixtureReadLines: open %s: %v", active, err)
	}
	defer func() { _ = f.Close() }()

	var out []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("on035HandlerFixtureReadLines: parse JSON line: %v (line: %s)", err, line)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("on035HandlerFixtureReadLines: scan: %v", err)
	}
	return out
}

// TestON035Handler_RequiredFieldsPresent verifies that every required field
// from the ON-035 wire format is present in emitted records.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.
func TestON035Handler_RequiredFieldsPresent(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	logger := slog.New(h)
	logger.Info("hello world")

	recs := on035HandlerFixtureReadLines(t, cfg)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	rec := recs[0]

	required := []string{"ts", "log_schema_version", "level", "subsystem", "source_subsystem", "msg", "fields"}
	for _, key := range required {
		if _, ok := rec[key]; !ok {
			t.Errorf("ON-035: required field %q missing from emitted record", key)
		}
	}
}

// TestON035Handler_SchemaVersion verifies that log_schema_version is "1.0".
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.
func TestON035Handler_SchemaVersion(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("test")

	recs := on035HandlerFixtureReadLines(t, cfg)
	if got := recs[0]["log_schema_version"]; got != "1.0" {
		t.Errorf("ON-035: log_schema_version = %v, want '1.0'", got)
	}
}

// TestON035Handler_LevelMapping verifies that all four slog levels map to the
// ON-035 level strings.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035 — "level ∈ {debug, info, warn, error}".
func TestON035Handler_LevelMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		slogLevel slog.Level
		wantLevel string
	}{
		{slog.LevelDebug, "debug"},
		{slog.LevelInfo, "info"},
		{slog.LevelWarn, "warn"},
		{slog.LevelError, "error"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.wantLevel, func(t *testing.T) {
			t.Parallel()

			cfg := on035HandlerFixtureCfg(t)
			h, err := structuredlog.NewHandler(cfg)
			if err != nil {
				t.Fatalf("NewHandler: %v", err)
			}
			defer func() { _ = h.Close() }()

			logger := slog.New(h)
			logger.Log(context.Background(), tc.slogLevel, "test")

			recs := on035HandlerFixtureReadLines(t, cfg)
			if len(recs) != 1 {
				t.Fatalf("want 1 record, got %d", len(recs))
			}
			if got := recs[0]["level"]; got != tc.wantLevel {
				t.Errorf("ON-035: level = %q, want %q", got, tc.wantLevel)
			}
		})
	}
}

// TestON035Handler_SubsystemAndSourceSubsystem verifies the subsystem fields.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.
func TestON035Handler_SubsystemAndSourceSubsystem(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("test")
	recs := on035HandlerFixtureReadLines(t, cfg)
	rec := recs[0]

	if got := rec["subsystem"]; got != cfg.Subsystem {
		t.Errorf("subsystem = %q, want %q", got, cfg.Subsystem)
	}
	if got := rec["source_subsystem"]; got != cfg.Subsystem {
		t.Errorf("source_subsystem = %q, want %q (default = Subsystem)", got, cfg.Subsystem)
	}
}

// TestON035Handler_SourceSubsystemOverride verifies SourceSubsystem is used
// when explicitly set.
func TestON035Handler_SourceSubsystemOverride(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	cfg.SourceSubsystem = "peer_subsystem"
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("test")
	recs := on035HandlerFixtureReadLines(t, cfg)
	if got := recs[0]["source_subsystem"]; got != "peer_subsystem" {
		t.Errorf("source_subsystem = %q, want %q", got, "peer_subsystem")
	}
}

// TestON035Handler_OptionalFieldsOmitted verifies that run_id, node_id, and
// event_id are absent when not set.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035 — "run_id?, node_id?, event_id?".
func TestON035Handler_OptionalFieldsOmitted(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("test")
	recs := on035HandlerFixtureReadLines(t, cfg)
	rec := recs[0]

	for _, opt := range []string{"run_id", "node_id", "event_id"} {
		if _, present := rec[opt]; present {
			t.Errorf("ON-035: optional field %q should be absent when not set", opt)
		}
	}
}

// TestON035Handler_OptionalFieldsPopulated verifies that run_id, node_id, and
// event_id appear in the top-level record when supplied as slog attrs.
func TestON035Handler_OptionalFieldsPopulated(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("test",
		"run_id", "run-abc",
		"node_id", "node-xyz",
		"event_id", "01950000-0000-7000-8000-000000000000",
	)

	recs := on035HandlerFixtureReadLines(t, cfg)
	rec := recs[0]

	if got := rec["run_id"]; got != "run-abc" {
		t.Errorf("run_id = %v, want %q", got, "run-abc")
	}
	if got := rec["node_id"]; got != "node-xyz" {
		t.Errorf("node_id = %v, want %q", got, "node-xyz")
	}
	if got := rec["event_id"]; got != "01950000-0000-7000-8000-000000000000" {
		t.Errorf("event_id = %v, want %q", got, "01950000-0000-7000-8000-000000000000")
	}
}

// TestON035Handler_SecretsRedaction verifies that the Redact function is
// called before emission and its output lands in the record.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035; §4.7 ON-022 (producer-side).
func TestON035Handler_SecretsRedaction(t *testing.T) {
	t.Parallel()

	const sentinel = "<redacted>"
	cfg := on035HandlerFixtureCfg(t)
	cfg.Redact = func(fields map[string]any) map[string]any {
		out := make(map[string]any, len(fields))
		for k, v := range fields {
			if strings.Contains(strings.ToLower(k), "token") {
				out[k] = sentinel
			} else {
				out[k] = v
			}
		}
		return out
	}
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("auth event", "api_token", "supersecret123", "user", "alice")

	recs := on035HandlerFixtureReadLines(t, cfg)
	fieldsRaw, ok := recs[0]["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields is not map[string]any: %T", recs[0]["fields"])
	}

	if got := fieldsRaw["api_token"]; got != sentinel {
		t.Errorf("ON-022: api_token = %v, want %q (not redacted)", got, sentinel)
	}
	if got := fieldsRaw["user"]; got != "alice" {
		t.Errorf("user = %v, want %q (should not be redacted)", got, "alice")
	}
}

// TestON035Handler_TSFormat verifies the ts field is RFC 3339 with ms.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035 — "ts (RFC 3339 with ms)".
func TestON035Handler_TSFormat(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("test")
	recs := on035HandlerFixtureReadLines(t, cfg)
	ts, ok := recs[0]["ts"].(string)
	if !ok {
		t.Fatalf("ts is not string: %T", recs[0]["ts"])
	}

	_, err = time.Parse("2006-01-02T15:04:05.000Z07:00", ts)
	if err != nil {
		t.Errorf("ON-035: ts %q is not RFC 3339 with ms: %v", ts, err)
	}
}

// TestON035Handler_RotationBySize verifies that the active file is rotated
// when it exceeds 100 MiB.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035 — "rotate at 100 MiB".
func TestON035Handler_RotationBySize(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	// Inflate the internal counter above the 100 MiB threshold by writing a
	// large record. We do this by padding the fields map.
	padding := strings.Repeat("x", rotateMaxBytesForTest+1)
	slog.New(h).Info("big record", "padding", padding)

	logsDir := filepath.Join(cfg.ProjectDir, ".harmonik", "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", logsDir, err)
	}

	// After writing 1 oversized record the file rotates on the NEXT write.
	// Emit one more record to trigger rotation.
	slog.New(h).Info("trigger rotation")
	_ = h.Close()

	entries, err = os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", logsDir, err)
	}

	var rotated []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "-active.jsonl") {
			rotated = append(rotated, e.Name())
		}
	}
	if len(rotated) == 0 {
		t.Errorf("ON-035: expected at least one rotated file in %s after size threshold, got none (all files: %v)",
			logsDir, func() []string {
				var names []string
				for _, e := range entries {
					names = append(names, e.Name())
				}
				return names
			}())
	}
}

// rotateMaxBytesForTest must match handler.go's rotateMaxBytes constant.
// It is duplicated here as an explicit anchor so a change to either constant
// causes a compile-time discrepancy (the test will produce an obviously wrong
// result).
const rotateMaxBytesForTest = 100 << 20

// TestON035Handler_RotationByAge verifies that the active file is rotated
// after 24 hours.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035 — "rotate at 24 hours".
func TestON035Handler_RotationByAge(t *testing.T) {
	t.Parallel()

	// Use a clock that starts 25 hours in the past.
	epoch := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ticks := []time.Time{
		epoch,                     // openFile() call
		epoch,                     // first Handle call (rotateIfNeeded)
		epoch,                     // second Handle call (rotateIfNeeded) — same instant
		epoch.Add(25 * time.Hour), // third Handle call — age exceeded
	}
	idx := 0
	cfg := on035HandlerFixtureCfg(t)
	cfg.NowFn = func() time.Time {
		t := ticks[idx]
		if idx < len(ticks)-1 {
			idx++
		}
		return t
	}

	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	logger := slog.New(h)
	logger.Info("first record")
	logger.Info("second record")
	logger.Info("trigger rotation after 25h")

	_ = h.Close()

	logsDir := filepath.Join(cfg.ProjectDir, ".harmonik", "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", logsDir, err)
	}

	var rotated []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "-active.jsonl") {
			rotated = append(rotated, e.Name())
		}
	}
	if len(rotated) == 0 {
		t.Errorf("ON-035: expected at least one rotated file after 25h, none found (files: %v)", func() []string {
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			return names
		}())
	}
}

// TestON035Handler_RotatedPathContainsSubsystem verifies that rotated files
// are named "<subsystem>-<rotated_at>.jsonl".
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035 — ".harmonik/logs/<subsystem>-<rotated_at>.jsonl".
func TestON035Handler_RotatedPathContainsSubsystem(t *testing.T) {
	t.Parallel()

	epoch := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	calls := 0
	cfg := on035HandlerFixtureCfg(t)
	cfg.Subsystem = "mysubsystem"
	cfg.NowFn = func() time.Time {
		calls++
		if calls <= 2 {
			return epoch
		}
		return epoch.Add(25 * time.Hour)
	}

	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("first")
	slog.New(h).Info("rotate trigger")
	_ = h.Close()

	logsDir := filepath.Join(cfg.ProjectDir, ".harmonik", "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", logsDir, err)
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "-active.jsonl") {
			continue
		}
		if !strings.HasPrefix(name, "mysubsystem-") {
			t.Errorf("ON-035: rotated file %q does not start with 'mysubsystem-'", name)
		}
		if !strings.HasSuffix(name, ".jsonl") {
			t.Errorf("ON-035: rotated file %q does not end with '.jsonl'", name)
		}
	}
}

// TestON035Handler_ConcurrentWritesFromClones verifies that concurrent writes
// through the original handler and clones produced by WithAttrs/WithGroup do
// not corrupt each other. Each goroutine emits N records; the test asserts that
// every emitted line is valid NDJSON (no interleaving of bytes from separate
// writes). This is the regression test for the sync.Mutex lock-copy bug fixed
// in hk-ampck: clone() was doing h2 := *h, copying the mutex value so each
// clone held its own independent lock over the shared *os.File.
func TestON035Handler_ConcurrentWritesFromClones(t *testing.T) {
	t.Parallel()

	const goroutines = 8
	const recsPerGoroutine = 50

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each goroutine gets its own clone via WithAttrs/WithGroup to
			// exercise the shared-lock path.
			var logger *slog.Logger
			if i%2 == 0 {
				logger = slog.New(h.WithAttrs([]slog.Attr{slog.Int("worker", i)}))
			} else {
				logger = slog.New(h.WithGroup(fmt.Sprintf("g%d", i)))
			}
			for j := 0; j < recsPerGoroutine; j++ {
				logger.Info("concurrent write", "seq", j)
			}
		}()
	}
	wg.Wait()
	_ = h.Close()

	// Every line must be valid JSON. A corrupted line (interleaved bytes) will
	// fail to parse.
	active := filepath.Join(cfg.ProjectDir, ".harmonik", "logs", cfg.Subsystem+"-active.jsonl")
	//nolint:gosec // test helper
	f, err := os.Open(active)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer func() { _ = f.Close() }()

	lineN := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lineN++
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Errorf("line %d is not valid JSON (concurrent corruption?): %v\nline: %s", lineN, err, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	want := goroutines * recsPerGoroutine
	if lineN != want {
		t.Errorf("want %d records, got %d", want, lineN)
	}
}

// TestON035Handler_FieldsIsAlwaysPresent verifies that the fields key is
// always a JSON object, even when no extra fields are logged.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035 — "fields (map of typed values)".
func TestON035Handler_FieldsIsAlwaysPresent(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("no extra fields")
	recs := on035HandlerFixtureReadLines(t, cfg)

	fieldsVal, ok := recs[0]["fields"]
	if !ok {
		t.Fatalf("ON-035: 'fields' key missing from record")
	}
	if _, ok := fieldsVal.(map[string]any); !ok {
		t.Errorf("ON-035: 'fields' is %T, want map[string]any (JSON object)", fieldsVal)
	}
}

// TestON035Handler_ExtraAttrsInFields verifies that additional slog attrs
// land in the fields map.
func TestON035Handler_ExtraAttrsInFields(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	defer func() { _ = h.Close() }()

	slog.New(h).Info("with extras", "bead_id", "hk-abc", "phase", "run")
	recs := on035HandlerFixtureReadLines(t, cfg)
	fields := recs[0]["fields"].(map[string]any)

	if got := fields["bead_id"]; got != "hk-abc" {
		t.Errorf("fields.bead_id = %v, want %q", got, "hk-abc")
	}
	if got := fields["phase"]; got != "run" {
		t.Errorf("fields.phase = %v, want %q", got, "run")
	}
}

// TestON035Handler_HandleAfterCloseNoPanic verifies that a Handle call issued
// after Close does not nil-deref the closed log file. It must no-op and return
// a benign error rather than panic (shutdown/teardown ordering hazard).
func TestON035Handler_HandleAfterCloseNoPanic(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Must not panic; returns an error signalling the closed handler.
	if err := h.Handle(context.Background(), slog.Record{}); err == nil {
		t.Errorf("Handle after Close: want non-nil error, got nil")
	}

	// Idempotent: a second Close and further Handle calls are also safe.
	if err := h.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	slog.New(h).Info("after close")
}

// TestON035Handler_ConcurrentHandleAndClose stresses the Close/Handle race
// under -race: many goroutines log while another closes the handler. No write
// may panic on the nil file.
func TestON035Handler_ConcurrentHandleAndClose(t *testing.T) {
	t.Parallel()

	cfg := on035HandlerFixtureCfg(t)
	h, err := structuredlog.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger := slog.New(h)
			for j := 0; j < 100; j++ {
				logger.Info("racing write", "j", j)
			}
		}()
	}
	// Close concurrently with the writers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = h.Close()
	}()
	wg.Wait()
}
