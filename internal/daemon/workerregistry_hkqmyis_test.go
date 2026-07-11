package daemon

// workerregistry_hkqmyis_test.go — regression test for hk-qmyis (commit
// c3307acc): buildWorkerRegistryWithRunner previously stayed silent about how
// many workers.yaml entries were loaded vs enabled, making "workers.yaml has
// entries but none enabled" indistinguishable in the logs from "no
// workers.yaml at all" (both were silent). The fix emits a worker_registry_init
// slog.Info with loaded/enabled counts whenever at least one worker is
// configured, plus a slog.Warn when zero of the configured workers are
// enabled — since the registry is built once at process start, a live edit to
// workers.yaml enabling a worker is a no-op until the next restart, so the
// warning says so explicitly.
//
// These tests install a capturing slog.Handler as the process default for the
// duration of each test (restored via t.Cleanup) and assert on the recorded
// records — NOT on stdout text — so they are immune to slog's default output
// format. Not run with t.Parallel(): slog.SetDefault mutates global state:
// Go's test runner never runs a non-parallel test concurrently with any
// other, so this is safely isolated without further locking.
//
// Bead ref: hk-qmyis.

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workers"
)

// hkqmyisCapturingSlogHandler records every slog.Record handed to it so tests
// can assert on message/level/attrs without depending on slog's text/JSON
// output formatting.
type hkqmyisCapturingSlogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *hkqmyisCapturingSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *hkqmyisCapturingSlogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *hkqmyisCapturingSlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *hkqmyisCapturingSlogHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *hkqmyisCapturingSlogHandler) find(msg string) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == msg {
			return r, true
		}
	}
	return slog.Record{}, false
}

func hkqmyisRecordAttr(t *testing.T, r slog.Record, key string) (slog.Value, bool) {
	t.Helper()
	var found slog.Value
	ok := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = a.Value
			ok = true
			return false
		}
		return true
	})
	return found, ok
}

// hkqmyisInstallCapturingHandler installs a fresh capturing handler as the
// slog default for the duration of the test and restores the prior default
// on cleanup.
func hkqmyisInstallCapturingHandler(t *testing.T) *hkqmyisCapturingSlogHandler {
	t.Helper()
	h := &hkqmyisCapturingSlogHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

// hkqmyisWorkerCfg builds a workers.Config with the given enabled flags, one
// worker per entry.
func hkqmyisWorkerCfg(enabled ...bool) workers.Config {
	cfg := workers.Config{Version: 1}
	for _, en := range enabled {
		cfg.Workers = append(cfg.Workers, workers.Worker{
			Name:      "worker",
			Transport: "ssh",
			Host:      "worker.local",
			OS:        "darwin",
			RepoPath:  "/repo",
			MaxSlots:  1,
			Enabled:   en,
		})
	}
	return cfg
}

// TestBuildWorkerRegistry_LogsCountsOnBoot_NoneEnabled is the core hk-qmyis
// regression: workers.yaml has entries but NONE are enabled must be visible
// via a worker_registry_init log line (workers_loaded=2, workers_enabled=0)
// AND a loud warning — this is the exact case that was previously silent and
// indistinguishable from "no workers.yaml at all".
func TestBuildWorkerRegistry_LogsCountsOnBoot_NoneEnabled(t *testing.T) {
	h := hkqmyisInstallCapturingHandler(t)

	bus := &handlercontract.CollectingEmitter{}
	_ = buildWorkerRegistryWithRunner(context.Background(), hkqmyisWorkerCfg(false, false), bus, nil)

	rec, ok := h.find("worker_registry_init")
	if !ok {
		t.Fatal("expected a worker_registry_init log record; got none (hk-qmyis regression)")
	}
	if rec.Level != slog.LevelInfo {
		t.Errorf("worker_registry_init level = %v; want Info", rec.Level)
	}
	if v, ok := hkqmyisRecordAttr(t, rec, "workers_loaded"); !ok || v.Int64() != 2 {
		t.Errorf("workers_loaded attr = %v (present=%v); want 2", v, ok)
	}
	if v, ok := hkqmyisRecordAttr(t, rec, "workers_enabled"); !ok || v.Int64() != 0 {
		t.Errorf("workers_enabled attr = %v (present=%v); want 0", v, ok)
	}

	// The loud all-disabled warning must also fire.
	foundWarn := false
	h.mu.Lock()
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			foundWarn = true
		}
	}
	h.mu.Unlock()
	if !foundWarn {
		t.Error("expected a Warn-level log when zero workers are enabled; got none (hk-qmyis regression)")
	}
}

// TestBuildWorkerRegistry_LogsCountsOnBoot_SomeEnabled verifies the counts are
// accurate when some (not all) configured workers are enabled, and that no
// all-disabled warning fires in that case.
func TestBuildWorkerRegistry_LogsCountsOnBoot_SomeEnabled(t *testing.T) {
	h := hkqmyisInstallCapturingHandler(t)

	bus := &handlercontract.CollectingEmitter{}
	_ = buildWorkerRegistryWithRunner(context.Background(), hkqmyisWorkerCfg(true, false, true), bus, nil)

	rec, ok := h.find("worker_registry_init")
	if !ok {
		t.Fatal("expected a worker_registry_init log record; got none")
	}
	if v, _ := hkqmyisRecordAttr(t, rec, "workers_loaded"); v.Int64() != 3 {
		t.Errorf("workers_loaded = %v; want 3", v)
	}
	if v, _ := hkqmyisRecordAttr(t, rec, "workers_enabled"); v.Int64() != 2 {
		t.Errorf("workers_enabled = %v; want 2", v)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			t.Errorf("unexpected Warn-level log when some workers are enabled: %q", r.Message)
		}
	}
}

// TestBuildWorkerRegistry_NoLogWhenNoWorkersConfigured verifies the pre-fix
// silent behaviour is preserved for the OTHER "no workers.yaml at all" case —
// buildWorkerRegistryWithRunner must stay silent (and return nil) when
// cfg.Workers is empty, so this path remains distinguishable from "entries
// present but none enabled" (which now logs).
func TestBuildWorkerRegistry_NoLogWhenNoWorkersConfigured(t *testing.T) {
	h := hkqmyisInstallCapturingHandler(t)

	bus := &handlercontract.CollectingEmitter{}
	reg := buildWorkerRegistryWithRunner(context.Background(), workers.Config{Version: 1}, bus, nil)

	if reg != nil {
		t.Errorf("expected nil registry when no workers configured, got %+v", reg)
	}
	if _, ok := h.find("worker_registry_init"); ok {
		t.Error("expected no worker_registry_init log when no workers are configured at all")
	}
}
