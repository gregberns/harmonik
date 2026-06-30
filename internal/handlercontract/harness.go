// harness.go — Harness interface + CompletionMode/SessionIDPolicy enums (codex-harness T1, hk-e8omz).
//
// A Harness abstracts the per-implementation-harness behaviors within a harmonik run so that
// a second harness (OpenAI codex) can be selected per-run while all shared infrastructure
// (tmux substrate, worktree mgmt, commit-detection, merge, review-loop control flow) stays
// untouched.
//
// Spec: specs/harness-contract.md §2 (normative contract).
// See also: C1-harness-interface-spec.md for the full AC set.
package handlercontract

import (
	"io"

	"github.com/gregberns/harmonik/internal/core"
)

// CompletionMode declares how a harness run signals completion to the shared loop.
//
// Spec: specs/harness-contract.md §2 N2.
type CompletionMode int

const (
	// CompletionEventStreamThenQuit — the run completes via the event-stream /quit path.
	// The shared loop sends /quit + kill grace to the session and waits for sess.Wait.
	// Used by the claude harness.
	CompletionEventStreamThenQuit CompletionMode = iota

	// CompletionProcessExit — the harness process self-terminates on turn completion.
	// The shared loop bypasses pasteInjectQuitOnCommit and relies on sess.Wait +
	// the absolute commitHardCeiling (90m). Used by the codex harness.
	CompletionProcessExit
)

// SessionIDPolicy declares how a harness obtains its session identifier.
//
// Spec: specs/harness-contract.md §2 N3.
type SessionIDPolicy int

const (
	// SessionIDMinted — the session ID is a caller-minted UUIDv7 passed to the
	// harness before launch via RunCtx.PriorSessionID (for resume) or freshly
	// generated. Used by the claude harness.
	SessionIDMinted SessionIDPolicy = iota

	// SessionIDCaptured — the session ID is obtained from the harness after launch
	// (e.g. codex thread_id from the first JSONL event). The caller records it for
	// subsequent Retask calls.
	SessionIDCaptured
)

// SpawnSpec carries the subprocess configuration returned by Harness.LaunchSpec.
// It maps to the per-spawn binary + argv + env + cwd tuple.
//
// The Env slice MUST apply the harness's credential strip+empty-override per
// specs/harness-contract.md §2 N1 (claude: ANTHROPIC_*; codex: OPENAI_API_KEY/CODEX_API_KEY).
type SpawnSpec struct {
	// Binary is the absolute path to the harness executable.
	Binary string

	// Args are the command-line arguments forwarded to Binary.
	Args []string

	// Env is the full subprocess environment in "KEY=VALUE" form. Credential env
	// vars for this harness MUST be stripped and re-emitted as empty overrides (N1).
	Env []string

	// WorkDir is the working-directory for the subprocess (typically the worktree path).
	WorkDir string
}

// RunCtx carries the per-launch inputs passed to Harness methods.
// It is assembled by the daemon cascade from the resolved bead record and config,
// then passed read-only to each Harness method.
//
// Harness implementations MUST treat all fields as read-only.
type RunCtx struct {
	// RunID is the UUIDv7 run identifier for this dispatch.
	RunID core.RunID

	// BeadID is the opaque bead correlation identifier.
	BeadID string

	// WorkspacePath is the absolute path to the worktree assigned to this bead.
	WorkspacePath string

	// DaemonSocket is the UNIX-domain socket path for the hook-relay.
	DaemonSocket string

	// WorkflowMode is the resolved workflow mode for this run.
	WorkflowMode core.WorkflowMode

	// Phase is the review-loop phase, or empty for single-mode.
	Phase ReviewLoopPhase

	// IterationCount is the 1-based iteration index; zero means non-iterating (single-mode).
	IterationCount int

	// HandlerBinary is the resolved path to the handler executable.
	HandlerBinary string

	// DaemonBinaryPath is the absolute path to the running harmonik binary.
	// Used so hook "command" fields reference an absolute path (hk-kqdpf.6).
	DaemonBinaryPath string

	// BaseEnv is the base environment from daemon Config.HandlerEnv.
	BaseEnv []string

	// BeadTitle is the human-readable bead title from the Beads ledger.
	BeadTitle string

	// BeadDescription is the bead body verbatim from the Beads ledger.
	BeadDescription string

	// NodePrompt is the optional inline LLM prompt from the DOT node's prompt= attribute.
	// When non-empty and Phase is implementer, it replaces BeadDescription as the task body.
	NodePrompt string

	// AgentTaskReAttach signals a re-attach (daemon restart mid-session); WriteAgentTask
	// skips collision check when true.
	AgentTaskReAttach bool

	// PriorVerdictFile is the absolute path to the archived reviewer verdict for the
	// preceding iteration. Set only for Phase = implementer-resume; empty otherwise.
	PriorVerdictFile string

	// PriorVerdictSummary is a short human-readable summary of the prior verdict.
	// Set only for Phase = implementer-resume; empty otherwise.
	PriorVerdictSummary string

	// ReviewBaseSHA is the base commit SHA for the diff under review.
	// Set only for Phase = reviewer; empty otherwise.
	ReviewBaseSHA string

	// ReviewHeadSHA is the head commit SHA for the diff under review.
	// Set only for Phase = reviewer; empty otherwise.
	ReviewHeadSHA string

	// Model is the resolved model alias from the ModelPreference descriptor (EM-012b).
	// Empty means no model flag is emitted (tool default).
	Model string

	// Effort is the resolved effort level from the ModelPreference descriptor.
	// Empty means no effort flag is emitted.
	Effort string

	// WorktreeRootPath is the absolute path to the harmonik worktrees root directory.
	// Used to decide whether to emit --dangerously-skip-permissions (HC-055b).
	WorktreeRootPath string

	// ExtraContext is an optional operator-supplied string injected into agent-task.md
	// as an "## Extra Context" section (hk-boiwe). Empty means no section is rendered.
	ExtraContext string

	// BaseBranch is the resolved lands_on branch for this run (hk-mtm0w).
	BaseBranch string

	// PriorSessionID carries the prior session identifier for iteration ≥2.
	// For claude: the prior UUIDv7 session ID (used for --resume).
	// For codex:  the captured thread_id from the prior turn.
	// Nil for first-iteration (implementer-initial) and reviewer launches.
	PriorSessionID *string
}

