//go:build !windows

package daemon

// handlerpause_sigusr1_bdvae.go — external-trigger resume via SIGUSR1 (hk-bdvae).
//
// Implements one of the three external-trigger resume paths listed in
// specs/handler-pause.md §1.2:
//
//	External-trigger resume (webhook, SIGUSR1, file-marker) — post-MVH
//
// Design:
//   - SignalResumeWatcher listens for SIGUSR1 on a dedicated channel.
//   - On receipt it calls HandlerPauseController.Resume for every handler type
//     that is currently paused, using core.HandlerResumedBySignal as the initiator.
//   - All resume logic is centralised in HandlerPauseController.Resume (HP-040),
//     which persists state and emits handler_resumed events.
//
// Authentication / authorisation:
//   - SIGUSR1 can only be delivered by processes whose real or saved user ID
//     matches the daemon's UID, or by the superuser (root).  This is enforced
//     by the kernel's kill(2) permission check; no additional application-level
//     authentication is required.  The daemon's PID is available in the pidfile
//     at <ProjectDir>/.harmonik/daemon.pid so authorised operators can locate
//     the process without inspecting /proc or similar.
//
// Spec ref: specs/handler-pause.md §1.2 (post-MVH external-trigger resume).
// Bead ref: hk-bdvae.

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gregberns/harmonik/internal/core"
)

// SignalResumeWatcher listens for SIGUSR1 and resumes all paused handler types.
//
// Construct with NewSignalResumeWatcher; start with Run inside a goroutine.
// The Run loop exits when ctx is cancelled (daemon shutdown).
type SignalResumeWatcher struct {
	ctrl      *HandlerPauseController
	logWriter *log.Logger
}

// NewSignalResumeWatcher constructs a SignalResumeWatcher for the given controller.
//
// logger may be nil; in that case signal events are silently processed.
func NewSignalResumeWatcher(ctrl *HandlerPauseController, logger *log.Logger) *SignalResumeWatcher {
	return &SignalResumeWatcher{ctrl: ctrl, logWriter: logger}
}

// Run blocks, listening for SIGUSR1 until ctx is cancelled.
//
// Each SIGUSR1 received calls handleSignalResume to resume all paused handlers.
// Designed to run in a dedicated goroutine started by daemon.Start.
//
// The caller is responsible for ensuring Run is not called more than once
// concurrently (each call registers its own signal channel).
func (w *SignalResumeWatcher) Run(ctx context.Context) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGUSR1)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigCh:
			w.handleSignalResume(ctx)
		}
	}
}

// handleSignalResume resumes all currently-paused handler types.
//
// It queries the controller for all known handler states, then calls Resume
// for every handler that is currently paused.  Errors from individual Resume
// calls (e.g. ErrHandlerNotPaused due to a concurrent resume) are logged at
// DEBUG level and do not abort processing of remaining handlers.
//
// This method is exported as a test seam (ExportedSignalResumeWatcherHandle)
// so tests can call it directly without sending real OS signals.
func (w *SignalResumeWatcher) handleSignalResume(ctx context.Context) {
	snapshots := w.ctrl.Status("") // all known handlers
	resumed := 0
	for _, snap := range snapshots {
		if !snap.Paused {
			continue
		}
		if err := w.ctrl.Resume(ctx, snap.AgentType, core.HandlerResumedBySignal); err != nil {
			// ErrHandlerNotPaused is benign: a concurrent resume already cleared it.
			if w.logWriter != nil {
				w.logWriter.Printf("signal-resume: Resume(%s): %v", snap.AgentType, err)
			}
			continue
		}
		resumed++
		if w.logWriter != nil {
			w.logWriter.Printf("INFO signal-resume: resumed agent_type=%s by=signal", snap.AgentType)
		}
	}
	if w.logWriter != nil && resumed == 0 && len(snapshots) > 0 {
		w.logWriter.Printf("signal-resume: SIGUSR1 received but no handlers were paused")
	}
}
