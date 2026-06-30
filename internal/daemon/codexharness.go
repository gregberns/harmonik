package daemon

// codexharness.go — CodexHarness: handlercontract.Harness impl for OpenAI codex
// (codex-harness C2/T8, hk-m57va).
//
// codex's shape differs from claude's in the two ways the Harness interface was
// designed to abstract (specs/harness-contract.md §2 N2/N3):
//
//   - Completion = CompletionProcessExit. `codex exec --json` is a one-shot
//     run-to-exit invocation: it streams JSONL and self-terminates on turn
//     completion. There is no TUI, no bracketed-paste seed, and no `/quit`. The
//     shared loop therefore bypasses pasteInjectQuitOnCommit and relies on
//     sess.Wait + the absolute commitHardCeiling.
//
//   - SessionIDPolicy = SessionIDCaptured. codex does not accept a caller-minted
//     session id; the thread identifier is CAPTURED from the first thread.started
//     event in the JSONL stream (codexjsonlparser.go) and recorded in
//     codexRunArtifacts. The next turn is launched with
//     `codex exec resume <thread_id>` (buildCodexLaunchSpec resume path, T7).
//
// Because codex delivers its task via argv (the seed prompt in
// buildCodexLaunchSpec) and self-terminates, Seed/Retask/Teardown are no-ops or
// best-effort kills — there is no interactive session to paste into. The real
// re-task mechanism is the resume argv, built by buildCodexLaunchSpec when the
// next turn's RunCtx carries PriorSessionID (the captured thread_id); Retask
// itself has no live REPL to drive.
//
// NOT REGISTERED in newHarnessRegistry by this bead: T3's routedLaunchSpecBuilder
// fails closed for any resolved-but-non-claude harness ("not wired into the
// claudeRunArtifacts launch path"). Registering CodexHarness here without the T12
// cascade wiring would turn that fail-closed into a hard run failure the moment a
// bead resolves to codex. The cascade + registration is T12 (hk-xhawy). This file
// builds the adapter as a standalone, fully-tested unit.
//
// Spec: specs/harness-contract.md §2. See also: claudeharness.go (the structural
// template), codexlaunchspec.go (buildCodexLaunchSpec), codexjsonlparser.go.

