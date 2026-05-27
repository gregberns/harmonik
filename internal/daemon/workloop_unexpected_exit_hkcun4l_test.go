package daemon_test

// workloop_unexpected_exit_hkcun4l_test.go — Verify workloop handles unexpected
// br CLI exit codes (2, 3, 137, 139) without infinite retry.
//
// The bounded-retry mechanism (hk-6pspu) should cap dispatch attempts at
// maxItemAttempts (3), marking the item failed rather than looping forever.
//
// Bead ref: hk-cun4l.

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// unexpectedExitLedger is a stub beadLedger where ClaimBead always returns an
// error simulating an unexpected br CLI exit code. ShowBead returns
// CoarseStatusOpen so the pre-claim guard passes.
type unexpectedExitLedger struct {
	mu       sync.Mutex
	exitCode int
	claimed  []core.BeadID
	closed   []core.BeadID
	reopened []core.BeadID
}

func (u *unexpectedExitLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch
}

func (u *unexpectedExitLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Title: "unexpected-exit-test"}, nil
}

func (u *unexpectedExitLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.claimed = append(u.claimed, id)
	return fmt.Errorf("brcli: exit status %d", u.exitCode)
}

func (u *unexpectedExitLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.closed = append(u.closed, id)
	return nil
}

func (u *unexpectedExitLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reopened = append(u.reopened, id)
	return nil
}

func (u *unexpectedExitLedger) claimCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.claimed)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_UnexpectedBrExitCodes (hk-cun4l)
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_UnexpectedBrExitCodes verifies that unexpected br CLI exit codes
// (2, 3, 137/SIGKILL, 139/SIGSEGV) do not cause infinite retry. The
// bounded-retry mechanism (hk-6pspu, maxItemAttempts=3) should mark the queue
// item as failed after 3 attempts and allow the workloop to exit.
//
// Bead ref: hk-cun4l.
func TestWorkLoop_UnexpectedBrExitCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		exitCode int
	}{
		{"exit_2", 2},
		{"exit_3", 3},
		{"exit_137_SIGKILL", 137},
		{"exit_139_SIGSEGV", 139},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir, _ := workloopFixtureProjectDir(t)
			workloopFixtureGitRepo(t, projectDir)

			beadID := core.BeadID(fmt.Sprintf("hk-cun4l-%s", tc.name))

			now := time.Now()
			q := &queue.Queue{
				SchemaVersion: 1,
				QueueID:       fmt.Sprintf("cun4l-%s-queue", tc.name),
				SubmittedAt:   now,
				Status:        queue.QueueStatusActive,
				Groups: []queue.Group{
					{
						GroupIndex: 0,
						Kind:       queue.GroupKindWave,
						Status:     queue.GroupStatusActive,
						Items: []queue.Item{
							{BeadID: beadID, Status: queue.ItemStatusPending},
						},
						CreatedAt: now,
					},
				},
			}

			qs := daemon.ExportedNewQueueStore()
			qs.SetQueue(q)

			ledger := &unexpectedExitLedger{exitCode: tc.exitCode}
			bus := &stubEventCollector{}
			exitCtx, cancelExit := context.WithCancel(context.Background())

			p := daemon.WorkLoopDepsParams{
				BrAdapter:         ledger,
				Bus:               bus,
				ProjectDir:        projectDir,
				HandlerBinary:     "/bin/sh",
				HandlerArgs:       []string{"-c", "exit 0"},
				IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
				QueueStore:        qs,
				MaxConcurrent:     1,
				AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
				CancelOnQueueExit: cancelExit,
			}
			deps := daemon.ExportedWorkLoopDeps(p)

			testCtx, testCancel := context.WithTimeout(exitCtx, 15*time.Second)
			defer testCancel()

			loopDone := make(chan error, 1)
			go func() {
				loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
			}()

			select {
			case err := <-loopDone:
				if err != nil {
					t.Errorf("runWorkLoop returned non-nil error: %v", err)
				}
			case <-time.After(14 * time.Second):
				t.Fatalf("runWorkLoop did not exit within 14s — exit code %d may cause infinite retry (hk-cun4l)", tc.exitCode)
			}

			// Verify the queue item ended up failed.
			finalQ := daemon.ExportedQueueStoreOf(deps).Queue()
			if finalQ == nil {
				t.Fatal("queue is nil after workloop exit")
			}
			if len(finalQ.Groups) == 0 || len(finalQ.Groups[0].Items) == 0 {
				t.Fatal("queue has no groups or items after workloop exit")
			}
			item := finalQ.Groups[0].Items[0]
			if item.Status != queue.ItemStatusFailed {
				t.Errorf("item.Status = %q; want %q (exit code %d should be bounded by maxItemAttempts)",
					item.Status, queue.ItemStatusFailed, tc.exitCode)
			}
			if item.Attempts < int(queue.MaxItemAttempts) {
				t.Errorf("item.Attempts = %d; want >= %d", item.Attempts, queue.MaxItemAttempts)
			}

			// Queue should be paused-by-failure (single item failed).
			if finalQ.Status != queue.QueueStatusPausedByFailure {
				t.Errorf("queue.Status = %q; want %q", finalQ.Status, queue.QueueStatusPausedByFailure)
			}

			// ClaimBead is called on dispatch attempts that proceed past the
			// Attempts check. The third attempt increments Attempts to 3
			// (>= maxItemAttempts) and fails the item without calling ClaimBead.
			// So ClaimBead is called maxItemAttempts-1 times.
			claims := ledger.claimCount()
			wantClaims := int(queue.MaxItemAttempts) - 1
			if claims != wantClaims {
				t.Errorf("ClaimBead call count = %d; want %d (maxItemAttempts-1)", claims, wantClaims)
			}
		})
	}
}
