package daemon_test

// worktreecreatefail_hk3vbc_test.go — regression test for hk-3vbc Fix 1:
// when worktree creation fails for a run, the daemon must SURFACE the failure
// as a terminal run_failed event (carrying the worktree error in the summary)
// BEFORE it reopens the bead and returns.
//
// Before the fix the failure path only logged to stderr and reopened the bead —
// no event landed in events.jsonl, so operators saw only a downstream
// agent_ready_timeout (~90s later) with no cause attached.
//
// Reuses the mergeToMain* fixtures (project dir, git repo, recording ledger,
// stubEventCollector, event-finder) defined in mergetomain_hkftyvo_test.go.
//
// Bead: hk-3vbc.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestWorktreeCreateFailure_EmitsRunFailed verifies Fix 1: a WorktreeFactory
// error makes beadRunOne emit a run_failed event whose summary contains the
// worktree error, and then reopen the bead.
func TestWorktreeCreateFailure_EmitsRunFailed(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("worktree-create-fail-hk3vbc-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// Factory that always fails — simulates the remote worktree-create collision
	// ("branch/reference already exists") that the daemon previously swallowed.
	sentinel := errors.New("git worktree add: fatal: a branch named 'run/x' already exists")
	failingFactory := func(_ context.Context, _, _, _ string) (string, func(), error) {
		return "", nil, sentinel
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  failingFactory,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Fatal("timed out waiting for bead reopen after worktree-create failure")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	// ── Assertion 1: bead reopened with a create-worktree-failed reason. ───────
	if got := ledger.getReopenedCount(); got != 1 {
		t.Errorf("ReopenBead count = %d; want 1 (worktree-create failure must reopen the bead)", got)
	}
	if reason := ledger.getReopenReason(); !strings.Contains(reason, "create worktree failed") {
		t.Errorf("ReopenBead reason = %q; want it to mention 'create worktree failed'", reason)
	}

	// ── Assertion 2 (the fix): a run_failed event was emitted carrying the
	// worktree error. Without the fix NO run_failed lands here. ───────────────
	evs := mergeToMainFindEvents(collector, "run_failed")
	if len(evs) == 0 {
		t.Fatalf("no run_failed event emitted on worktree-create failure (hk-3vbc Fix 1 regression); events: %v", collector.eventTypes())
	}

	// The summary must identify this as a worktree-create failure and embed the
	// underlying git error.
	var foundSummary bool
	for _, ev := range evs {
		var m map[string]interface{}
		if err := json.Unmarshal(ev.Payload, &m); err != nil {
			t.Fatalf("run_failed payload unmarshal: %v", err)
		}
		summary, _ := m["summary"].(string)
		if strings.Contains(summary, "worktree_create_failed") && strings.Contains(summary, "already exists") {
			foundSummary = true
		}
		if success, ok := m["success"].(bool); ok && success {
			t.Errorf("run_failed event has success=true; want false")
		}
	}
	if !foundSummary {
		t.Errorf("run_failed summary did not contain 'worktree_create_failed' + the git error; payloads: %s", joinPayloads(evs))
	}
}

func joinPayloads(evs []stubEmittedEvent) string {
	parts := make([]string, len(evs))
	for i, ev := range evs {
		parts[i] = string(ev.Payload)
	}
	return strings.Join(parts, " | ")
}
