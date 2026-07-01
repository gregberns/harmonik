package daemon

// piharness.go — PiHarness: handlercontract.Harness impl for Pi (codename:pilot, PI-010/011/012/013/014).
//
// Pi's shape is a ProcessExit + SessionIDCaptured harness, mirroring codex
// (specs/harness-contract.md §2 N2/N3):
//
//   - Completion = CompletionProcessExit. `pi --mode json` is a one-shot
//     run-to-exit invocation: it streams NDJSON and nominally self-terminates
//     on turn completion. The shared loop bypasses pasteInjectQuitOnCommit and
//     relies on sess.Wait + the absolute commitHardCeiling. PI-014 adds the
//     agent_end watcher as an event-driven Kill because Pi's process exit is
//     unreliable (#4303/#161/#4942); the 90m ceiling is backstop only.
//
//   - SessionIDPolicy = SessionIDCaptured. Pi emits, as the FIRST NDJSON line,
//     `{"type":"session","version":3,"id":"<uuid>","cwd":"..."}`. The session
//     id is captured via piSessionIDInterceptor (pijsonlparser.go) wired into
//     the shared loop's implIsSessionIDCaptured block (PI-012a: forced-exec
//     substrate). On the resume turn the captured id is passed as --session
//     <id> in the argv (buildPiLaunchSpec, pilaunchspec.go).
//
// Because Pi delivers its task via argv, Seed and Retask are no-ops. Teardown
// is load-bearing (PI-014): the agent_end watcher in piSessionIDInterceptor
// calls Teardown→Kill on the terminal NDJSON event so a hung Pi does not burn
// the full 90-minute ceiling. The real retask mechanism is the resume argv: on
// iteration ≥2 the next turn's RunCtx carries PriorSessionID (the captured
// session id) and buildPiLaunchSpec emits `pi --mode json --session <id> ...`.
//
// Spec: specs/pi-harness.md §1 (PI-010/011/012/013/014).
// Design: ~/.kerf/projects/gregberns-harmonik/pilot/04-design/pi-harness-design.md §3.1/§3.3/§3.4.
// See also: codexharness.go (structural template), pilaunchspec.go, pijsonlparser.go.
// Beads: hk-4rmj1 (PI-010/012/013); hk-mkcwg (PI-014).

