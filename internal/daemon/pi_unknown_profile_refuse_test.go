package daemon

// pi_unknown_profile_refuse_test.go — Scenario 2's REQUIRED unknown-profile
// refuse-to-launch sub-case (pi-provider-switch C5-wiring, hk-m6uu2.6;
// integration-review finding #1). Extends resolvePiProfile's unit-level
// TestResolvePiProfile_UnknownProfile_FailLoud (pi_profile_resolve_test.go)
// up to the WORKLOOP: drives a pi-resolved bead carrying an unknown
// profile:<name> label through beadRunOne directly and asserts the workloop
// refuses to launch — NO LaunchSpec is built and the bead is routed to
// brAdapter.ReopenBead with the unknown profile named in the reason.
//
// This is an IN-PACKAGE (white-box) test: beadRunOne is unexported, so this
// file lives in package daemon (mirroring workloop_gate_n5md3_test.go's
// pattern of driving beadRunOne directly with a fake brAdapter) rather than
// package daemon_test like the other C5 scenario files, which only need the
// Exported* seams.
//
// The unknown-profile refuse fires at workloop.go:3099-3109, strictly BEFORE
// resolveParentCommit / worktree creation, so this test needs no git repo and
// no worktree factory — it observes the refuse via the fake brAdapter's
// captured ReopenBead call and confirms no launch-spec builder was ever
// invoked.
//
// Helper prefix: hkppsNoLaunch (per implementer-protocol.md §Helper-prefix
// discipline).
//
// Bead: hk-m6uu2.6 (pi-provider-switch C5-wiring). Guards C3 requirement 5
// (unknown profile → fail loud, does NOT launch).

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
)

// hkppsNoLaunchReopenCall records one ReopenBead invocation.
type hkppsNoLaunchReopenCall struct {
	beadID core.BeadID
	reason string
}

// hkppsNoLaunchLedger is a capturing beadLedger: records every ReopenBead
// call so the test can assert the workloop refused to launch and named the
// unknown profile in the reason. Other methods are inert no-ops — beadRunOne
// calls only ReopenBead and resolveOwningEpicFromRecord (returns early for an
// edge-less record) on this refuse-before-launch path.
type hkppsNoLaunchLedger struct {
	mu      sync.Mutex
	reopens []hkppsNoLaunchReopenCall
}

func (l *hkppsNoLaunchLedger) Ready(context.Context) ([]core.BeadRecord, error) { return nil, nil }

func (l *hkppsNoLaunchLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id}, nil
}

func (l *hkppsNoLaunchLedger) ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error {
	return nil
}

func (l *hkppsNoLaunchLedger) CloseBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, bool) error {
	return nil
}

func (l *hkppsNoLaunchLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reopens = append(l.reopens, hkppsNoLaunchReopenCall{beadID: beadID, reason: reason})
	return nil
}

func (l *hkppsNoLaunchLedger) calls() []hkppsNoLaunchReopenCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]hkppsNoLaunchReopenCall, len(l.reopens))
	copy(out, l.reopens)
	return out
}

// hkppsNoLaunchSealedAdapterRegistry mirrors n5md3SealedAdapterRegistry: register
// the real ClaudeCode adapter, then seal via ForAgent. beadRunOne's hk-d8u1y
// precondition requires a non-nil sealed registry, even though this test's
// refuse-before-launch path never reaches waitAgentReady.
func hkppsNoLaunchSealedAdapterRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("hkppsNoLaunch: register claude adapter: %v", err)
	}
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) // seal
	return reg
}

// TestPi_UnknownProfile_WorkloopRefusesLaunch is the end-to-end workloop
// assertion (C3 fail-loud e2e, integration-review finding #1): a pi-resolved
// bead carrying `profile:does-not-exist` (absent from harnesses.pi.profiles)
// MUST be refused before any launch spec is built. beadRunOne must route the
// bead to brAdapter.ReopenBead naming the unknown profile in the reason, and
// return without producing an argv/launch-spec (verified indirectly: this
// test wires NO WorktreeFactory / launchSpecBuilder override, so if the
// workloop proceeded past the refuse it would panic on nil worktree creation
// rather than silently succeed).
func TestPi_UnknownProfile_WorkloopRefusesLaunch(t *testing.T) {
	const unknownProfile = "does-not-exist"

	ledger := &hkppsNoLaunchLedger{}
	adapterReg := hkppsNoLaunchSealedAdapterRegistry(t)

	// harnesses.pi.profiles does NOT contain "does-not-exist" — only a
	// differently-named profile, so the unknown reference is a real
	// existence-check failure, not an accidentally-empty map.
	projectCfg := ProjectConfig{
		Harnesses: HarnessesConfig{
			Pi: PiHarnessConfig{
				Profiles: map[string]PiProfileConfig{
					"ornith-dgx": {
						Provider:  "ornith-provider",
						Model:     "ornith-provider/some-id",
						APIKeyEnv: "HKPPS_NO_LAUNCH_PI_KEY",
					},
				},
			},
		},
	}

	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              eventbus.NewBusImpl(), // no assertions on emitted events; a real bus avoids a nil-interface panic
		ProjectDir:       t.TempDir(),
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     t.TempDir(),
		MaxConcurrent:    1,
		AdapterRegistry2: adapterReg,
		ProjectCfg:       projectCfg,
		DefaultHarness:   core.AgentTypePi,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runID := core.RunID(uuid.New())
	beadRecord := core.BeadRecord{
		BeadID:   core.BeadID("hk-m6uu2-unknown-profile-bead"),
		Title:    "unknown-profile refuse-to-launch probe",
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
		Labels:   []string{"profile:" + unknownProfile},
	}

	// Drive beadRunOne DIRECTLY — the smallest seam that reaches the
	// resolvePiProfile refuse gate at workloop.go:3099-3109, bypassing the
	// whole work loop (mirrors workloop_gate_n5md3_test.go).
	beadRunOne(ctx, deps, runID, beadRecord,
		"", nil, nil, 0, nil, "", "", "", nil,
		false, "", nil, false)

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
}
