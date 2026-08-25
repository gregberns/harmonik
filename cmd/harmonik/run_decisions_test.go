package main

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestParseRunBeadOptionsRejectsInvalidDecisions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"mixed bead sources", []string{"pos", "--beads", "flag"}, "cannot mix"},
		{"unknown workflow", []string{"bead", "--workflow-mode", "other"}, "unknown --workflow-mode"},
		{"dot needs ref", []string{"bead", "--workflow-mode", "dot"}, "requires --workflow-ref"},
		{"retired workflow", []string{"bead", "--workflow-mode", "review-loop"}, "was retired"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseRunBeadOptions(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestSelectRunBeads(t *testing.T) {
	t.Parallel()
	ids, err := selectRunBeads("a, b", nil)
	if err != nil || len(ids) != 2 || ids[1] != "b" {
		t.Fatalf("ids = %v, err = %v", ids, err)
	}
}

func TestSelectRunWorkflow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode, ref, wantMode, wantRef string
		wantErr                      bool
	}{
		{"builtin", "", "", "", false},
		{"single", "", "single", "", false},
		{"dot", "flow.dot", "dot", "flow.dot", false},
		{"dot", "", "", "", true},
	}
	for _, tt := range tests {
		got, err := selectRunWorkflow(tt.mode, tt.ref)
		if (err != nil) != tt.wantErr || got.mode != tt.wantMode || got.ref != tt.wantRef {
			t.Errorf("selectRunWorkflow(%q, %q) = %+v, %v", tt.mode, tt.ref, got, err)
		}
	}
}

func TestSelectNotifyStream(t *testing.T) {
	t.Parallel()
	tests := []struct {
		explicit, disabled bool
		beads, concurrent  int
		wantSet            bool
		want               string
	}{
		{false, false, 1, 1, false, ""},
		{false, false, 2, 1, true, "-"},
		{false, false, 1, 2, true, "-"},
		{false, true, 2, 2, false, ""},
		{true, false, 1, 1, true, "events"},
	}
	for _, tt := range tests {
		set, value := selectNotifyStream(tt.explicit, tt.disabled, tt.beads, tt.concurrent, tt.want)
		if set != tt.wantSet || value != tt.want {
			t.Errorf("got (%v, %q), want (%v, %q)", set, value, tt.wantSet, tt.want)
		}
	}
}

func TestClassifyStaleQueue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status  queue.QueueStatus
		lock    lifecycle.PidfileLockStatus
		missing bool
		want    staleQueueDecision
	}{
		{queue.QueueStatusCompleted, lifecycle.PidfileLockStatusHeld, false, staleQueueContinue},
		{queue.QueueStatusPausedByFailure, lifecycle.PidfileLockStatusHeld, false, staleQueueArchive},
		{queue.QueueStatusCancelled, lifecycle.PidfileLockStatusHeld, false, staleQueueArchive},
		{queue.QueueStatusActive, lifecycle.PidfileLockStatusStale, false, staleQueueArchive},
		{queue.QueueStatusActive, lifecycle.PidfileLockStatusHeld, true, staleQueueArchive},
		{queue.QueueStatusActive, lifecycle.PidfileLockStatusHeld, false, staleQueueRefuse},
	}
	for _, tt := range tests {
		if got := classifyStaleQueue(tt.status, tt.lock, tt.missing); got != tt.want {
			t.Errorf("status %q: got %v, want %v", tt.status, got, tt.want)
		}
	}
}
