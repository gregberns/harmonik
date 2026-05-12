package daemon_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestDaemonStartCompiles verifies the package compiles and that Start can be
// invoked with a zero-value Config without panicking. This is the smoke-test
// sensor for hk-8mup.61: once this test is green the composition root
// scaffold is in place for subsequent wiring beads.
//
// Spec ref: specs/process-lifecycle.md §4.6 PL-020, PL-020a, PL-005 step 0.
func TestDaemonStartCompiles(t *testing.T) {
	t.Parallel()

	t.Run("start-with-zero-config-returns-nil", func(t *testing.T) {
		t.Parallel()

		cfg := daemon.Config{}
		err := daemon.Start(cfg)
		if err != nil {
			t.Errorf("daemon.Start(Config{}) returned non-nil error: %v; "+
				"stub Start must return nil until subsystem wiring is added", err)
		}
	})

	t.Run("start-with-nil-log-writer-does-not-panic", func(t *testing.T) {
		t.Parallel()

		// Config.LogWriter is nil → silences log output; must not panic.
		cfg := daemon.Config{LogWriter: nil}
		if err := daemon.Start(cfg); err != nil {
			t.Errorf("daemon.Start with nil LogWriter returned error: %v", err)
		}
	})
}
