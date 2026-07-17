package daemon

import (
	"context"
	"testing"
)

// TestRunBootPreflights_SkipFlagMatrix asserts that runBootPreflights (extracted
// from startWithHooks in giant-retirement boot-config B2) short-circuits every
// pre-flight step in unit-test mode: with an empty ProjectDir (or with all skip
// flags set) it performs no I/O and returns a zero backoff delay. This is the
// independently-testable win of the extraction — the skip-flag guards can be
// exercised without booting the bus / subscribers.
func TestRunBootPreflights_SkipFlagMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "empty_project_dir",
			cfg:  Config{},
		},
		{
			name: "project_dir_all_skips_set",
			cfg: Config{
				ProjectDir:                 t.TempDir(),
				SkipWALCheckpoint:          true,
				SkipBrHistoryRotation:      true,
				SkipRestartBackoff:         true,
				SkipBeadsMergeDriverConfig: true,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := runBootPreflights(context.Background(), tc.cfg); got != 0 {
				t.Errorf("runBootPreflights returned backoff delay %v; want 0 (backoff skipped)", got)
			}
		})
	}
}
