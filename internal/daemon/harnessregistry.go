package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/codex"
	"github.com/gregberns/harmonik/internal/harness/pi"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/workspace"
)

func newHarnessRegistry(piCfg projectconfig.PiHarnessConfig) (*handlercontract.HarnessRegistry, error) {
	reg := handlercontract.NewHarnessRegistry()
	if err := reg.Register(core.AgentTypeClaudeCode, claude.NewHarness()); err != nil {
		return nil, fmt.Errorf("daemon: newHarnessRegistry: register claude harness: %w", err)
	}
	if err := reg.Register(core.AgentTypeCodex, codex.NewHarness("", "")); err != nil {
		return nil, fmt.Errorf("daemon: newHarnessRegistry: register codex harness: %w", err)
	}
	piH := pi.NewHarness(
		"", // piBinary: normalised to "pi" by pi.BuildLaunchSpec
		piCfg.Provider,
		piCfg.Model,
		piCfg.APIKeyEnv,
		piCfg.APIKeyFile,
		piCfg.BaseURL,
		piCfg.API,
	)
	if err := reg.Register(core.AgentTypePi, piH); err != nil {
		return nil, fmt.Errorf("daemon: newHarnessRegistry: register pi harness: %w", err)
	}
	return reg, nil
}

func effectiveModel(h handlercontract.Harness, rc shared.LaunchCtx) string {
	if piH, ok := h.(*pi.Harness); ok {
		if rc.Model != "" {
			return rc.Model
		}
		return piH.Model()
	}
	return rc.Model
}

func emitModelSelected(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	model string,
	agentType core.AgentType,
) {
	pl := core.ModelSelectedPayload{
		RunID:   runID.String(),
		Model:   model,
		Harness: string(agentType),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.Emit(ctx, core.EventTypeModelSelected, b); emitErr != nil {
		slog.WarnContext(ctx, "daemon: emit model_selected failed", "err", emitErr, "run_id", runID.String())
	}
}

func routedLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		agentType := resolveHarness(ctx, bead, queueDefault, nodeDefault, globalDefault, bus)

		h, err := reg.ForAgent(agentType)
		if err != nil {
			return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
				"daemon: routedLaunchSpecBuilder: resolve harness %q: %w", agentType, err)
		}

		emitModelSelected(ctx, bus, core.RunID(rc.RunID), effectiveModel(h, rc), agentType)

		if _, ok := h.(*claude.Harness); ok {
			return claude.BuildLaunchSpec(ctx, rc)
		}

		return buildCodexRoutedLaunchSpec(ctx, rc, h, agentType)
	}
}

func pinnedHarnessLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	agentType core.AgentType,
	bus handlercontract.EventEmitter,
) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		emitHarnessSelected(ctx, bus, bead, agentType, 3)
		h, err := reg.ForAgent(agentType)
		if err != nil {
			return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
				"daemon: pinnedHarnessLaunchSpecBuilder: resolve harness %q: %w", agentType, err)
		}
		emitModelSelected(ctx, bus, core.RunID(rc.RunID), effectiveModel(h, rc), agentType)
		if _, ok := h.(*claude.Harness); ok {
			return claude.BuildLaunchSpec(ctx, rc)
		}
		return buildCodexRoutedLaunchSpec(ctx, rc, h, agentType)
	}
}