// Harness abstracts the per-harness behaviors that vary between claude and codex.
//
// The shared loop (tmux substrate, worktree mgmt, commit-detection, merge, DOT cascade,
// review-loop control flow) is harness-blind and MUST NOT be branched per harness except
// at the two declared seam points: launchSpecBuilder/registry lookup and the
// Completion() gate at dot_cascade.go:643 (specs/harness-contract.md §2 N5).
//
// Spec: specs/harness-contract.md §2.
type Harness interface {
	// AgentType returns the harness's agent-type identifier, used as the registry key.
	AgentType() core.AgentType

	// LaunchSpec returns the subprocess configuration for one spawn.
	// The returned SpawnSpec.Env MUST strip+empty-override harness credential vars (N1).
	// Returns a non-nil error on any failure; caller MUST NOT call handler.Launch on error.
	LaunchSpec(rc RunCtx) (SpawnSpec, error)

	// Seed delivers the first-turn task to a freshly-spawned session.
	// claude: splash-dismiss + bracketed-paste of agent-task.md.
	// codex:  no-op (task delivered via argv/stdin in LaunchSpec).
	Seed(sess Session, rc RunCtx) error

	// Retask delivers review feedback for iteration ≥2 to a (re-spawned) session.
	// claude: pastes combined task+feedback into the claude --resume REPL.
	// codex:  no-op (LaunchSpec for iter≥2 uses `codex exec resume <thread_id>`).
	Retask(sess Session, feedback string, rc RunCtx) error

	// Teardown ends the session so the shared loop's sess.Wait returns.
	// claude: /quit + 60s grace + Kill.
	// codex:  no-op (exec self-terminates on turn completion).
	Teardown(sess Session) error

	// DetectReady reports whether ev is the agent_ready signal for this harness.
	// MUST NOT return true for launch_initiated (HC-041 hard rule).
	DetectReady(ev EventEnvelope) bool

	// SessionIDPolicy reports how this harness obtains its session identifier.
	SessionIDPolicy() SessionIDPolicy

	// Completion reports how this harness signals run completion.
	// Governs whether the shared loop runs the heartbeat-staleness kill path (N2).
	Completion() CompletionMode

	// NewSessionIDInterceptor returns an io.Reader that wraps inner, fires cb
	// exactly once with the captured session identifier, and passes all bytes
	// through unchanged. It is called by the shared loop in the
	// implIsSessionIDCaptured block to obtain the harness-appropriate NDJSON
	// interceptor without the loop branching on a concrete harness type.
	//
	// Each SessionIDCaptured harness implements this with its own parser (e.g.
	// PiHarness parses {"type":"session","id":"..."}, CodexHarness parses
	// {"type":"thread.started","thread_id":"..."}). Non-SessionIDCaptured
	// harnesses (e.g. ClaudeHarness) MUST return inner unchanged — the method
	// will never be called for those harnesses because the implIsSessionIDCaptured
	// gate prevents entry, but the interface requires an implementation.
	NewSessionIDInterceptor(inner io.Reader, cb func(string)) io.Reader
}
