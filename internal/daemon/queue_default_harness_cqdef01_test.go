package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

type cqDef01OpenLedger struct{ n5md3Ledger }

func (cqDef01OpenLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func cqDef01Queue(defaultHarness core.AgentType, labels []string) (*queuewiring.QueueStore, core.BeadRecord) {
	bead := core.BeadRecord{
		BeadID: core.BeadID("cq-def-01"),
		Labels: labels,
	}
	qs := queuewiring.NewQueueStore()
	qs.SetQueueByName("main", &queue.Queue{
		SchemaVersion:  1,
		QueueID:        "cq-def-01-qid",
		Name:           "main",
		Status:         queue.QueueStatusActive,
		DefaultHarness: defaultHarness,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{{
				BeadID: bead.BeadID,
				Status: queue.ItemStatusPending,
			}},
		}},
	})
	return qs, bead
}

func cqDef01PiRegistry(t *testing.T) *handlercontract.HarnessRegistry {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "pi-key")
	if err := os.WriteFile(keyFile, []byte("test-key\n"), 0o600); err != nil {
		t.Fatalf("write Pi key fixture: %v", err)
	}
	reg, err := newHarnessRegistry(projectconfig.PiHarnessConfig{
		Provider:   "cq-def-01-provider",
		Model:      "cq-def-01-model",
		APIKeyEnv:  "CQ_DEF_01_PI_KEY",
		APIKeyFile: keyFile,
		BaseURL:    "http://127.0.0.1:1/v1",
		API:        "openai-completions",
	})
	if err != nil {
		t.Fatalf("newHarnessRegistry: %v", err)
	}
	return reg
}

// TestSelectQueueDefaultHarness covers harness precedence after the pure
// selector has preserved the selected queue's tier-2 value.
func TestSelectQueueDefaultHarness(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		labels        []string
		queueDefault  core.AgentType
		globalDefault core.AgentType
		want          core.AgentType
		compareLegacy bool
	}{
		{
			name:          "valid tier-1 label wins",
			labels:        []string{"harness:codex"},
			queueDefault:  core.AgentTypePi,
			globalDefault: core.AgentTypeClaudeCode,
			want:          core.AgentTypeCodex,
		},
		{
			name:          "empty queue default preserves golden fallback",
			queueDefault:  core.AgentType(""),
			globalDefault: core.AgentTypeCodex,
			want:          core.AgentTypeCodex,
			compareLegacy: true,
		},
		{
			name:          "valid non-pi queue default is not hard-coded away",
			queueDefault:  core.AgentTypeCodex,
			globalDefault: core.AgentTypeClaudeCode,
			want:          core.AgentTypeCodex,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			qs, bead := cqDef01Queue(tc.queueDefault, tc.labels)
			lq := qs.LockForMutation()
			sel, ok := selectNextQueue(lq, NewRunRegistry(), 1, 0, nil)
			lq.Done()
			if !ok {
				t.Fatal("selectNextQueue returned no selection")
			}
			got := resolveHarnessAgentTypeQuiet(
				bead, sel.queueDefaultHarness, core.AgentType(""), tc.globalDefault,
			)
			if got != tc.want {
				t.Fatalf("resolved harness = %q; want %q", got, tc.want)
			}
			if tc.compareLegacy {
				legacy := resolveHarnessAgentTypeQuiet(
					bead, core.AgentType(""), core.AgentType(""), tc.globalDefault,
				)
				if got != legacy {
					t.Fatalf("empty queue default changed legacy routing: got %q, legacy %q", got, legacy)
				}
			}
		})
	}
}

