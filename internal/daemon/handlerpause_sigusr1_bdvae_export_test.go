//go:build !windows

package daemon

import "context"

// ExportedSignalResumeWatcherHandle exposes SignalResumeWatcher.handleSignalResume
// for tests in package daemon_test.
//
// Tests call this directly to exercise the resume logic without sending real OS
// signals (which are process-global and would interfere with other test goroutines).
func ExportedSignalResumeWatcherHandle(w *SignalResumeWatcher, ctx context.Context) {
	w.handleSignalResume(ctx)
}
