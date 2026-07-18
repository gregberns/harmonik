package daemon_test

// projectconfig_ru06_test.go — regression tests for the RU-06 empty-file
// sentinel fix. The prior hand-maintained per-block field list only checked a
// couple of fields per block (e.g. watch: only status_target / opsmonitor_target),
// so a config carrying ONLY a "minor" field (watch.absent_thresh_s) was mistaken
// for an empty file and SILENTLY DISCARDED — the daemon booted on defaults and
// the operator's tuning was lost. The sentinel is now a structural
// reflect.DeepEqual against a zero rawProjectConfig, so ANY set field defeats it.
//
// Covers:
//   - A partial block with only watch.absent_thresh_s and NO schema_version is no
//     longer swallowed: it falls through to the version check and FAILS LOUD with
//     *ErrUnsupportedConfigVersion (it is not treated as an empty file).
//   - A partial block with schema_version: 1 is HONORED end-to-end (the field
//     survives into ProjectConfig, not dropped).
//   - A genuinely empty file (and `agents: {}`) still reads as absent → zero value.
//
// Helper prefix: ru06 (implementer-protocol.md §Helper-prefix discipline).

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
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
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatalf("LoadProjectConfig: a watch block without schema_version was silently accepted as empty; want *ErrUnsupportedConfigVersion")
	}
	var verr *daemon.ExportedErrUnsupportedConfigVersion
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
	cfg, err := daemon.ExportedLoadProjectConfig(root)
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
		content := content
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := projCfgFixtureDir(t, content)
			cfg, err := daemon.ExportedLoadProjectConfig(root)
			if err != nil {
				t.Fatalf("LoadProjectConfig(%s): unexpected error: %v", name, err)
			}
			if cfg.Watch.AbsentThreshSec != 0 {
				t.Errorf("LoadProjectConfig(%s): expected zero-value config, got Watch.AbsentThreshSec=%d", name, cfg.Watch.AbsentThreshSec)
			}
		})
	}
}
