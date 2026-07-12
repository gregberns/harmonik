package daemon

// pi_provider_selected_hk8ziid2_test.go — drives beadRunOne end-to-end through
// the claim-time Pi profile resolver and asserts C2's two observable effects
// (hk-8ziid.2, docs/design/pi-multi-provider-slot-accounting.md):
//
//  1. RunHandle.SetResolvedProvider is called with the resolved provider
//     string (the matched profile's provider, or the harness-global
//     harnesses.pi.provider default when no profile: label matched) —
//     observed via RunRegistry.Get(runID).GetResolvedProvider() after the run.
//  2. A provider_selected event carrying the same value is emitted on the bus.
//
// Mirrors workloop_gate_n5md3_test.go's pattern for the git repo + sealed
// claude adapter registry, but stops the run via an UNRESOLVABLE `start_from`
// ref (pi_unknown_profile_refuse_test.go's refuse-before-launch shape) rather
// than an erroring WorktreeFactory: resolveParentCommit's StartFromRefError
// path (workloop.go, immediately after the profile-resolution block under
// test) reopens the bead and returns WITHOUT calling emitDone — unlike a
// WorktreeFactory error, which calls emitDone and spawns emitDone's
// sessiondata.Collect goroutine (workloop.go ~3143) reading deps.projectDir
// asynchronously, racing this test's t.TempDir() cleanup of that same
// directory (observed as an intermittent "TempDir RemoveAll cleanup:
// directory not empty" failure). No WorktreeFactory is needed at all: the
// run never reaches worktree creation.
//
// Bead: hk-8ziid.2 (MR2 C2-wiring).

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// hk8ziid2BranchingBody is a bead description carrying a `## Branching`
// section whose start_from names a ref absent from the throwaway repo
// (n5md3RepoWithCommit only ever creates "main"), so resolveParentCommit
// fails fast with a *StartFromRefError immediately after the
// profile-resolution block under test — before any worktree/launch-spec work,
// and without the emitDone/sessiondata.Collect goroutine a WorktreeFactory
// error would trigger (see file-level comment above).
const hk8ziid2BranchingBody = "## Summary\n\nprobe.\n\n## Branching\n\n```yaml\nstart_from: hk8ziid2-no-such-ref\n```\n"

// hk8ziid2Ledger is a no-op beadLedger: these runs stop at resolveParentCommit,
// well before ClaimBead/CloseBead would fire; ReopenBead is called once.
type hk8ziid2Ledger struct{}

func (hk8ziid2Ledger) Ready(context.Context) ([]core.BeadRecord, error) { return nil, nil }
func (hk8ziid2Ledger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id}, nil
}
func (hk8ziid2Ledger) ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error {
	return nil
}
func (hk8ziid2Ledger) CloseBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, bool) error {
	return nil
}
func (hk8ziid2Ledger) ReopenBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, string) error {
	return nil
}

// hk8ziid2Collector records every emitted (eventType, payload) pair.
type hk8ziid2Collector struct {
	mu     sync.Mutex
	events []hk8ziid2Event
}

type hk8ziid2Event struct {
	typ     core.EventType
	payload []byte
}

func (c *hk8ziid2Collector) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	c.record(eventType, payload)
	return nil
}

func (c *hk8ziid2Collector) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	c.record(eventType, payload)
	return nil
}

func (c *hk8ziid2Collector) record(eventType core.EventType, payload []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	c.events = append(c.events, hk8ziid2Event{typ: eventType, payload: cp})
}

// providerSelected returns the payload of the first provider_selected event
// recorded, if any.
func (c *hk8ziid2Collector) providerSelected(t *testing.T) (core.ProviderSelectedPayload, bool) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e.typ != core.EventTypeProviderSelected {
			continue
		}
		var pl core.ProviderSelectedPayload
		if err := json.Unmarshal(e.payload, &pl); err != nil {
			t.Fatalf("hk8ziid2: decode provider_selected payload: %v\nraw: %s", err, e.payload)
		}
		return pl, true
	}
	return core.ProviderSelectedPayload{}, false
}

