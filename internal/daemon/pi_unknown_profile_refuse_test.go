package daemon

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
	"github.com/gregberns/harmonik/internal/projectconfig"
)

type hkppsNoLaunchReopenCall struct {
	beadID core.BeadID
	reason string
}

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

	projectCfg := projectconfig.ProjectConfig{
		Harnesses: projectconfig.HarnessesConfig{
			Pi: projectconfig.PiHarnessConfig{
				Profiles: map[string]projectconfig.PiProfileConfig{
					"ornith-dgx": {
						Provider:  "ornith-provider",
						Model:     "ornith-provider/some-id",
						APIKeyEnv: "HKPPS_NO_LAUNCH_PI_KEY",
					},
				},
			},
		},
	}

	deps := ExportedTestRuntime(TestRuntimeParams{
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

	runBeadOneTest(ctx, deps, deps.runEnv(runID, beadRecord, "", "", core.AgentType("")),
		"", nil, false)

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
