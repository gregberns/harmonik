package main

import (
	"testing"

	"github.com/gregberns/harmonik/internal/workers"
)

// TestApplyWorkerOverrides_PrecedenceChain asserts the flag > file > default
// precedence for the remote-worker registry (B4 remote-substrate boot-wiring).
func TestApplyWorkerOverrides_PrecedenceChain(t *testing.T) {
	fileWorker := workers.Worker{
		Name:      "mac-studio",
		Transport: "ssh",
		Host:      "file-host.local",
		Enabled:   false,
	}
	fileCfg := workers.Config{
		Version: 1,
		Workers: []workers.Worker{fileWorker},
	}

	t.Run("flag beats file for host", func(t *testing.T) {
		got := applyWorkerOverrides(fileCfg, map[string]bool{"worker-host": true}, "flag-host.local", false)
		if got.Workers[0].Host != "flag-host.local" {
			t.Fatalf("host: got %q, want %q", got.Workers[0].Host, "flag-host.local")
		}
	})

	t.Run("file beats default for host", func(t *testing.T) {
		got := applyWorkerOverrides(fileCfg, map[string]bool{}, "", false)
		if got.Workers[0].Host != "file-host.local" {
			t.Fatalf("host: got %q, want %q", got.Workers[0].Host, "file-host.local")
		}
	})

	t.Run("default: no file no flag yields zero Config", func(t *testing.T) {
		got := applyWorkerOverrides(workers.Config{}, map[string]bool{"worker-host": true}, "flag-host.local", false)
		if len(got.Workers) != 0 {
			t.Fatalf("expected zero Config, got %+v", got)
		}
	})

	t.Run("flag beats file for enabled", func(t *testing.T) {
		got := applyWorkerOverrides(fileCfg, map[string]bool{"worker-enabled": true}, "", true)
		if !got.Workers[0].Enabled {
			t.Fatal("enabled: expected true (flag override), got false")
		}
	})

	t.Run("file beats default for enabled", func(t *testing.T) {
		cfg := workers.Config{
			Version: 1,
			Workers: []workers.Worker{{Host: "h", Enabled: true}},
		}
		got := applyWorkerOverrides(cfg, map[string]bool{}, "", false)
		if !got.Workers[0].Enabled {
			t.Fatal("enabled: expected true (file value), got false")
		}
	})
}

// TestApplyWorkerOverrides_TargetsPrimaryWorker asserts the RU-17 fix: overrides
// land on the worker the Registry consumes (PrimaryWorkerIndex) and leave any
// other configured worker untouched, rather than hardcoding index 0 independent
// of the Registry's selection.
func TestApplyWorkerOverrides_TargetsPrimaryWorker(t *testing.T) {
	cfg := workers.Config{
		Version: 1,
		Workers: []workers.Worker{
			{Name: "primary", Host: "primary-file.local", Enabled: false},
			{Name: "secondary", Host: "secondary-file.local", Enabled: true},
		},
	}
	idx := workers.PrimaryWorkerIndex(cfg)
	if idx != 0 {
		t.Fatalf("PrimaryWorkerIndex: got %d, want 0", idx)
	}

	got := applyWorkerOverrides(cfg,
		map[string]bool{"worker-host": true, "worker-enabled": true},
		"flag-host.local", true)

	// Primary (the Registry's worker) must receive both overrides.
	if got.Workers[idx].Host != "flag-host.local" {
		t.Fatalf("primary host: got %q, want %q", got.Workers[idx].Host, "flag-host.local")
	}
	if !got.Workers[idx].Enabled {
		t.Fatal("primary enabled: expected true (flag override)")
	}

	// The non-primary worker must be left exactly as configured.
	if got.Workers[1].Host != "secondary-file.local" {
		t.Fatalf("secondary host: got %q, want %q (untouched)", got.Workers[1].Host, "secondary-file.local")
	}
	if !got.Workers[1].Enabled {
		t.Fatal("secondary enabled: expected true (untouched file value)")
	}

	// The caller's Config must not be mutated.
	if cfg.Workers[0].Host != "primary-file.local" {
		t.Fatalf("input cfg mutated: primary host now %q", cfg.Workers[0].Host)
	}
}