// TestBeadRunOne_ProviderSelected_ProfileMatch drives a Pi-resolved bead
// carrying a `profile:<name>` label that matches harnesses.pi.profiles, and
// asserts the resolved provider is the PROFILE's provider (not the
// harness-global default) — on both the run handle and the emitted event.
func TestBeadRunOne_ProviderSelected_ProfileMatch(t *testing.T) {
	projectDir := n5md3RepoWithCommit(t)
	adapterReg := n5md3SealedAdapterRegistry(t)
	collector := &hk8ziid2Collector{}
	runRegistry := ExportedNewRunRegistry()

	runID := core.RunID(uuid.New())
	ExportedRunRegistryRegister(runRegistry, runID, &RunHandle{BeadID: core.BeadID("hk-8ziid2-match-bead")})

	projectCfg := ProjectConfig{
		Harnesses: HarnessesConfig{
			Pi: PiHarnessConfig{
				Provider: "harness-global-default",
				Profiles: map[string]PiProfileConfig{
					"ornith-dgx": {
						Provider:  "ornith-provider",
						Model:     "ornith-provider/some-id",
						APIKeyEnv: "HK8ZIID2_PI_KEY",
					},
				},
			},
		},
	}

	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:        hk8ziid2Ledger{},
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     t.TempDir(),
		MaxConcurrent:    1,
		AdapterRegistry2: adapterReg,
		ProjectCfg:       projectCfg,
		DefaultHarness:   core.AgentTypePi,
		RunRegistry:      runRegistry,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	beadRecord := core.BeadRecord{
		BeadID:      core.BeadID("hk-8ziid2-match-bead"),
		Title:       "provider_selected profile-match probe",
		Description: hk8ziid2BranchingBody,
		BeadType:    "task",
		Status:      core.CoarseStatusOpen,
		Labels:      []string{"profile:ornith-dgx"},
	}

	beadRunOne(ctx, deps, runID, beadRecord,
		"", nil, nil, 0, nil, "", "", "", nil,
		false, "", nil, false)

	rh, ok := runRegistry.Get(runID)
	if !ok || rh == nil {
		t.Fatalf("run handle for %s not found in registry after run", runID)
	}
	gotProvider, resolvedOK := rh.GetResolvedProvider()
	if !resolvedOK || gotProvider != "ornith-provider" {
		t.Errorf("RunHandle.GetResolvedProvider() = (%q, %v); want (\"ornith-provider\", true)", gotProvider, resolvedOK)
	}

	pl, gotEvent := collector.providerSelected(t)
	if !gotEvent {
		t.Fatalf("no provider_selected event emitted")
	}
	if pl.RunID != runID.String() {
		t.Errorf("provider_selected run_id = %q; want %q", pl.RunID, runID.String())
	}
	if pl.Provider != "ornith-provider" {
		t.Errorf("provider_selected provider = %q; want %q", pl.Provider, "ornith-provider")
	}
}

