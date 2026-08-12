package main

// run_exit_code_test.go — the exit code `harmonik run` returns for each way a
// run can end.
//
// Scripts key on these numbers, so the mapping is a contract: 0 all beads
// succeeded, 1 the run failed or the operator cancelled it, 2 the queue was
// left in a state nobody planned for. Exit 2 is a diagnostic. It must stay rare,
// because an operator who sees it is being told the tool does not understand
// its own state.
//
// The case that regressed (hk-3plak): Ctrl-C used to leave no queue in the
// store, because shutdown archived it. Shutdown now parks the queue in place as
// paused-by-drain with the restart intent set, so the store stays populated and
// a plain operator cancel started reporting as an unexpected state with exit 2.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

func TestClassifyRunExit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		finalQueue  *queue.Queue
		signalled   bool
		wantCode    int
		wantMessage string
	}{
		{
			name:       "all beads succeeded, queue unlinked, no signal",
			finalQueue: nil,
			signalled:  false,
			wantCode:   0,
		},
		{
			name:        "operator cancel, queue already gone from the store",
			finalQueue:  nil,
			signalled:   true,
			wantCode:    1,
			wantMessage: "cancelled by operator (signal)",
		},
		{
			// hk-3plak. The shutdown drain calls PauseQueueForRestart, persists
			// in place and puts the queue back in the store, so the store is
			// still populated when the exit code is chosen.
			name:        "operator cancel, queue parked by the shutdown drain",
			finalQueue:  &queue.Queue{QueueID: "q-drain", Status: queue.QueueStatusPausedByDrain, ResumeOnStart: true},
			signalled:   true,
			wantCode:    1,
			wantMessage: "cancelled by operator (signal)",
		},
		{
			// Same parked queue, no signal: the run ended some other way, so
			// naming the operator would be false. Still exit 1 — work is unfinished.
			name:        "shutdown drain parked the queue with no signal",
			finalQueue:  &queue.Queue{QueueID: "q-drain", Status: queue.QueueStatusPausedByDrain, ResumeOnStart: true},
			signalled:   false,
			wantCode:    1,
			wantMessage: "queue parked for restart",
		},
		{
			name:        "a bead failed",
			finalQueue:  &queue.Queue{QueueID: "q-fail", Status: queue.QueueStatusPausedByFailure},
			signalled:   false,
			wantCode:    1,
			wantMessage: "one or more beads failed",
		},
		{
			// No restart intent means an explicit operator drain, not a
			// shutdown. Nothing in the inline path produces it, so it stays a
			// diagnostic rather than joining the cancel branch.
			name:        "queue drained by an explicit operator pause",
			finalQueue:  &queue.Queue{QueueID: "q-op", Status: queue.QueueStatusPausedByDrain},
			signalled:   false,
			wantCode:    2,
			wantMessage: "unexpected queue state after exit",
		},
		{
			name:        "queue still active after the daemon returned",
			finalQueue:  &queue.Queue{QueueID: "q-active", Status: queue.QueueStatusActive},
			signalled:   false,
			wantCode:    2,
			wantMessage: "unexpected queue state after exit",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := classifyRunExit(tc.finalQueue, tc.signalled)
			if got.code != tc.wantCode {
				t.Errorf("exit code = %d, want %d (message %q)", got.code, tc.wantCode, got.message)
			}
			if tc.wantMessage == "" {
				if got.message != "" {
					t.Errorf("message = %q, want none", got.message)
				}
				return
			}
			if !strings.Contains(got.message, tc.wantMessage) {
				t.Errorf("message = %q, want it to contain %q", got.message, tc.wantMessage)
			}
		})
	}
}
