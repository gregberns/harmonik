package main

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
			name:        "operator cancel, queue parked by the shutdown drain",
			finalQueue:  &queue.Queue{QueueID: "q-drain", Status: queue.QueueStatusPausedByDrain, ResumeOnStart: true},
			signalled:   true,
			wantCode:    1,
			wantMessage: "cancelled by operator (signal)",
		},
		{
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