// TestBeadRunOne_ProviderSelected_NoProfile_UsesGlobalDefault drives a
// Pi-resolved bead carrying NO profile: label and asserts the resolved
// provider falls back to the harness-global harnesses.pi.provider default —
// on both the run handle and the emitted event — per the design's state (2)
// ("resolved to the harness-global default", distinct from "not yet resolved").
func TestBeadRunOne_ProviderSelected_NoProfile_UsesGlobalDefault(t *testing.T) {
	projectDir := n5md3RepoWithCommit(t)
	adapterReg := n5md3SealedAdapterRegistry(t)
	collector := &hk8ziid2Collector{}
	runRegistry := ExportedNewRunRegistry()

	runID := core.RunID(uuid.New())
	ExportedRunRegistryRegister(runRegistry, runID, &RunHandle{BeadID: core.BeadID("hk-8ziid2-noprofile-bead")})

	projectCfg := ProjectConfig{
		Harnesses: HarnessesConfig{
			Pi: PiHarnessConfig{
				Provider: "harness-global-default",
			},
		},
	}

	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:        hk8ziid2Ledger{},
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     t.TempDir(),
		MaxConcurrent:    1,
		AdapterRegistry2: adapterReg,
		ProjectCfg:       projectCfg,
		DefaultHarness:   core.AgentTypePi,
		RunRegistry:      runRegistry,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	beadRecord := core.BeadRecord{
		BeadID:      core.BeadID("hk-8ziid2-noprofile-bead"),
		Title:       "provider_selected no-profile probe",
		Description: hk8ziid2BranchingBody,
		BeadType:    "task",
		Status:      core.CoarseStatusOpen,
	}

	beadRunOne(ctx, deps, runID, beadRecord,
		"", nil, nil, 0, nil, "", "", "", nil,
		false, "", nil, false)

	rh, ok := runRegistry.Get(runID)
	if !ok || rh == nil {
		t.Fatalf("run handle for %s not found in registry after run", runID)
	}
	gotProvider, resolvedOK := rh.GetResolvedProvider()
	if !resolvedOK || gotProvider != "harness-global-default" {
		t.Errorf("RunHandle.GetResolvedProvider() = (%q, %v); want (\"harness-global-default\", true)", gotProvider, resolvedOK)
	}

	pl, gotEvent := collector.providerSelected(t)
	if !gotEvent {
		t.Fatalf("no provider_selected event emitted")
	}
	if pl.Provider != "harness-global-default" {
		t.Errorf("provider_selected provider = %q; want %q", pl.Provider, "harness-global-default")
	}
}

// TestBeadRunOne_ProviderSelected_NonPiRun_LeavesUnresolved drives a
// claude-resolved bead (no harness:pi label, DefaultHarness left at the
// claude-code built-in fallback) through beadRunOne and asserts NEITHER
// SetResolvedProvider NOR provider_selected fires: the run handle's resolved
// provider must stay ("", false) — distinct from state (2)'s ("", true) — and
// no provider_selected event is emitted, per the design's "permanently
// unresolved" contract for non-Pi runs (docs/design/pi-multi-provider-slot-accounting.md).
func TestBeadRunOne_ProviderSelected_NonPiRun_LeavesUnresolved(t *testing.T) {
	projectDir := n5md3RepoWithCommit(t)
	adapterReg := n5md3SealedAdapterRegistry(t)
	collector := &hk8ziid2Collector{}
	runRegistry := ExportedNewRunRegistry()

	runID := core.RunID(uuid.New())
	ExportedRunRegistryRegister(runRegistry, runID, &RunHandle{BeadID: core.BeadID("hk-8ziid2-nonpi-bead")})

	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:        hk8ziid2Ledger{},
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     t.TempDir(),
		MaxConcurrent:    1,
		AdapterRegistry2: adapterReg,
		RunRegistry:      runRegistry,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	beadRecord := core.BeadRecord{
		BeadID:      core.BeadID("hk-8ziid2-nonpi-bead"),
		Title:       "provider_selected non-Pi probe",
		Description: hk8ziid2BranchingBody,
		BeadType:    "task",
		Status:      core.CoarseStatusOpen,
	}

	beadRunOne(ctx, deps, runID, beadRecord,
		"", nil, nil, 0, nil, "", "", "", nil,
		false, "", nil, false)

	rh, ok := runRegistry.Get(runID)
	if !ok || rh == nil {
		t.Fatalf("run handle for %s not found in registry after run", runID)
	}
	if gotProvider, resolvedOK := rh.GetResolvedProvider(); resolvedOK || gotProvider != "" {
		t.Errorf("RunHandle.GetResolvedProvider() = (%q, %v); want (\"\", false) for a non-Pi run", gotProvider, resolvedOK)
	}
	if _, gotEvent := collector.providerSelected(t); gotEvent {
		t.Errorf("provider_selected event emitted for a non-Pi run; want none")
	}
}