// TestQueueDefaultHarnessProductionPath drives runWorkLoop itself so the queue
// default must survive selection, capture into the goroutine parameter, RunEnv
// construction, and beadRunOne's quiet harness resolution.
func TestQueueDefaultHarnessProductionPath(t *testing.T) {
	qs, bead := cqDef01Queue(core.AgentTypePi, nil)
	projectDir := n5md3RepoWithCommit(t)
	configDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "queues"), 0o750); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	configBody := []byte(`schema_version: 1
agents:
  claude-code:
    model: cq-def-global-claude
  pi:
    model: cq-def-queue-pi
`)
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), configBody, 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	projectCfg, err := projectconfig.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	t.Setenv(EnvModelKey, "")
	t.Setenv(EnvEffortKey, "")

	errStopAfterQuietResolution := errors.New("cq-def-01: stop after quiet resolution")
	modelC := make(chan string, 1)
	launchBuilder := func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		modelC <- rc.Model
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, errStopAfterQuietResolution
	}
	wtPath := t.TempDir()
	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		return wtPath, func() {}, nil
	}

	bus := &n5md3Collector{}
	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:           cqDef01OpenLedger{},
		Bus:                 bus,
		ProjectDir:          projectDir,
		IntentLogDir:        filepath.Join(configDir, "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeSingle,
		AdapterRegistry2:    n5md3SealedAdapterRegistry(t),
		HarnessRegistry:     cqDef01PiRegistry(t),
		ProjectCfg:          projectCfg,
		LaunchSpecBuilder:   launchBuilder,
		WorktreeFactory:     worktreeFactory,
		QueueStore:          qs,
		NoAutoPull:          true,
		DefaultHarness:      core.AgentTypeClaudeCode,
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runWorkLoop(ctx, deps)
	}()

	select {
	case got := <-modelC:
		if got != "cq-def-queue-pi" {
			t.Fatalf("beadRunOne resolved model = %q; want queue Pi model %q (global Claude model is %q)",
				got, "cq-def-queue-pi", "cq-def-global-claude")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not reach beadRunOne launch builder")
	}
	runDoneDeadline := time.Now().Add(5 * time.Second)
	for deps.runRegistry.Len() != 0 && time.Now().Before(runDoneDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if deps.runRegistry.Len() != 0 {
		t.Fatal("beadRunOne goroutine did not finish after launch-builder stop")
	}
	cancel()
	select {
	case loopErr := <-done:
		if loopErr != nil {
			t.Fatalf("runWorkLoop: %v", loopErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not stop after cancellation")
	}

	// Exercise the production (nil override) buildRunBundles routing boundary
	// separately from the injected beadRunOne observation above. Calling the
	// returned builder is hermetic: it materializes a launch specification but
	// never starts the external process.
	builderDeps := deps
	builderDeps.launchSpecBuilder = nil
	builderEnv := builderDeps.runEnv(
		core.RunID{}, bead, "main", nil, nil, 0, "", "", nil, false, "", core.AgentTypePi,
	)
	builderPorts, _ := builderDeps.buildRunBundles(builderEnv)
	if builderPorts.LaunchBuilder == nil {
		t.Fatal("production buildRunBundles returned a nil launch builder")
	}
	_, artifacts, buildErr := builderPorts.LaunchBuilder(t.Context(), shared.LaunchCtx{
		BeadID:        string(bead.BeadID),
		WorkspacePath: t.TempDir(),
	})
	if buildErr != nil {
		t.Fatalf("production buildRunBundles launch builder: %v", buildErr)
	}
	if got := artifacts.ResolvedAgentType; got != core.AgentTypePi {
		t.Fatalf("production buildRunBundles resolved harness = %q; want pi", got)
	}
}

// TestQueueDefaultHarnessDoesNotOverrideGlobalReviewerDefault proves the queue
// tier remains separate from the global tier and that DOT reviewer-class paths
// correct an inherited Pi implementer harness to the supported Claude harness.
func TestQueueDefaultHarnessDoesNotOverrideGlobalReviewerDefault(t *testing.T) {
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", filepath.Join(t.TempDir(), "claude.json"))
	reg := cqDef01PiRegistry(t)
	bead := core.BeadRecord{BeadID: core.BeadID("cq-def-01-reviewer")}

	// An implementer node with no harness pin reaches dot_cascade_core.go's
	// nodeModelHarness quiet-resolution branch. A Pi effective harness must
	// ignore the Claude-scoped node model and retain the run-level Pi model.
	implementerModelC := make(chan string, 1)
	errStopAtImplementerBuilder := errors.New("cq-def-01: stop at DOT implementer builder")
	implementerBuilder := func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		implementerModelC <- rc.Model
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, errStopAtImplementerBuilder
	}
	implementerDir := t.TempDir()
	implementerDeps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:           n5md3Ledger{},
		Bus:                 &n5md3Collector{},
		ProjectDir:          implementerDir,
		HandlerBinary:       filepath.Join(implementerDir, "no-such-agent-cq-def-01"),
		IntentLogDir:        filepath.Join(implementerDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    n5md3SealedAdapterRegistry(t),
		HarnessRegistry:     reg,
		LaunchSpecBuilder:   implementerBuilder,
		DefaultHarness:      core.AgentTypeClaudeCode,
	})
	implementerEnv := implementerDeps.runEnv(
		core.RunID{}, bead, "", nil, nil, -1, "", "", nil, false, "", core.AgentTypePi,
	)
	implementerPorts, implementerHandles := implementerDeps.buildRunBundles(implementerEnv)
	implementerNode := &dot.Node{
		ID:         "cq_def_01_implementer",
		Type:       core.NodeTypeAgentic,
		AgentType:  "implementer",
		HandlerRef: "implementer",
		Model:      "cq-def-node-claude-model",
	}
	implementerSessionID := ""
	_, implementerDispatchErr := dispatchDotAgenticNode(
		t.Context(), implementerEnv, implementerPorts, implementerHandles,
		core.RunID{}, bead.BeadID, bead, "implementer fixture", "implementer fixture body",
		implementerDir, "", "", implementerNode, false, 1, &implementerSessionID,
		"cq-def-queue-pi-model", "", "", "main", core.AgentType(""),
		nil, "", "", "", false,
	)
	select {
	case got := <-implementerModelC:
		if got != "cq-def-queue-pi-model" {
			t.Fatalf("DOT implementer node model = %q; want run-level Pi model %q (Claude node model is %q)",
				got, "cq-def-queue-pi-model", implementerNode.Model)
		}
	default:
		t.Fatal("DOT implementer dispatch did not reach the launch builder")
	}
	if !errors.Is(implementerDispatchErr, errStopAtImplementerBuilder) {
		t.Fatalf("DOT implementer dispatch error = %v; want launch-builder sentinel", implementerDispatchErr)
	}

	wtPath := n5md3RepoWithCommit(t)
	parentSHA, err := resolveHEAD(t.Context(), wtPath)
	if err != nil {
		t.Fatalf("resolveHEAD: %v", err)
	}
	bus := &n5md3Collector{}
	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:           n5md3Ledger{},
		Bus:                 bus,
		ProjectDir:          wtPath,
		HandlerBinary:       filepath.Join(wtPath, "no-such-agent-cq-def-01"),
		IntentLogDir:        filepath.Join(wtPath, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    n5md3SealedAdapterRegistry(t),
		HarnessRegistry:     reg,
		DefaultHarness:      core.AgentTypeClaudeCode,
	})
	env := deps.runEnv(
		core.RunID{}, bead, "", nil, nil, -1, "", "", nil, false, "", core.AgentTypePi,
	)
	ports, handles := deps.buildRunBundles(env)
	node := &dot.Node{
		ID:         "cq_def_01_reviewer",
		Type:       core.NodeTypeAgentic,
		AgentType:  "reviewer",
		HandlerRef: "claude-reviewer",
	}
	claudeSessionID := ""
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, dispatchErr := dispatchDotAgenticNode(
		ctx, env, ports, handles, core.RunID{}, bead.BeadID, bead,
		"reviewer fixture", "reviewer fixture body", wtPath, parentSHA, "",
		node, true, 1, &claudeSessionID, "", "", "", "main",
		core.AgentType(""), nil, "", "", "", false,
	)

	selected := cqDef01HarnessSelected(t, bus)
	if len(selected) != 1 {
		t.Fatalf("DOT cascade emitted %d harness_selected events; want 1 (dispatch error: %v)",
			len(selected), dispatchErr)
	}
	if got := core.AgentType(selected[0].AgentType); got != core.AgentTypeClaudeCode {
		t.Fatalf("DOT cascade reviewer harness = %q; want claude-code", got)
	}
	if selected[0].Tier != 3 {
		t.Fatalf("DOT cascade reviewer tier = %d; want pinned reviewer tier 3", selected[0].Tier)
	}
}

func cqDef01HarnessSelected(t *testing.T, bus *n5md3Collector) []core.HarnessSelectedPayload {
	t.Helper()
	bus.mu.Lock()
	defer bus.mu.Unlock()
	selected := make([]core.HarnessSelectedPayload, 0, len(bus.events))
	for _, event := range bus.events {
		if event.typ != core.EventTypeHarnessSelected {
			continue
		}
		var payload core.HarnessSelectedPayload
		if err := json.Unmarshal(event.payload, &payload); err != nil {
			t.Fatalf("decode harness_selected: %v", err)
		}
		selected = append(selected, payload)
	}
	return selected
}
