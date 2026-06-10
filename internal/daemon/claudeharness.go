// claudeharness.go — ClaudeHarness: handlercontract.Harness impl for Claude Code (C1/T2, hk-3kyh3).
//
// ClaudeHarness wraps the existing buildClaudeLaunchSpec path.  It satisfies the
// Harness interface without changing any dispatch behavior (no behavior change rule
// for C1).  T3 (hk-hj9ld) will route the registry + launchSpecBuilder lookup
// through this struct; T12 (hk-xhawy) will route the full cascade through it.
//
// Spec: specs/harness-contract.md §2; specs/handler-contract.md §4.10 HC-045a
// (claude-code agent type governed by claude-hook-bridge spec).
// See also: handlercontract/harness.go.
package daemon

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ClaudeHarness implements handlercontract.Harness for the Claude Code agent.
//
// The zero value is not valid; construct via NewClaudeHarness.
type ClaudeHarness struct{}

// NewClaudeHarness returns a ready ClaudeHarness.
func NewClaudeHarness() *ClaudeHarness {
	return &ClaudeHarness{}
}

// Compile-time assertion: *ClaudeHarness satisfies handlercontract.Harness.
var _ handlercontract.Harness = (*ClaudeHarness)(nil)

// AgentType returns core.AgentTypeClaudeCode — the registry key for this harness.
func (h *ClaudeHarness) AgentType() core.AgentType {
	return core.AgentTypeClaudeCode
}

// LaunchSpec converts rc to a claudeRunCtx, calls buildClaudeLaunchSpec, and
// returns the subprocess SpawnSpec (Binary/Args/Env/WorkDir).
//
// The caller receives a non-nil error on any CHB-001..CHB-024 failure; it MUST
// NOT call handler.Launch on error.
//
// Note: buildClaudeLaunchSpec also returns claudeRunArtifacts (session IDs,
// preExecMsgs).  Those remain available only on the internal claudeRunCtx path
// until T3 threads a richer seam through the registry.
func (h *ClaudeHarness) LaunchSpec(rc handlercontract.RunCtx) (handlercontract.SpawnSpec, error) {
	internal := claudeRunCtx{
		runID:               rc.RunID,
		beadID:              rc.BeadID,
		workspacePath:       rc.WorkspacePath,
		daemonSocket:        rc.DaemonSocket,
		workflowMode:        rc.WorkflowMode,
		phase:               rc.Phase,
		iterationCount:      rc.IterationCount,
		priorClaudeSessID:   rc.PriorSessionID,
		handlerBinary:       rc.HandlerBinary,
		daemonBinaryPath:    rc.DaemonBinaryPath,
		baseEnv:             rc.BaseEnv,
		beadTitle:           rc.BeadTitle,
		beadDescription:     rc.BeadDescription,
		nodePrompt:          rc.NodePrompt,
		agentTaskReAttach:   rc.AgentTaskReAttach,
		priorVerdictFile:    rc.PriorVerdictFile,
		priorVerdictSummary: rc.PriorVerdictSummary,
		reviewBaseSHA:       rc.ReviewBaseSHA,
		reviewHeadSHA:       rc.ReviewHeadSHA,
		model:               rc.Model,
		effort:              rc.Effort,
		worktreeRootPath:    rc.WorktreeRootPath,
		extraContext:        rc.ExtraContext,
		baseBranch:          rc.BaseBranch,
	}

	spec, _, err := buildClaudeLaunchSpec(context.Background(), internal)
	if err != nil {
		return handlercontract.SpawnSpec{}, err
	}

	return handlercontract.SpawnSpec{
		Binary:  spec.Binary,
		Args:    spec.Args,
		Env:     spec.Env,
		WorkDir: spec.WorkDir,
	}, nil
}

// Seed delivers the first-turn task to a freshly-spawned session.
//
// For the claude harness this is splash-dismiss + bracketed-paste of
// agent-task.md, driven via the tmux substrate's pasteInjecter interface.
// The actual paste path is still threaded through beadRunOne's pasteInjectOnLaunch
// call until T12 (hk-xhawy) routes the full cascade through the harness.
// This method is a no-op for T2 (no behavior change).
func (h *ClaudeHarness) Seed(_ handlercontract.Session, _ handlercontract.RunCtx) error {
	return nil
}

// Retask delivers reviewer feedback for iteration ≥2 to a re-spawned session.
//
// For the claude harness this pastes combined task+feedback via the tmux substrate.
// No-op for T2 (no behavior change); T12 will route the real call here.
func (h *ClaudeHarness) Retask(_ handlercontract.Session, _ string, _ handlercontract.RunCtx) error {
	return nil
}

// Teardown ends the session so the shared loop's sess.Wait returns.
//
// For the claude harness this is /quit + 60 s grace + Kill, matching the
// pasteInjectQuitOnCommit sequence.  For T2 the quit+grace path is still driven
// by beadRunOne; Teardown calls Kill directly (same as forceTeardownSession) so
// a future T12 caller that only dispatches through the Harness still gets a
// safe session close.
func (h *ClaudeHarness) Teardown(sess handlercontract.Session) error {
	if sess == nil {
		return nil
	}
	return sess.Kill(context.Background())
}

// DetectReady reports whether ev is the agent_ready signal for this harness.
//
// Returns true iff ev.Type == "agent_ready".  Explicitly never returns true for
// "launch_initiated" per HC-041 (the type check below is a positive-match; a
// future event whose type string happens to be equal to agent_ready would still
// satisfy the contract, while launch_initiated never will).
func (h *ClaudeHarness) DetectReady(ev handlercontract.EventEnvelope) bool {
	return core.EventType(ev.Type) == core.EventTypeAgentReady
}

// SessionIDPolicy returns SessionIDMinted: the claude harness mints a fresh
// UUIDv7 (or reuses a prior one for implementer-resume) via MintClaudeSessionID
// inside LaunchSpec; the caller receives the ID as part of the returned SpawnSpec.Env
// (HARMONIK_CLAUDE_SESSION_ID) until T3 exposes a richer artifacts seam.
func (h *ClaudeHarness) SessionIDPolicy() handlercontract.SessionIDPolicy {
	return handlercontract.SessionIDMinted
}

// Completion returns CompletionEventStreamThenQuit: the claude harness run
// signals completion via the event-stream /quit path.  The shared loop sends
// /quit + kill grace to the session and waits for sess.Wait.
func (h *ClaudeHarness) Completion() handlercontract.CompletionMode {
	return handlercontract.CompletionEventStreamThenQuit
}