func buildCodexRoutedLaunchSpec(
	ctx context.Context,
	rc shared.LaunchCtx,
	h handlercontract.Harness,
	agentType core.AgentType,
) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	taskBody := rc.NodePrompt
	if taskBody == "" {
		taskBody = rc.BeadDescription
	}
	if taskBody == "" {
		taskBody = rc.BeadID
	}
	taskTitle := rc.BeadTitle
	if taskTitle == "" {
		taskTitle = rc.BeadID
	}
	agentTaskPayload := workspace.AgentTaskPayload{
		BeadID:              rc.BeadID,
		Title:               taskTitle,
		Phase:               string(rc.Phase),
		Iteration:           rc.IterationCount,
		RunID:               core.RunID(rc.RunID).String(),
		WorkspacePath:       rc.WorkspacePath,
		Body:                taskBody,
		PriorVerdictFile:    rc.PriorVerdictFile,
		PriorVerdictSummary: rc.PriorVerdictSummary,
		ReviewBaseSHA:       rc.ReviewBaseSHA,
		ReviewHeadSHA:       rc.ReviewHeadSHA,
		ReAttach:            rc.AgentTaskReAttach,
		ExtraContext:        rc.ExtraContext,
		BaseBranch:          rc.BaseBranch,
		// The harness declares how its run signals completion; the task file
		// tells the agent to finish the way that harness actually finishes.
		// Hard-coding the claude `/quit` form here is what made a pi agent
		// that had already committed run `echo "/quit" | pbcopy`, outlive its
		// budget, and be killed as a crash
		// (hk-quit-instruction-not-portable-ms55w).
		Completion: h.Completion(),
	}
	if err := workspace.WriteAgentTaskVia(ctx, rc.Runner, rc.WorkspacePath, agentTaskPayload); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: WriteAgentTaskVia: %w", err)
	}

	hrc := handlercontract.RunCtx{
		RunID:               core.RunID(rc.RunID),
		BeadID:              rc.BeadID,
		WorkspacePath:       rc.WorkspacePath,
		DaemonSocket:        rc.DaemonSocket,
		WorkflowMode:        rc.WorkflowMode,
		Phase:               rc.Phase,
		IterationCount:      rc.IterationCount,
		HandlerBinary:       rc.HandlerBinary,
		DaemonBinaryPath:    rc.DaemonBinaryPath,
		BaseEnv:             rc.BaseEnv,
		BeadTitle:           rc.BeadTitle,
		BeadDescription:     rc.BeadDescription,
		NodePrompt:          rc.NodePrompt,
		PriorVerdictFile:    rc.PriorVerdictFile,
		PriorVerdictSummary: rc.PriorVerdictSummary,
		ReviewBaseSHA:       rc.ReviewBaseSHA,
		ReviewHeadSHA:       rc.ReviewHeadSHA,
		Model:               rc.Model,
		Effort:              rc.Effort,
		Provider:            rc.Provider,
		APIKeyEnv:           rc.APIKeyEnv,
		APIKeyFile:          rc.APIKeyFile,
		BaseURL:             rc.BaseURL,
		API:                 rc.API,
		WorktreeRootPath:    rc.WorktreeRootPath,
		ExtraContext:        rc.ExtraContext,
		BaseBranch:          rc.BaseBranch,
		PriorSessionID:      rc.PriorClaudeSessID,
	}
	spawnSpec, err := h.LaunchSpec(hrc)
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: harness.LaunchSpec: %w", err)
	}

	handlerSessUID, err := uuid.NewV7()
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: mint handlerSessionID: %w", err)
	}
	handlerSessionID := handlerSessUID.String()

	trackingUID, err := uuid.NewV7()
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: mint trackingSessionID: %w", err)
	}
	trackingSessionID := trackingUID.String()

	nodeID := "bead/" + rc.BeadID
	runIDStr := core.RunID(rc.RunID).String()
	sessionLogPath := workspace.SessionLogDirPath(rc.WorkspacePath, handlerSessionID)
	if agentType == core.AgentTypePi {
		sessionLogPath = filepath.Join(rc.WorkspacePath, ".harmonik", "pi-agent")
	}
	rawMsgs, err := handler.PreExecMessages(
		runIDStr,
		handlerSessionID,
		nodeID,
		trackingSessionID,
		string(agentType),
		sessionLogPath,
		nil,
	)
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: PreExecMessages: %w", err)
	}
	preExecMsgs := make([]json.RawMessage, len(rawMsgs))
	for i, b := range rawMsgs {
		preExecMsgs[i] = json.RawMessage(b)
	}

	spec := handler.LaunchSpec{
		Binary:       spawnSpec.Binary,
		Args:         spawnSpec.Args,
		Env:          spawnSpec.Env,
		WorkDir:      spawnSpec.WorkDir,
		Role:         string(rc.Phase),
		StdinDevNull: spawnSpec.StdinDevNull, // hk-j0p1r: forward /dev/null stdin so ProcessExit harnesses (pi, codex) get startup EOF
		// remote-substrate M4-C4 (T6): thread the per-run runner so a
		// worker-selected pi/codex run spawns the agent process ON THE WORKER via
		// the SSHRunner. rc.runner is the worker's SSHRunner for a REMOTE run and
		// nil for a LOCAL run (set at the dispatch seam from rbc.sshRunner). These
		// harnesses are SessionIDCaptured, so the caller forces spec.Substrate=nil
		// (exec path) — where handler.Launch consults spec.Runner. A nil runner
		// keeps the exec path byte-identical to box-A-local (NFR7). This composes
		// with the landed pi provider config ({Provider,BaseURL,API}, decision 6):
		// the runner only changes WHICH host the process runs on, never the wire
		// config carried in spawnSpec.Env/Args above.
		Runner: rc.Runner,
	}
	artifacts := shared.LaunchArtifacts{
		ClaudeSessionID:   trackingSessionID,
		HandlerSessionID:  handlerSessionID,
		PreExecMsgs:       preExecMsgs,
		ResolvedAgentType: agentType,
	}
	return spec, artifacts, nil
}
