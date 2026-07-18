package daemon

// hk3hozm_slot_leak_test.go — Regression test for the daemon remote worker-slot
// leak fixed in bead hk-3hozm.
//
// THE BUG (now fixed): beadRunOne receives a pre-reserved remote worker slot via
// its preSelectedWorker param. The outer dispatch loop reserves it by calling
// deps.workerRegistry.SelectWorker() (which increments the registry's InFlight
// count), and the caller MUST balance that reservation with ReleaseSlot(). The
// balancing `defer deps.workerRegistry.ReleaseSlot()` used to be registered ONLY
// inside the remote-runner-setup block (`if preSelectedWorker != nil { ... }`),
// which is reached only AFTER four "refuse-before-launch" early returns:
//   1. bad pi profile         (resolvePiProfile err)
//   2. CrossRepoUnsafeError    (target_repo not on the allowed_repos safelist)
//   3. unresolvable start_from  (resolveParentCommit err)
//   4. LandsOnProtected         (lands_on in ProtectBranches)
// A remote bead that hit ANY of those returned WITHOUT releasing → the registry
// slot leaked permanently. After MaxSlots such refusals HasFreeSlot()==false
// forever and the remote dispatch path wedged.
//
// THE FIX (workloop.go, tag hk-3hozm): the release was hoisted to a top-level
// `defer` at the START of beadRunOne (the relWorkerSlot defer), keyed on
// preSelectedWorker != nil, so the pre-reserved slot is now released on EVERY
// return path including the four early ones.
//
// THE REPRO below drives the first early return (bad pi profile) — the cleanest
// to force in a unit harness (no git repo / worktree needed) and identical to
// the seam exercised by pi_unknown_profile_refuse_test.go. A pre-reserved worker
// slot is held (reg.SelectWorker() → InFlight()==1) before the call; the fix
// makes InFlight() return to 0 after the refuse. With the pre-fix code it would
// stay 1 (leaked).
//
// This is an IN-PACKAGE (white-box) test — beadRunOne is unexported and there is
// no ExportedBeadRunOne seam, so, exactly like pi_unknown_profile_refuse_test.go,
// this file lives in package daemon to call beadRunOne directly.
//
// Helper prefix: hk3hozm.
//
// Bead: hk-3hozm.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workers"
)

// hk3hozmReopenCall records one ReopenBead invocation so the test can confirm
// the refuse-before-launch path actually fired (and named the unknown profile).
type hk3hozmReopenCall struct {
	beadID core.BeadID
	reason string
}

// hk3hozmLedger is a capturing beadLedger: it records every ReopenBead call.
// beadRunOne calls only ReopenBead (and resolveOwningEpicFromRecord, which
// returns early for an edge-less record) on this refuse-before-launch path;
// the other methods are inert no-ops.
type hk3hozmLedger struct {
	mu      sync.Mutex
	reopens []hk3hozmReopenCall
}

func (l *hk3hozmLedger) Ready(context.Context) ([]core.BeadRecord, error) { return nil, nil }

func (l *hk3hozmLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id}, nil
}

func (l *hk3hozmLedger) ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error {
	return nil
}

func (l *hk3hozmLedger) CloseBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, bool) error {
	return nil
}

func (l *hk3hozmLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reopens = append(l.reopens, hk3hozmReopenCall{beadID: beadID, reason: reason})
	return nil
}

func (l *hk3hozmLedger) calls() []hk3hozmReopenCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]hk3hozmReopenCall, len(l.reopens))
	copy(out, l.reopens)
	return out
}

// hk3hozmSealedAdapterRegistry registers the real ClaudeCode adapter then seals
// it via ForAgent — beadRunOne's hk-d8u1y precondition requires a non-nil sealed
// registry even though this refuse-before-launch path never reaches launch.
func hk3hozmSealedAdapterRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("hk3hozm: register claude adapter: %v", err)
	}
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) // seal
	return reg
}

