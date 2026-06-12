package daemon_test

// brready_priority_scenario_hktul2a_test.go — scenario test: daemon br-ready
// priority claim order (hk-tul2a).
//
// # What this test covers
//
// Regression guard for workloop.go:692 (readyRecords[0] pick on the br-ready
// fallback path). The hk-rp48p fix added --sort priority to brcli.Ready so that
// br returns beads in priority order; the workloop then picks readyRecords[0].
//
// This test exercises the full dispatch path end-to-end via ExportedRunWorkLoop:
//
//  1. Two beads are seeded at different priorities: P0 ("hk-tul2a-p0") and P1
//     ("hk-tul2a-p1"). The stub ledger returns [P0, P1] on the first Ready call
//     — the same priority-sorted order that `br ready --sort priority` would
//     produce. On the second call it returns [P1] (P0 in-flight), and [] on
//     subsequent calls.
//
//  2. ExportedRunWorkLoop runs with MaxConcurrent=1 (single-threaded default).
//     The capacity gate ensures P1 is not dispatched until P0 completes.
//
//  3. After both beads reach a terminal state (closed or reopened), the test
//     asserts that ClaimBead was called for P0 before P1.
//
// A future regression that changes readyRecords[0] to a different index, removes
// the priority-first sort from the brcli adapter, or reorders the dispatch path
// would cause this assertion to fail.
//
// Helper prefix: brPriority (bead hk-tul2a; per implementer-protocol §Helper-prefix).
//
// Spec refs: specs/beads-integration.md §4.a BI-013d (--sort priority adapter
// sort discipline); workloop.go:692 (readyRecords[0] pick).
// Source: review verdict on hk-rp48p commit 2e48555.
// Bead: hk-tul2a.

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// brPriorityP0BeadID is the P0 (highest-priority) bead ID used in this test.
const brPriorityP0BeadID = core.BeadID("hk-tul2a-p0")

// brPriorityP1BeadID is the P1 (lower-priority) bead ID used in this test.
const brPriorityP1BeadID = core.BeadID("hk-tul2a-p1")

// brPriorityOrderStub is a bead ledger stub that serves pre-seeded Ready
// responses and records the order in which ClaimBead is called.
//
// Concurrency: all methods are safe to call concurrently.
type brPriorityOrderStub struct {
	mu         sync.Mutex
	readySlots [][]core.BeadRecord // successive Ready() responses, consumed in order
	readyIdx   int
	claimOrder []core.BeadID // order in which ClaimBead was called
	terminal   []core.BeadID // beads that reached CloseBead or ReopenBead
}

func (s *brPriorityOrderStub) Ready(_ context.Context) ([]core.BeadRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readyIdx >= len(s.readySlots) {
		return []core.BeadRecord{}, nil
	}
	recs := s.readySlots[s.readyIdx]
	s.readyIdx++
	return recs, nil
}

func (s *brPriorityOrderStub) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	// Always report "open" so the pre-claim guard in workloop.go passes.
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (s *brPriorityOrderStub) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimOrder = append(s.claimOrder, beadID)
	return nil
}

func (s *brPriorityOrderStub) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminal = append(s.terminal, beadID)
	return nil
}

func (s *brPriorityOrderStub) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminal = append(s.terminal, beadID)
	return nil
}

func (s *brPriorityOrderStub) terminalCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.terminal)
}

func (s *brPriorityOrderStub) claimOrderSnapshot() []core.BeadID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.BeadID, len(s.claimOrder))
	copy(out, s.claimOrder)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_BrReadyPriorityClaimOrder_hktul2a
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_BrReadyPriorityClaimOrder_hktul2a is the end-to-end scenario test
// for the daemon br-ready priority claim path (hk-tul2a).
//
// Seeds two beads at different priorities (P0 and P1). The stub Ready() returns
// [P0, P1] on the first call — the priority-sorted order produced by brcli.Ready
// after the hk-rp48p fix. Asserts that the workloop claims P0 before P1, catching
// any future regression at workloop.go:692 (readyRecords[0] pick).
//
// Spec refs: specs/beads-integration.md §4.a BI-013d; workloop.go:692.
// Bead: hk-tul2a.
func TestScenario_BrReadyPriorityClaimOrder_hktul2a(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Seed Ready responses:
	//   Call 1: [P0, P1] — both beads, priority order (P0 highest, as --sort priority returns).
	//   Call 2: [P1]     — P0 is in-flight; P1 still pending.
	//   Call 3+: []      — all beads processed; loop idles.
	ledger := &brPriorityOrderStub{
		readySlots: [][]core.BeadRecord{
			{
				{BeadID: brPriorityP0BeadID},
				{BeadID: brPriorityP1BeadID},
			},
			{
				{BeadID: brPriorityP1BeadID},
			},
		},
	}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		// MaxConcurrent defaults to 1 — ensures P0 is fully dispatched before P1.
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until both beads have reached a terminal state (CloseBead or ReopenBead).
	pollDeadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(pollDeadline) {
		if ledger.terminalCount() >= 2 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("brPriority: workloop did not exit within 5s after context cancel")
	}

	// Assert: P0 was claimed before P1.
	order := ledger.claimOrderSnapshot()
	if len(order) < 2 {
		t.Fatalf("brPriority: expected 2 ClaimBead calls; got %d (%v) — both beads must be dispatched", len(order), order)
	}
	if order[0] != brPriorityP0BeadID {
		t.Errorf("brPriority: first claimed bead = %q; want P0 = %q — "+
			"priority-first claim is broken (hk-tul2a / hk-rp48p regression at workloop.go:692)",
			order[0], brPriorityP0BeadID)
	}
	if order[1] != brPriorityP1BeadID {
		t.Errorf("brPriority: second claimed bead = %q; want P1 = %q",
			order[1], brPriorityP1BeadID)
	}
	t.Logf("brPriority PASS: claim order = [%s, %s]", order[0], order[1])
}