import (
	"context"
	"io"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// PiHarness implements handlercontract.Harness for the Pi agent.
//
// The zero value is not valid; construct via NewPiHarness. The struct carries
// Pi config fields (piBinary, provider, model, apiKeyEnv, apiKeyFile) that
// buildPiLaunchSpec requires; all default sensibly when empty (piBinary defaults
// to "pi"; provider/model/apiKeyEnv are validated by buildPiLaunchSpec on use).
type PiHarness struct {
	// piBinary is the pi executable path. Empty is normalised to "pi".
	piBinary string

	// provider is the Pi provider string (from harnesses.pi.provider config).
	// Required for initial-turn launches (priorSessionID == nil).
	provider string

	// model is the Pi model string in "provider/id" form (from harnesses.pi.model).
	// Required for initial-turn launches.
	model string

	// apiKeyEnv is the name of the env var the Pi child expects for the provider
	// API key (from harnesses.pi.api_key_env). Name only — no secret stored here.
	apiKeyEnv string

	// apiKeyFile is the OPTIONAL expanded path to a file holding the raw provider
	// API key (from harnesses.pi.api_key_file, pre-expanded by ResolvePiConfig).
	// When non-empty, the key is read from this file at launch time; the daemon
	// ambient env never carries the secret (PI-050/hk-xmfoi).
	apiKeyFile string
}

// NewPiHarness returns a ready PiHarness.
//
// All parameters may be empty; buildPiLaunchSpec validates them at launch time
// (missing provider/model on initial turn → launch error, not panic). The
// default empty piBinary is normalised to "pi" by buildPiLaunchSpec.
// apiKeyFile is optional; pass empty string when api_key_file is not configured.
func NewPiHarness(piBinary, provider, model, apiKeyEnv, apiKeyFile string) *PiHarness {
	return &PiHarness{
		piBinary:   piBinary,
		provider:   provider,
		model:      model,
		apiKeyEnv:  apiKeyEnv,
		apiKeyFile: apiKeyFile,
	}
}

// Compile-time assertion: *PiHarness satisfies handlercontract.Harness.
var _ handlercontract.Harness = (*PiHarness)(nil)

// AgentType returns core.AgentTypePi — the registry key for this harness.
// PI-010.
func (h *PiHarness) AgentType() core.AgentType {
	return core.AgentTypePi
}

// LaunchSpec converts rc to a piRunCtx, calls buildPiLaunchSpec, and returns
// the subprocess SpawnSpec (Binary/Args/Env/WorkDir).
//
// The resume argv is selected when rc.PriorSessionID is non-nil: PriorSessionID
// carries the captured Pi session id from the prior turn's first NDJSON session
// header, and buildPiLaunchSpec emits `pi --mode json --session <id> "<prompt>"`.
// For the initial turn PriorSessionID is nil and the initial argv is built.
//
// Returns a non-nil error on any buildPiLaunchSpec failure; the caller MUST NOT
// call handler.Launch on error. PI-010/PI-020.
func (h *PiHarness) LaunchSpec(rc handlercontract.RunCtx) (handlercontract.SpawnSpec, error) {
	prc := piRunCtx{
		piBinary:       h.piBinary,
		workspacePath:  rc.WorkspacePath,
		beadID:         rc.BeadID,
		provider:       h.provider,
		model:          h.model,
		apiKeyEnv:      h.apiKeyEnv,
		apiKeyFile:     h.apiKeyFile,
		priorSessionID: rc.PriorSessionID,
		baseEnv:        rc.BaseEnv,
	}

	spec, err := buildPiLaunchSpec(prc)
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
// For Pi this is a no-op: the task is delivered via the seed-prompt argv built
// by buildPiLaunchSpec (Pi has no TUI/paste path). PI-010/PI-015.
func (h *PiHarness) Seed(_ handlercontract.Session, _ handlercontract.RunCtx) error {
	return nil
}

// Retask delivers review feedback for iteration ≥2 to a re-spawned session.
//
// For Pi this is a no-op: there is no live REPL to drive. The real retask
// mechanism is the resume argv — the next turn's RunCtx carries the captured
// session id as PriorSessionID, and LaunchSpec emits
// `pi --mode json --session <id> "<feedback>"`. PI-010.
func (h *PiHarness) Retask(_ handlercontract.Session, _ string, _ handlercontract.RunCtx) error {
	return nil
}

// Teardown ends the session so the shared loop's sess.Wait returns.
//
// Pi's process exit is unreliable (#4303/#161/#4942 — `--mode json` may hang in
// epoll_wait with /dev/null stdin). Teardown is load-bearing: Kill is always
// invoked so a hung Pi does not burn the 90-minute commitHardCeiling. This is
// the target of the PI-014 agent_end watcher (piSessionIDInterceptor fires
// Teardown on {"type":"agent_end"}); it is also called defensively by the shared
// loop on timeout/ceiling paths. A nil session is a no-op. PI-010/PI-014.
func (h *PiHarness) Teardown(sess handlercontract.Session) error {
	if sess == nil {
		return nil
	}
	return sess.Kill(context.Background())
}

// DetectReady reports whether ev is the agent_ready signal for this harness.
//
// Pi has no distinct readiness handshake in `--mode json`; the harness is
// ProcessExit, so the shared loop bypasses the agent-ready wait entirely
// (workloop.go ~3999). DetectReady is nevertheless required to be HC-041
// correct: it MUST NOT return true for launch_initiated. The positive type
// check below can only match agent_ready — launch_initiated never will.
// PI-013.
func (h *PiHarness) DetectReady(ev handlercontract.EventEnvelope) bool {
	return core.EventType(ev.Type) == core.EventTypeAgentReady
}

// SessionIDPolicy returns SessionIDCaptured: Pi does not accept a caller-minted
// session id. The session id is captured from the `id` field of Pi's first
// NDJSON line `{"type":"session",...}` via piSessionIDInterceptor
// (pijsonlparser.go), wired into the exec-path StdoutWrapper by the shared
// launch code (reviewloop.go implIsSessionIDCaptured block).
//
// The forced-exec substrate (PI-012a) is load-bearing: the launch code forces
// implSpec.Substrate = nil for SessionIDCaptured harnesses so stdout is an
// io.Reader and the StdoutWrapper fires. Without it, session-id capture silently
// no-ops. PI-012.
func (h *PiHarness) SessionIDPolicy() handlercontract.SessionIDPolicy {
	return handlercontract.SessionIDCaptured
}

// Completion returns CompletionProcessExit: `pi --mode json` nominally self-
// terminates on turn completion. The shared loop bypasses pasteInjectQuitOnCommit
// and relies on sess.Wait + the absolute commitHardCeiling. PI-014 adds an
// event-driven kill (via the agent_end watcher) as a reliable backstop for Pi's
// unreliable process exit (#4303). PI-011.
func (h *PiHarness) Completion() handlercontract.CompletionMode {
	return handlercontract.CompletionProcessExit
}

// NewSessionIDInterceptor returns a piSessionIDInterceptor wrapping inner.
//
// The interceptor handles two callbacks (PI-012 + PI-014):
//
//   - sessionIDCb fires once on the first {"type":"session","id":"<uuid>",...}
//     NDJSON line, supplying the captured session id for the resume turn argv.
//   - agentEndCb fires once on {"type":"agent_end",...}; the caller wires it to
//     Teardown→Kill so a hung Pi does not burn the 90-minute ceiling (PI-014).
//
// All bytes pass through unchanged. Called by the shared loop's
// implIsSessionIDCaptured block without concrete-type branching. PI-012/PI-014.
func (h *PiHarness) NewSessionIDInterceptor(inner io.Reader, sessionIDCb func(string), agentEndCb func()) io.Reader {
	return newPiSessionIDInterceptor(inner, sessionIDCb, agentEndCb)
}
