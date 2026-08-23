package projectconfig

import (
	"errors"
	"testing"
)

// A block carrying only a "minor" field with NO schema_version must NOT be
// treated as an empty file. It falls through to the version gate and fails loud,
// rather than being silently discarded (the RU-06 bug).
func TestRU06_PartialWatchBlockNoSchema_NotSilentlyDropped(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
watch:
  absent_thresh_s: 120
`)
	_, err := LoadProjectConfig(root)
	if err == nil {
		t.Fatalf("LoadProjectConfig: a watch block without schema_version was silently accepted as empty; want *ErrUnsupportedConfigVersion")
	}
	var verr *ErrUnsupportedConfigVersion
	if !errors.As(err, &verr) {
		t.Fatalf("error type = %T (%v); want *ErrUnsupportedConfigVersion", err, err)
	}
}

// The same "minor" field WITH schema_version: 1 must survive into ProjectConfig —
// proving the operator's tuning is honored, not dropped.
func TestRU06_PartialWatchBlock_Honored(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
watch:
  absent_thresh_s: 120
`)
	cfg, err := LoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Watch.AbsentThreshSec != 120 {
		t.Errorf("Watch.AbsentThreshSec = %d; want 120 (partial watch block was dropped)", cfg.Watch.AbsentThreshSec)
	}
}

// A truly empty file (and an explicit-but-empty agents map) still reads as absent.
func TestRU06_EmptyFile_ReadsAsAbsent(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"whitespace-only":  "\n\n",
		"empty-agents-map": "agents: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := projCfgFixtureDir(t, content)
			cfg, err := LoadProjectConfig(root)
			if err != nil {
				t.Fatalf("LoadProjectConfig(%s): unexpected error: %v", name, err)
			}
			if cfg.Watch.AbsentThreshSec != 0 {
				t.Errorf("LoadProjectConfig(%s): expected zero-value config, got Watch.AbsentThreshSec=%d", name, cfg.Watch.AbsentThreshSec)
			}
		})
	}
}
