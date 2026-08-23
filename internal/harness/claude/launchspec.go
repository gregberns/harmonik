package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/workspace"
)

// BuildLaunchSpec threads together all bridge pieces required to launch
// a Claude Code (or twin) subprocess for any workflow phase.
//
// The sequence follows the design in
// .kerf/projects/gregberns-harmonik/bridge-integration/04-research/component-3-4/design.md §1:
//
//  1. MintClaudeSessionID — mint fresh or reuse (CHB-008/009).
//  2. DeriveClaudeTranscriptPath — session log location for CHB-018 step 2.
//  3. MaterializeClaudeSettings — atomic hook-bridge settings file (CHB-001..005).
//     3a. EnsureWorktreeTrust — pre-seed ~/.claude.json trust entry (CHB-029/WM-040b).
//     3b. WriteAgentTask — atomic agent-task.md write (CHB-028).
//  4. CheckSettingsLocalJSON — fail-fast on settings.local.json shadow (CHB-024).
//  5. Build ClaudeEnvConfig and call ClaudeEnvVars — CHB-006 env.
//  6. Build argv — --session-id or --resume per CHB-008 (OQ3 allow-list).
//  7. CheckForbiddenFlags — deny-list guard (CHB-007).
//  8. PreExecMessages — render 4 ordered progress messages (CHB-018).
//  9. Return handler.LaunchSpec + shared.LaunchArtifacts.
//
// Returns a non-nil error (wrapping handler.ErrStructural where applicable)
// if any step fails. The caller MUST NOT call handler.Launch on error.
//
// Spec refs: claude-hook-bridge.md §4.2..4.3, §4.7, §4.9;
// handler-contract.md HC-005, HC-055.
func BuildLaunchSpec(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	_ = ctx // reserved for future async steps (e.g. skill provisioning)

	mintRes, err := handler.MintClaudeSessionID(string(rc.Phase), rc.PriorClaudeSessID)
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: MintClaudeSessionID: %w", err)
	}

	sessionLogPath, err := handler.DeriveClaudeTranscriptPath(rc.WorkspacePath, mintRes.ClaudeSessionID)
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: DeriveClaudeTranscriptPath: %w", err)
	}

	settingsHookBinary := rc.DaemonBinaryPath
	if rc.Runner != nil && rc.WorkerBinaryPath != "" {
		settingsHookBinary = rc.WorkerBinaryPath
	}
	if err := workspace.MaterializeClaudeSettingsVia(ctx, rc.Runner, rc.WorkspacePath, settingsHookBinary, sessionLogPath); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: MaterializeClaudeSettings: %w", err)
	}

	if err := workspace.EnsureWorktreeTrustVia(ctx, rc.Runner, rc.WorkspacePath); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: EnsureWorktreeTrust: %w", err)
	}

	var isolatedClaudeConfigDir string
	if rc.Runner != nil {
		isolatedClaudeConfigDir, err = workspace.PrepareIsolatedClaudeConfigDirVia(ctx, rc.Runner, rc.WorkspacePath)
		if err != nil {
			return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
				"daemon: buildClaudeLaunchSpec: PrepareIsolatedClaudeConfigDirVia: %w", err)
		}
	}

	taskBody := rc.BeadDescription
	if rc.NodePrompt != "" && rc.Phase != handlercontract.ReviewLoopPhaseReviewer {
		taskBody = rc.NodePrompt
	}
	if strings.TrimSpace(taskBody) == "" {
		taskBody = rc.BeadTitle
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
		RunID:               rc.RunID.String(),
		WorkspacePath:       rc.WorkspacePath,
		Body:                taskBody,
		PriorVerdictFile:    rc.PriorVerdictFile,
		PriorVerdictSummary: rc.PriorVerdictSummary,
		ReviewBaseSHA:       rc.ReviewBaseSHA,
		ReviewHeadSHA:       rc.ReviewHeadSHA,
		ReAttach:            rc.AgentTaskReAttach,
		ExtraContext:        rc.ExtraContext,
		BaseBranch:          rc.BaseBranch,
		// Claude is a REPL that outlives the work, so its task file keeps the
		// `/quit` instruction that fires the Stop hook (CHB-028, hk-cmybm).
		// Stated rather than left to the zero value, so the two launch paths
		// read the same way (hk-quit-instruction-not-portable-ms55w).
		Completion: handlercontract.CompletionEventStreamThenQuit,
	}
	if err := workspace.WriteAgentTaskVia(ctx, rc.Runner, rc.WorkspacePath, agentTaskPayload); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: WriteAgentTask: %w", err)
	}

	if err := handler.CheckSettingsLocalJSON(rc.WorkspacePath); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: CheckSettingsLocalJSON: %w", err)
	}

	handlerSessUID, err := uuid.NewV7()
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: mint handlerSessionID UUIDv7: %w", err)
	}
	handlerSessionID := handlerSessUID.String()

	// WorkflowID and NodeID: the bead is the workflow unit, so we
	// synthesise "bead/<beadID>" as the node identifier. WorkflowID reuses the
	// runID's UUID (run is the workflow scope).
	//
	// TODO(hk-gql20.x): replace with typed WorkflowID / NodeID from a workflow
	// registry once multi-node workflows are introduced.
	nodeID := "bead/" + rc.BeadID
	workflowID, err := core.NewWorkflowID(rc.RunID.String())
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: validate workflow ID: %w", err)
	}

	workflowModeStr := string(rc.WorkflowMode)
	phaseStr := string(rc.Phase)
	iterCountStr := ""
	if rc.IterationCount > 0 {
		iterCountStr = strconv.Itoa(rc.IterationCount)
	}

	cfg := handler.ClaudeEnvConfig{
		RunID:            rc.RunID.String(),
		DaemonSocket:     rc.DaemonSocket,
		WorkspacePath:    rc.WorkspacePath,
		HandlerSessionID: handlerSessionID,
		ClaudeSessionID:  mintRes.ClaudeSessionID,
		WorkflowID:       workflowID.String(),
		NodeID:           nodeID,
		WorkflowMode:     workflowModeStr,
		Phase:            phaseStr,
		IterationCount:   iterCountStr,
		BeadID:           rc.BeadID,
		// HarmonikAgent distinguishes this implementer on the keeper bus so the
		// statusLine helper writes impl-<runID>.ctx rather than captain.ctx (hk-4hk).
		HarmonikAgent: "impl-" + rc.RunID.String(),
		BaseEnv:       rc.BaseEnv,
	}
	env := handler.ClaudeEnvVars(cfg)

	if isolatedClaudeConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+isolatedClaudeConfigDir)
	}

	if rc.Model != "" {
		if err := shared.ValidateModel(rc.Model); err != nil {
			return handler.LaunchSpec{}, shared.LaunchArtifacts{}, err
		}
	}
	if rc.Effort != "" {
		if err := shared.ValidateEffort(rc.Effort); err != nil {
			return handler.LaunchSpec{}, shared.LaunchArtifacts{}, err
		}
	}

	var args []string
	if mintRes.ResumeMode {
		args = []string{"--resume", mintRes.ClaudeSessionID}
	} else {
		args = []string{"--session-id", mintRes.ClaudeSessionID}
	}
	if rc.Model != "" {
		args = append(args, "--model", rc.Model)
	}
	if rc.Effort != "" {
		args = append(args, "--effort", rc.Effort)
	}
	if isHarmonikManagedWorktree(rc.WorkspacePath, rc.WorktreeRootPath) {
		args = append(args, "--dangerously-skip-permissions")
	}

	if err := handler.CheckForbiddenFlags(args, env); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: CheckForbiddenFlags: %w", err)
	}

	runIDStr := rc.RunID.String()
	rawMsgs, err := handler.PreExecMessages(
		runIDStr,
		handlerSessionID,
		nodeID,
		mintRes.ClaudeSessionID,
		string(core.AgentTypeClaudeCode),
		sessionLogPath,
		nil, // skills = nil per design §1 step 9
	)
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: PreExecMessages: %w", err)
	}
	preExecMsgs := make([]json.RawMessage, len(rawMsgs))
	for i, b := range rawMsgs {
		preExecMsgs[i] = json.RawMessage(b)
	}

	spec := handler.LaunchSpec{
		Binary:  rc.HandlerBinary,
		Args:    args,
		Env:     env,
		WorkDir: rc.WorkspacePath,
		Role:    string(rc.Phase), // "implementer-initial", "implementer-resume", "reviewer", or "" (single)
	}

	artifacts := shared.LaunchArtifacts{
		ClaudeSessionID:   mintRes.ClaudeSessionID,
		SessionLogPath:    sessionLogPath,
		HandlerSessionID:  handlerSessionID,
		PreExecMsgs:       preExecMsgs,
		Substrate:         nil,
		ResolvedAgentType: core.AgentTypeClaudeCode,
	}

	return spec, artifacts, nil
}

func isHarmonikManagedWorktree(workspacePath, worktreeRootPath string) bool {
	if workspacePath == "" {
		return false
	}
	canonWS, err := filepath.EvalSymlinks(workspacePath)
	if err != nil {
		canonWS = workspacePath
	}
	if worktreeRootPath != "" {
		if canonRoot, rerr := filepath.EvalSymlinks(worktreeRootPath); rerr == nil {
			prefix := canonRoot + string(filepath.Separator)
			if strings.HasPrefix(canonWS, prefix) {
				return true
			}
		}
	}
	sep := string(filepath.Separator)
	for _, seg := range []string{
		sep + ".harmonik" + sep + "worktrees" + sep, // implementer worktrees (DefaultWorktreeRoot)
		// crew-launch worktree path — no Go code creates .harmonik/crew-worktrees/
		// today (DefaultWorktreeRoot is .harmonik/worktrees), but the crew launch
		// flow uses this dir; keep it as forward-compat/defensive coverage so a
		// crew pane also gets --dangerously-skip-permissions.
		sep + ".harmonik" + sep + "crew-worktrees" + sep,
	} {
		if strings.Contains(canonWS, seg) {
			return true
		}
	}
	return false
}
