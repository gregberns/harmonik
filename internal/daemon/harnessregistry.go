package daemon

// harnessregistry.go — daemon-side HarnessRegistry wiring + registry-routed
// launchSpecBuilder (codex-harness C1/T3 hk-hj9ld; C5/T12 hk-xhawy).
//
// This file closes the first of the two declared harness seam points
// (harness.go §"the two declared seam points"): the launchSpecBuilder lookup is
// routed through resolveHarness (the four-tier precedence walk, harnessresolve.go)
// and HarnessRegistry.ForAgent (the per-agent-type route table).
//
// T12 wires the codex path: CodexHarness is now registered alongside ClaudeHarness,
// and routedLaunchSpecBuilder produces a handler.LaunchSpec + claudeRunArtifacts for
// the codex harness (previously it failed closed). The claude path retains its
// byte-identical delegation to buildClaudeLaunchSpec.
//
// Spec: specs/harness-contract.md §2 N5.
// See also: handlercontract/harnessregistry.go (the registry type),
// claudeharness.go, codexharness.go, harnessresolve.go.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workspace"
)

// newHarnessRegistry builds the daemon's HarnessRegistry with ClaudeHarness and
// CodexHarness registered.
//
// Returns a non-nil error only if Register fails (a duplicate or sealed-registry
// defect), which is impossible for these two distinct registrations but surfaced
// so callers fail-closed if this grows.
func newHarnessRegistry() (*handlercontract.HarnessRegistry, error) {
	reg := handlercontract.NewHarnessRegistry()
	if err := reg.Register(core.AgentTypeClaudeCode, NewClaudeHarness()); err != nil {
		return nil, fmt.Errorf("daemon: newHarnessRegistry: register claude harness: %w", err)
	}
	if err := reg.Register(core.AgentTypeCodex, NewCodexHarness("", "")); err != nil {
		return nil, fmt.Errorf("daemon: newHarnessRegistry: register codex harness: %w", err)
	}
	if err := reg.Register(core.AgentTypePi, NewPiHarness("", "", "", "")); err != nil {
		return nil, fmt.Errorf("daemon: newHarnessRegistry: register pi harness: %w", err)
	}
	return reg, nil
}

// routedLaunchSpecBuilder returns a launchSpecBuilder (the workLoopDeps hook
// shape: func(ctx, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error))
// that routes through resolveHarness + reg.ForAgent before building the spec.
//
// Precedence/selection: resolveHarness walks bead>queue>node>global and falls
// back to core.AgentTypeClaudeCode. The resolved agent_type is looked up in reg;
// an unregistered type returns a well-defined error (the routed builder fails the
// run rather than silently launching claude for an unknown type).
//
// Claude path: delegates to buildClaudeLaunchSpec directly so the returned
// LaunchSpec and claudeRunArtifacts are byte-identical to the pre-T3 call.
// Harness.LaunchSpec returns only a SpawnSpec, so routing the claude build
// through it would drop the artifacts the workloop/review-loop consume.
//
// Codex path (T12): writes agent-task.md, calls CodexHarness.LaunchSpec for the
// SpawnSpec, and assembles claudeRunArtifacts with a tracking session ID and
// pre-exec bus messages. The claudeSessionID field is a harmonic-internal tracking
// ID (not used for codex resume; resume uses the captured thread_id via
// RunCtx.PriorSessionID / claudeRunCtx.priorClaudeSessID).
//
// The bead argument carries the labels resolveHarness reads for the tier-1
// harness:<agent-type> override. Production passes the dispatch-time BeadRecord;
// callers with no bead (legacy/test) may pass a zero BeadRecord, which resolves to
// the claude default.
func routedLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
) func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return func(ctx context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
		agentType := resolveHarness(ctx, bead, queueDefault, nodeDefault, globalDefault, bus)

		h, err := reg.ForAgent(agentType)
		if err != nil {
			return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
				"daemon: routedLaunchSpecBuilder: resolve harness %q: %w", agentType, err)
		}

		// Claude path: delegate to buildClaudeLaunchSpec directly so the returned
		// LaunchSpec AND claudeRunArtifacts are byte-identical to the pre-T3 call.
		// buildClaudeLaunchSpec also sets artifacts.resolvedAgentType = claude-code.
		if _, ok := h.(*ClaudeHarness); ok {
			return buildClaudeLaunchSpec(ctx, rc)
		}

		// Codex path (T12): write agent-task.md, call harness.LaunchSpec for the
		// SpawnSpec, then build claudeRunArtifacts with a tracking session ID.
		return buildCodexRoutedLaunchSpec(ctx, rc, h, agentType)
	}
}

// pinnedHarnessLaunchSpecBuilder is like routedLaunchSpecBuilder but bypasses
// resolveHarness entirely: the caller has already determined agentType (e.g. via
// a DOT node-level harness/reviewer_harness pin) and it MUST NOT be overridden by
// a coarse bead label. Emits harness_selected at tier 3. (hk-2jxqg)
func pinnedHarnessLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	agentType core.AgentType,
	bus handlercontract.EventEmitter,
) func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return func(ctx context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
		emitHarnessSelected(ctx, bus, bead, agentType, 3)
		h, err := reg.ForAgent(agentType)
		if err != nil {
			return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
				"daemon: pinnedHarnessLaunchSpecBuilder: resolve harness %q: %w", agentType, err)
		}
		if _, ok := h.(*ClaudeHarness); ok {
			return buildClaudeLaunchSpec(ctx, rc)
		}
		return buildCodexRoutedLaunchSpec(ctx, rc, h, agentType)
	}
}