import (
	"context"
	"io"
	"os"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// CodexHarness implements handlercontract.Harness for the OpenAI codex agent.
//
// The zero value is not valid; construct via NewCodexHarness. The struct carries
// the codex binary path and CODEX_HOME so LaunchSpec can build a codexRunCtx;
// both default sensibly when empty (buildCodexLaunchSpec normalises "" → "codex"
// and "" → "$HOME/.codex").
type CodexHarness struct {
	// codexBinary is the codex executable path. Empty is normalised to "codex"
	// by buildCodexLaunchSpec.
	codexBinary string

	// codexHome is the CODEX_HOME path. Empty is normalised to "$HOME/.codex".
	codexHome string
}

// NewCodexHarness returns a ready CodexHarness.
//
// codexBinary and codexHome may be empty; buildCodexLaunchSpec normalises both.
func NewCodexHarness(codexBinary, codexHome string) *CodexHarness {
	return &CodexHarness{
		codexBinary: codexBinary,
		codexHome:   codexHome,
	}
}

// Compile-time assertion: *CodexHarness satisfies handlercontract.Harness.
var _ handlercontract.Harness = (*CodexHarness)(nil)

// AgentType returns core.AgentTypeCodex — the registry key for this harness.
func (h *CodexHarness) AgentType() core.AgentType {
	return core.AgentTypeCodex
}

// LaunchSpec converts rc to a codexRunCtx, calls buildCodexLaunchSpec, and
// returns the subprocess SpawnSpec (Binary/Args/Env/WorkDir).
//
// The resume argv is selected when rc.PriorSessionID is non-nil: PriorSessionID
// carries the captured thread_id from the prior turn (RunCtx doc-comment), which
// buildCodexLaunchSpec uses to emit `codex exec resume <thread_id>`. For the
// initial turn PriorSessionID is nil and the initial argv is built.
//
// Returns a non-nil error on any buildCodexLaunchSpec failure; the caller MUST
// NOT call handler.Launch on error.
func (h *CodexHarness) LaunchSpec(rc handlercontract.RunCtx) (handlercontract.SpawnSpec, error) {
	// Per-launch stale-WAL guard (hk-2pb79): clean a stale, unheld codex
	// state_*.sqlite-wal left by a killed codex run before this launch, so the
	// new codex session is not corrupted into a <10s "exited without advancing
	// HEAD" fast-fail. No-op when there is no .harmonik/config.yaml; fails loud
	// only when config.yaml exists but omits the required codex.stale_wal_max_bytes
	// key. projectRoot is the daemon CWD (== ProjectDir).
	projectRoot, _ := os.Getwd()
	if err := cleanCodexStaleWAL(projectRoot, h.codexHome); err != nil {
		return handlercontract.SpawnSpec{}, err
	}

	internal := codexRunCtx{
		codexBinary:   h.codexBinary,
		workspacePath: rc.WorkspacePath,
		beadID:        rc.BeadID,
		priorThreadID: rc.PriorSessionID,
		baseEnv:       rc.BaseEnv,
		codexHome:     h.codexHome,
	}

	spec, err := buildCodexLaunchSpec(internal)
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
// For codex this is a no-op: the task is delivered via the seed-prompt argv built
// by buildCodexLaunchSpec (codex has no TUI/paste path). There is nothing to
// paste into a `codex exec` process.
func (h *CodexHarness) Seed(_ handlercontract.Session, _ handlercontract.RunCtx) error {
	return nil
}

// Retask delivers review feedback for iteration ≥2 to a re-spawned session.
//
// For codex this is a no-op: there is no live REPL to drive. The real re-task
// mechanism is the resume argv — the next turn's RunCtx carries the captured
// thread_id as PriorSessionID, and LaunchSpec emits
// `codex exec resume <thread_id>` with the feedback folded into the seed prompt
// by the cascade (T12). Retask itself has no session to write to.
func (h *CodexHarness) Retask(_ handlercontract.Session, _ string, _ handlercontract.RunCtx) error {
	return nil
}

// Teardown ends the session so the shared loop's sess.Wait returns.
//
// codex self-terminates on turn completion (CompletionProcessExit), so under
// normal operation there is nothing to tear down — sess.Wait has already
// returned. Teardown is best-effort: if a session handle is still live (e.g. the
// shared loop calls Teardown defensively after a timeout), Kill closes it. A nil
// session is a no-op.
func (h *CodexHarness) Teardown(sess handlercontract.Session) error {
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
func (h *CodexHarness) DetectReady(ev handlercontract.EventEnvelope) bool {
	return core.EventType(ev.Type) == core.EventTypeAgentReady
}

// SessionIDPolicy returns SessionIDCaptured: codex does not accept a
// caller-minted session id. The thread_id is captured from the first
// thread.started event in the JSONL stream (codexjsonlparser.go) and recorded in
// codexRunArtifacts for the next `codex exec resume` launch.
func (h *CodexHarness) SessionIDPolicy() handlercontract.SessionIDPolicy {
	return handlercontract.SessionIDCaptured
}

// Completion returns CompletionProcessExit: `codex exec --json` self-terminates
// on turn completion. The shared loop bypasses pasteInjectQuitOnCommit and
// relies on sess.Wait + the absolute commitHardCeiling (specs/harness-contract.md
// §2 N2).
func (h *CodexHarness) Completion() handlercontract.CompletionMode {
	return handlercontract.CompletionProcessExit
}

// NewSessionIDInterceptor returns a codexThreadIDInterceptor wrapping inner.
//
// The interceptor fires sessionIDCb exactly once with the captured thread_id from
// the first thread.started event in the codex JSONL stream, and passes all bytes
// through unchanged. agentEndCb is ignored — codex has no agent_end event;
// CompletionProcessExit for codex relies on self-exit, not an event-driven kill.
// Called by the shared loop's implIsSessionIDCaptured block without branching.
func (h *CodexHarness) NewSessionIDInterceptor(inner io.Reader, sessionIDCb func(string), _ func()) io.Reader {
	return newCodexThreadIDInterceptor(inner, sessionIDCb)
}
