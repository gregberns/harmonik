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