// buildCodexRoutedLaunchSpec assembles a handler.LaunchSpec + claudeRunArtifacts
// for non-claude harnesses (currently only CodexHarness).
//
// Steps:
//  1. Write agent-task.md (codex reads it via the seed-prompt argv).
//  2. Convert claudeRunCtx → handlercontract.RunCtx; call h.LaunchSpec.
//  3. Mint tracking session ID + handler session ID.
//  4. Render pre-exec bus messages (CHB-018 subset).
//  5. Return LaunchSpec + claudeRunArtifacts{resolvedAgentType: agentType}.
func buildCodexRoutedLaunchSpec(
	_ context.Context,
	rc claudeRunCtx,
	h handlercontract.Harness,
	agentType core.AgentType,
) (handler.LaunchSpec, claudeRunArtifacts, error) {
	// Step 1: write agent-task.md.
	taskBody := rc.nodePrompt
	if taskBody == "" {
		taskBody = rc.beadDescription
	}
	if taskBody == "" {
		taskBody = rc.beadID
	}
	taskTitle := rc.beadTitle
	if taskTitle == "" {
		taskTitle = rc.beadID
	}
	agentTaskPayload := workspace.AgentTaskPayload{
		BeadID:              rc.beadID,
		Title:               taskTitle,
		Phase:               string(rc.phase),
		Iteration:           rc.iterationCount,
		RunID:               core.RunID(rc.runID).String(),
		WorkspacePath:       rc.workspacePath,
		Body:                taskBody,
		PriorVerdictFile:    rc.priorVerdictFile,
		PriorVerdictSummary: rc.priorVerdictSummary,
		ReviewBaseSHA:       rc.reviewBaseSHA,
		ReviewHeadSHA:       rc.reviewHeadSHA,
		ReAttach:            rc.agentTaskReAttach,
		ExtraContext:        rc.extraContext,
		BaseBranch:          rc.baseBranch,
	}
	if err := workspace.WriteAgentTask(rc.workspacePath, agentTaskPayload); err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: WriteAgentTask: %w", err)
	}

	// Step 2: convert to RunCtx and call harness.LaunchSpec.
	hrc := handlercontract.RunCtx{
		RunID:               core.RunID(rc.runID),
		BeadID:              rc.beadID,
		WorkspacePath:       rc.workspacePath,
		DaemonSocket:        rc.daemonSocket,
		WorkflowMode:        rc.workflowMode,
		Phase:               rc.phase,
		IterationCount:      rc.iterationCount,
		HandlerBinary:       rc.handlerBinary,
		DaemonBinaryPath:    rc.daemonBinaryPath,
		BaseEnv:             rc.baseEnv,
		BeadTitle:           rc.beadTitle,
		BeadDescription:     rc.beadDescription,
		NodePrompt:          rc.nodePrompt,
		PriorVerdictFile:    rc.priorVerdictFile,
		PriorVerdictSummary: rc.priorVerdictSummary,
		ReviewBaseSHA:       rc.reviewBaseSHA,
		ReviewHeadSHA:       rc.reviewHeadSHA,
		Model:               rc.model,
		Effort:              rc.effort,
		WorktreeRootPath:    rc.worktreeRootPath,
		ExtraContext:        rc.extraContext,
		BaseBranch:          rc.baseBranch,
		PriorSessionID:      rc.priorClaudeSessID,
	}
	spawnSpec, err := h.LaunchSpec(hrc)
	if err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: harness.LaunchSpec: %w", err)
	}

	// Step 3: mint tracking session ID and handler session ID.
	handlerSessUID, err := uuid.NewV7()
	if err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: mint handlerSessionID: %w", err)
	}
	handlerSessionID := handlerSessUID.String()

	trackingUID, err := uuid.NewV7()
	if err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: mint trackingSessionID: %w", err)
	}
	trackingSessionID := trackingUID.String()

	// Step 4: render pre-exec bus messages (CHB-018 subset).
	nodeID := "bead/" + rc.beadID
	runIDStr := core.RunID(rc.runID).String()
	rawMsgs, err := handler.PreExecMessages(
		runIDStr,
		handlerSessionID,
		nodeID,
		trackingSessionID,
		"", // no session log path for codex
		nil,
	)
	if err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildCodexRoutedLaunchSpec: PreExecMessages: %w", err)
	}
	preExecMsgs := make([]json.RawMessage, len(rawMsgs))
	for i, b := range rawMsgs {
		preExecMsgs[i] = json.RawMessage(b)
	}

	// Step 5: assemble handler.LaunchSpec and claudeRunArtifacts.
	spec := handler.LaunchSpec{
		Binary:  spawnSpec.Binary,
		Args:    spawnSpec.Args,
		Env:     spawnSpec.Env,
		WorkDir: spawnSpec.WorkDir,
		Role:    string(rc.phase),
	}
	artifacts := claudeRunArtifacts{
		claudeSessionID:   trackingSessionID,
		handlerSessionID:  handlerSessionID,
		preExecMsgs:       preExecMsgs,
		resolvedAgentType: agentType,
	}
	return spec, artifacts, nil
}
