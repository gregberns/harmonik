package orchestrator

// groupadvance_hk45ude_test.go — pure truth-table coverage for the EM-015f
// group-advance CLASSIFICATION micro-predicates (M5 slice 3C). These migrate the
// pure classification portions of internal/daemon/workloop_hk45ude_test.go's
// EM-015f gate; the mutation/persist/emit end-to-end cases stay in package daemon
// and assert identical behavior.
//
// Spec ref: specs/execution-model.md §4.3 EM-015f.
// Bead ref: hk-45ude.

import (
	"testing"
)

// Group-status string values mirror internal/queue/types.go (the daemon projects
// the typed enum to these at the boundary). Kept local so the test stays on the
// orchestrator import edge ($gostd + core) and never pulls internal/queue.
const (
	pending             = "pending"
	active              = "active"
	completeSuccess     = "complete-success"
	completeWithFailure = "complete-with-failures"
)

func TestFirstPendingGroupIndex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		statuses []string
		want     int
	}{
		{"empty", nil, -1},
		{"none pending — all complete-success", []string{completeSuccess, completeSuccess}, -1},
		{"first is pending", []string{pending, pending}, 0},
		// EM-015f: after group 0 reaches complete-success, the first still-pending
		// group is the one activated next.
		{"group 0 done, group 1 pending", []string{completeSuccess, pending}, 1},
		{"active then pending — skip active", []string{active, pending}, 1},
		{"pending after a failure group", []string{completeWithFailure, pending}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FirstPendingGroupIndex(tc.statuses); got != tc.want {
				t.Errorf("FirstPendingGroupIndex(%v) = %d; want %d", tc.statuses, got, tc.want)
			}
		})
	}
}

func TestGroupReachedSuccess(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status string
		want   bool
	}{
		{completeSuccess, true},
		{completeWithFailure, false},
		{active, false},
		{pending, false},
		{"", false},
	}
	for _, tc := range cases {
		if got := GroupReachedSuccess(tc.status); got != tc.want {
			t.Errorf("GroupReachedSuccess(%q) = %v; want %v", tc.status, got, tc.want)
		}
	}
}

func TestGroupFailurePausesQueue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status string
		want   bool
	}{
		{completeWithFailure, true},
		{completeSuccess, false},
		{active, false},
		{pending, false},
		{"", false},
	}
	for _, tc := range cases {
		if got := GroupFailurePausesQueue(tc.status); got != tc.want {
			t.Errorf("GroupFailurePausesQueue(%q) = %v; want %v", tc.status, got, tc.want)
		}
	}
}

func TestAllGroupsSucceeded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		statuses []string
		want     bool
	}{
		{"empty → false (no queue completes with zero groups)", nil, false},
		{"single complete-success", []string{completeSuccess}, true},
		{"all complete-success", []string{completeSuccess, completeSuccess}, true},
		{"one still active", []string{completeSuccess, active}, false},
		{"one still pending", []string{completeSuccess, pending}, false},
		{"one failed → not all succeeded", []string{completeSuccess, completeWithFailure}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := AllGroupsSucceeded(tc.statuses); got != tc.want {
				t.Errorf("AllGroupsSucceeded(%v) = %v; want %v", tc.statuses, got, tc.want)
			}
		})
	}
}
