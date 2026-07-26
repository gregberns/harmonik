package claude

// harness.go — Harness: handlercontract.Harness impl for Claude Code (C1/T2, hk-3kyh3).
//
// Harness wraps the existing BuildLaunchSpec path.  It satisfies the
// Harness interface without changing any dispatch behavior (no behavior change rule
// for C1).  T3 (hk-hj9ld) will route the registry + launchSpecBuilder lookup
// through this struct; T12 (hk-xhawy) will route the full cascade through it.
//
// Spec: specs/harness-contract.md §2; specs/handler-contract.md §4.10 HC-045a
// (claude-code agent type governed by claude-hook-bridge spec).
// See also: handlercontract/harness.go.

import (
	"context"
	"io"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
)

// Harness implements handlercontract.Harness for the Claude Code agent.
//
// The zero value is not valid; construct via NewHarness.
type Harness struct{}

// NewHarness returns a ready Harness.
func NewHarness() *Harness {
	return &Harness{}
}

// Compile-time assertion: *Harness satisfies handlercontract.Harness.
var _ handlercontract.Harness = (*Harness)(nil)

// AgentType returns core.AgentTypeClaudeCode — the registry key for this harness.
func (h *Harness) AgentType() core.AgentType {
	return core.AgentTypeClaudeCode
}

// LaunchSpec converts rc to a shared.LaunchCtx, calls BuildLaunchSpec, and
// returns the subprocess SpawnSpec (Binary/Args/Env/WorkDir).
//
// The caller receives a non-nil error on any CHB-001..CHB-024 failure; it MUST
// NOT call handler.Launch on error.
//
// Note: BuildLaunchSpec also returns shared.LaunchArtifacts (session IDs,
// preExecMsgs).  Those remain available only on the internal shared.LaunchCtx path
// until T3 threads a richer seam through the registry.
func (h *Harness) LaunchSpec(rc handlercontract.RunCtx) (handlercontract.SpawnSpec, error) {
	internal := shared.LaunchCtx{
		RunID:               rc.RunID,
		BeadID:              rc.BeadID,
		WorkspacePath:       rc.WorkspacePath,
		DaemonSocket:        rc.DaemonSocket,
		WorkflowMode:        rc.WorkflowMode,
		Phase:               rc.Phase,
		IterationCount:      rc.IterationCount,
		PriorClaudeSessID:   rc.PriorSessionID,
		HandlerBinary:       rc.HandlerBinary,
		DaemonBinaryPath:    rc.DaemonBinaryPath,
		BaseEnv:             rc.BaseEnv,
		BeadTitle:           rc.BeadTitle,
		BeadDescription:     rc.BeadDescription,
		NodePrompt:          rc.NodePrompt,
		AgentTaskReAttach:   rc.AgentTaskReAttach,
		PriorVerdictFile:    rc.PriorVerdictFile,
		PriorVerdictSummary: rc.PriorVerdictSummary,
		ReviewBaseSHA:       rc.ReviewBaseSHA,
		ReviewHeadSHA:       rc.ReviewHeadSHA,
		Model:               rc.Model,
		Effort:              rc.Effort,
		WorktreeRootPath:    rc.WorktreeRootPath,
		ExtraContext:        rc.ExtraContext,
		BaseBranch:          rc.BaseBranch,
	}

	spec, _, err := BuildLaunchSpec(context.Background(), internal)
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
func (h *Harness) Seed(_ handlercontract.Session, _ handlercontract.RunCtx) error {
	return nil
}

// Retask delivers reviewer feedback for iteration ≥2 to a re-spawned session.
//
// For the claude harness this pastes combined task+feedback via the tmux substrate.
// No-op for T2 (no behavior change); T12 will route the real call here.
func (h *Harness) Retask(_ handlercontract.Session, _ string, _ handlercontract.RunCtx) error {
	return nil
}

// Teardown ends the session so the shared loop's sess.Wait returns.
//
// For the claude harness this is /quit + 60 s grace + Kill, matching the
// pasteInjectQuitOnCommit sequence.  For T2 the quit+grace path is still driven
// by beadRunOne; Teardown calls Kill directly (same as forceTeardownSession) so
// a future T12 caller that only dispatches through the Harness still gets a
// safe session close.
func (h *Harness) Teardown(sess handlercontract.Session) error {
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
func (h *Harness) DetectReady(ev handlercontract.EventEnvelope) bool {
	return core.EventType(ev.Type) == core.EventTypeAgentReady
}

// SessionIDPolicy returns SessionIDMinted: the claude harness mints a fresh
// UUIDv7 (or reuses a prior one for implementer-resume) via MintClaudeSessionID
// inside LaunchSpec; the caller receives the ID as part of the returned SpawnSpec.Env
// (HARMONIK_CLAUDE_SESSION_ID) until T3 exposes a richer artifacts seam.
func (h *Harness) SessionIDPolicy() handlercontract.SessionIDPolicy {
	return handlercontract.SessionIDMinted
}

// Completion returns CompletionEventStreamThenQuit: the claude harness run
// signals completion via the event-stream /quit path.  The shared loop sends
// /quit + kill grace to the session and waits for sess.Wait.
func (h *Harness) Completion() handlercontract.CompletionMode {
	return handlercontract.CompletionEventStreamThenQuit
}

// NewSessionIDInterceptor returns inner unchanged for the claude harness.
//
// Harness is SessionIDMinted, not SessionIDCaptured, so the shared
// loop's implIsSessionIDCaptured gate prevents this method from ever being
// called in production. The no-op passthrough satisfies the interface contract
// so no concrete-type branching is needed in the shared loop.
func (h *Harness) NewSessionIDInterceptor(inner io.Reader, _ func(string), _ func()) io.Reader {
	return inner
}
