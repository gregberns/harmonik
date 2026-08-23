package codex

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// Harness implements handlercontract.Harness for the OpenAI codex agent.
//
// The zero value is not valid; construct via NewHarness. The struct carries
// the codex binary path and CODEX_HOME so LaunchSpec can build a RunCtx;
// both default sensibly when empty (BuildLaunchSpec normalises "" → "codex"
// and "" → "$HOME/.codex").
type Harness struct {
	// codexBinary is the codex executable path. Empty is normalised to "codex"
	// by BuildLaunchSpec.
	codexBinary string

	// codexHome is the CODEX_HOME path. Empty is normalised to "$HOME/.codex".
	codexHome string
}

// NewHarness returns a ready Harness.
//
// codexBinary and codexHome may be empty; BuildLaunchSpec normalises both.
func NewHarness(codexBinary, codexHome string) *Harness {
	return &Harness{
		codexBinary: codexBinary,
		codexHome:   codexHome,
	}
}

var _ handlercontract.Harness = (*Harness)(nil)

// AgentType returns core.AgentTypeCodex — the registry key for this harness.
func (h *Harness) AgentType() core.AgentType {
	return core.AgentTypeCodex
}

// LaunchSpec converts rc to a RunCtx, calls BuildLaunchSpec, and
// returns the subprocess SpawnSpec (Binary/Args/Env/WorkDir).
//
// The resume argv is selected when rc.PriorSessionID is non-nil: PriorSessionID
// carries the captured thread_id from the prior turn (RunCtx doc-comment), which
// BuildLaunchSpec uses to emit `codex exec resume <thread_id>`. For the
// initial turn PriorSessionID is nil and the initial argv is built.
//
// Returns a non-nil error on any BuildLaunchSpec failure; the caller MUST
// NOT call handler.Launch on error.
func (h *Harness) LaunchSpec(rc handlercontract.RunCtx) (handlercontract.SpawnSpec, error) {
	projectRoot, err := os.Getwd()
	if err != nil {
		return handlercontract.SpawnSpec{}, fmt.Errorf("resolve project working directory: %w", err)
	}
	if err := cleanCodexStaleWAL(context.Background(), projectRoot, h.codexHome); err != nil {
		return handlercontract.SpawnSpec{}, err
	}

	internal := RunCtx{
		CodexBinary:    h.codexBinary,
		WorkspacePath:  rc.WorkspacePath,
		BeadID:         rc.BeadID,
		Model:          rc.Model,
		PriorThreadID:  rc.PriorSessionID,
		IterationCount: rc.IterationCount,
		BaseEnv:        rc.BaseEnv,
		CodexHome:      h.codexHome,
	}

	spec, err := BuildLaunchSpec(internal)
	if err != nil {
		return handlercontract.SpawnSpec{}, err
	}

	return handlercontract.SpawnSpec{
		Binary:       spec.Binary,
		Args:         spec.Args,
		Env:          spec.Env,
		WorkDir:      spec.WorkDir,
		StdinDevNull: spec.StdinDevNull, // hk-rpr6 / hk-j0p1r: thread ProcessExit /dev/null stdin through the routed path
	}, nil
}

// Seed delivers the first-turn task to a freshly-spawned session.
//
// For codex this is a no-op: the task is delivered via the seed-prompt argv built
// by BuildLaunchSpec (codex has no TUI/paste path). There is nothing to
// paste into a `codex exec` process.
func (h *Harness) Seed(_ handlercontract.Session, _ handlercontract.RunCtx) error {
	return nil
}

// Retask delivers review feedback for iteration ≥2 to a re-spawned session.
//
// For codex this is a no-op: there is no live REPL to drive. The real re-task
// mechanism is the resume argv — the next turn's RunCtx carries the captured
// thread_id as PriorSessionID, and LaunchSpec emits
// `codex exec resume <thread_id>` with the feedback folded into the seed prompt
// by the cascade (T12). Retask itself has no session to write to.
func (h *Harness) Retask(_ handlercontract.Session, _ string, _ handlercontract.RunCtx) error {
	return nil
}

// Teardown ends the session so the shared loop's sess.Wait returns.
//
// codex self-terminates on turn completion (CompletionProcessExit), so under
// normal operation there is nothing to tear down — sess.Wait has already
// returned. Teardown is best-effort: if a session handle is still live (e.g. the
// shared loop calls Teardown defensively after a timeout), Kill closes it. A nil
// session is a no-op.
func (h *Harness) Teardown(sess handlercontract.Session) error {
	if sess == nil {
		return nil
	}
	return sess.Kill(context.Background())
}

// DetectReady reports whether ev is the agent_ready signal for this harness.
//
// codex has no distinct readiness handshake the way claude does — `codex exec`
// begins working immediately and the thread.started event is captured by the
// JSONL parser, not surfaced as a harmonik agent_ready event. For the harness
// contract DetectReady operates on the harmonik EventEnvelope stream, so it
// matches the same agent_ready type the shared watcher emits. It explicitly
// never returns true for launch_initiated (HC-041 hard rule): the positive type
// check below can only match agent_ready, never launch_initiated.
func (h *Harness) DetectReady(ev handlercontract.EventEnvelope) bool {
	return ev.Type == core.EventTypeAgentReady
}

// SessionIDPolicy returns SessionIDCaptured: codex does not accept a
// caller-minted session id. The thread_id is captured from the first
// thread.started event in the JSONL stream (codexjsonlparser.go) and recorded in
// codexRunArtifacts for the next `codex exec resume` launch.
func (h *Harness) SessionIDPolicy() handlercontract.SessionIDPolicy {
	return handlercontract.SessionIDCaptured
}

// Completion returns CompletionProcessExit: `codex exec --json` self-terminates
// on turn completion. The shared loop bypasses pasteInjectQuitOnCommit and
// relies on sess.Wait + the absolute commitHardCeiling (specs/harness-contract.md
// §2 N2).
func (h *Harness) Completion() handlercontract.CompletionMode {
	return handlercontract.CompletionProcessExit
}

// NewSessionIDInterceptor returns a codexThreadIDInterceptor wrapping inner.
//
// The interceptor fires sessionIDCb exactly once with the captured thread_id from
// the first thread.started event in the codex JSONL stream, and passes all bytes
// through unchanged. agentEndCb is ignored — codex has no agent_end event;
// CompletionProcessExit for codex relies on self-exit, not an event-driven kill.
// Called by the shared loop's implIsSessionIDCaptured block without branching.
func (h *Harness) NewSessionIDInterceptor(inner io.Reader, sessionIDCb func(string), _ func()) io.Reader {
	return newCodexThreadIDInterceptor(inner, sessionIDCb)
}