// TestHK3hozm_RefuseBeforeLaunchReleasesWorkerSlot proves the hk-3hozm fix: a
// remote bead that is refused before launch (here: unknown pi profile) releases
// its pre-reserved worker slot, so the registry's InFlight count returns to 0.
// Pre-fix, the ReleaseSlot defer lived below the refuse and never ran, so
// InFlight would stay at 1 — the permanent leak that wedges the remote path.
func TestHK3hozm_RefuseBeforeLaunchReleasesWorkerSlot(t *testing.T) {
	const unknownProfile = "does-not-exist"

	// A registry with ONE enabled worker, MaxSlots >= 1 — the remote-dispatch
	// worker whose slot the outer loop pre-reserves for this run.
	reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
		Name:     "hk3hozm-worker",
		Host:     "hk3hozm.example.com",
		Enabled:  true,
		MaxSlots: 1,
	}}})

	// Mirror the outer dispatch loop: reserve the slot BEFORE calling beadRunOne.
	// This is the preSelectedWorker beadRunOne receives and must balance.
	preSelectedWorker := reg.SelectWorker()
	if preSelectedWorker == nil {
		t.Fatal("setup: SelectWorker returned nil; expected a reserved worker slot")
	}
	if got := reg.InFlight(); got != 1 {
		t.Fatalf("setup: InFlight = %d; want 1 (slot pre-reserved before beadRunOne)", got)
	}

	ledger := &hk3hozmLedger{}
	adapterReg := hk3hozmSealedAdapterRegistry(t)

	// harnesses.pi.profiles does NOT contain "does-not-exist" — the reference is
	// a real existence-check failure (not an accidentally-empty map), so
	// resolvePiProfile returns an error and beadRunOne takes the FIRST of the
	// four refuse-before-launch early returns.
	projectCfg := ProjectConfig{
		Harnesses: HarnessesConfig{
			Pi: PiHarnessConfig{
				Profiles: map[string]PiProfileConfig{
					"real-profile": {
						Provider:  "hk3hozm-provider",
						Model:     "hk3hozm-provider/some-id",
						APIKeyEnv: "HK3HOZM_PI_KEY",
					},
				},
			},
		},
	}

	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              eventbus.NewBusImpl(), // real bus avoids a nil-interface panic; no event assertions
		ProjectDir:       t.TempDir(),
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     t.TempDir(),
		MaxConcurrent:    1,
		AdapterRegistry2: adapterReg,
		ProjectCfg:       projectCfg,
		DefaultHarness:   core.AgentTypePi,
		WorkerRegistry:   reg, // the pre-reserved slot's owner; fix must release through this
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runID := core.RunID(uuid.New())
	beadRecord := core.BeadRecord{
		BeadID:   core.BeadID("hk-3hozm-slot-leak-probe"),
		Title:    "worker-slot leak refuse-before-launch probe",
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
		Labels:   []string{"profile:" + unknownProfile},
	}

	// Drive beadRunOne DIRECTLY with the pre-reserved worker (preSelectedWorker)
	// and localSlotHeld=false. This reaches the resolvePiProfile refuse gate and
	// returns — exercising exactly the early-return path that used to leak.
	beadRunOne(ctx, deps, runID, beadRecord,
		"", nil, nil, 0, "", "", "", nil,
		false, "", preSelectedWorker, false)

	// The refuse must actually have fired (guards against the bead sneaking past
	// the early return and making the InFlight assertion pass for the wrong
	// reason).
	calls := ledger.calls()
	if len(calls) != 1 {
		t.Fatalf("ReopenBead call count = %d; want exactly 1 (the unknown-profile refuse)\ncalls=%+v", len(calls), calls)
	}
	if calls[0].beadID != beadRecord.BeadID {
		t.Errorf("ReopenBead beadID = %q; want %q", calls[0].beadID, beadRecord.BeadID)
	}
	if !strings.Contains(calls[0].reason, unknownProfile) {
		t.Errorf("ReopenBead reason = %q; want it to name the unknown profile %q", calls[0].reason, unknownProfile)
	}

	// THE hk-3hozm ASSERTION: the pre-reserved slot was released on the early
	// return, so InFlight is back to 0. Pre-fix this stayed at 1 (leaked).
	if got := reg.InFlight(); got != 0 {
		t.Fatalf("InFlight after refuse-before-launch = %d; want 0 (pre-reserved worker slot must be released on the early return). "+
			"A non-zero count is the hk-3hozm leak: the ReleaseSlot defer used to sit below the refuse and never ran.", got)
	}
	// And the released slot is reusable — the registry is not wedged.
	if !reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot after refuse = false; the worker slot was not returned to the pool (hk-3hozm wedge)")
	}
}
