//go:build !windows

package daemon

// handlerpause_sigusr1_bdvae_export_test.go — test-seam exports for the
// SIGUSR1-based external-trigger resume (hk-bdvae).
//
// Build-constrained to !windows because SignalResumeWatcher uses syscall.SIGUSR1
// which is not available on Windows.
//
// Bead ref: hk-bdvae.

import "context"

// ExportedSignalResumeWatcherHandle exposes SignalResumeWatcher.handleSignalResume
// for tests in package daemon_test.
//
// Tests call this directly to exercise the resume logic without sending real OS
// signals (which are process-global and would interfere with other test goroutines).
func ExportedSignalResumeWatcherHandle(w *SignalResumeWatcher, ctx context.Context) {
	w.handleSignalResume(ctx)
}
