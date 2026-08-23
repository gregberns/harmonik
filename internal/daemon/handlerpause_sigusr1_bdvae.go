//go:build !windows

package daemon

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

func (w *SignalResumeWatcher) handleSignalResume(ctx context.Context) {
	snapshots := w.ctrl.Status("") // all known handlers
	resumed := 0
	for _, snap := range snapshots {
		if !snap.Paused {
			continue
		}
		if err := w.ctrl.Resume(ctx, snap.AgentType, core.HandlerResumedBySignal); err != nil {
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
