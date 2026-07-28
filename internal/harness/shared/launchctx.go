package shared

// launchctx.go — LaunchCtx / LaunchArtifacts, the universal per-launch DTO
// threaded through the daemon's LaunchPort seam (internal/daemon/runports.go).
//
// This type used to be called claudeRunCtx and lived in
// internal/daemon/claudelaunchspec.go, which mis-stated its scope: it is NOT a
// claude type. It is the parameter type of LaunchPort.BuildSpec, and
// buildCodexRoutedLaunchSpec (internal/daemon/harnessregistry.go) feeds the very
// same value to the codex and pi harnesses. Five of its fields — Provider,
// APIKeyEnv, APIKeyFile, BaseURL, API — are read only on the pi path and are
// never looked at by the claude builder.
//
// It lives in shared, not in any one harness impl, precisely so that no harness
// has to import a sibling to be launched: the daemon fills the DTO in, each
// impl reads the subset it understands.
//
// Origin: internal/daemon/claudelaunchspec.go lines 47–231, moved verbatim
// (field spellings exported, doc comments otherwise unchanged) by
// plans/2026-07-21-p2-extraction/E1b-claude.md unit E1b-prep.

import (
	"encoding/json"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// LaunchCtx carries the per-launch inputs to a harness launch-spec builder.
// The caller assembles this from a bead record and daemon configuration; the
// helper treats all fields as read-only.
type LaunchCtx struct {
	// RunID is the UUIDv7 run identifier for this dispatch.
	RunID core.RunID

	// BeadID is the opaque bead correlation identifier.
	BeadID string

	// WorkspacePath is the absolute path to the worktree assigned to this bead.
	WorkspacePath string

	// DaemonSocket is the UNIX-domain socket path for the hook-relay, typically
	// <ProjectDir>/.harmonik/daemon.sock.
	DaemonSocket string

	// WorkflowMode is the resolved workflow mode for this run (e.g. "single",
	// "review-loop").
	WorkflowMode core.WorkflowMode

	// Phase is the review-loop phase string, or the empty string for single-mode.
	// For review-loop, one of {implementer-initial, implementer-resume, reviewer}.
	Phase handlercontract.ReviewLoopPhase

	// IterationCount is the 1-based iteration index for review-loop runs.
	// Zero or negative means this is not a multi-phase run (single-mode).
	IterationCount int

	// PriorClaudeSessID is non-nil only for the implementer-resume phase; it
	// carries the Claude session ID minted by the previous implementer-initial
	// launch in the same cycle. All other phases MUST pass nil.
	PriorClaudeSessID *string

	// HandlerBinary is the resolved path to the handler executable, taken from
	// daemon Config (e.g. "claude" or "/usr/local/bin/harmonik-twin-claude").
	HandlerBinary string

	// DaemonBinaryPath is the absolute path to the running harmonik binary,
	// resolved via os.Executable() at daemon startup (hk-kqdpf.6). Passed to
	// MaterializeClaudeSettings so the hook "command" field in settings.json
	// references an absolute path rather than the bare "harmonik" name.
	DaemonBinaryPath string

	// BaseEnv is the base environment inherited from daemon Config.HandlerEnv,
	// which MUST already include HARMONIK_PROJECT_HASH per PL-006a. CHB-006
	// vars are appended (or overwrite) by ClaudeEnvVars.
	BaseEnv []string

	// BeadTitle is the human-readable bead title from the Beads ledger.
	// Used to populate the "title:" header in the CHB-028 agent-task.md.
	// When empty, beadID is substituted.
	BeadTitle string

	// BeadDescription is the bead body verbatim from the Beads ledger.
	// Used to populate the "## Task Description" section in agent-task.md
	// per CHB-028. When empty, a placeholder is used so the file is never
	// structurally empty.
	BeadDescription string

	// NodePrompt is the optional inline LLM prompt from the DOT node's prompt=
	// attribute (WG-040 §I.3, HC-006a §III.3). When non-empty and phase is
	// implementer-initial or implementer-resume, it REPLACES beadDescription as
	// the Body channel of the agent-task.md (CHB-028). On reviewer phase, it is
	// accepted-but-inert (EM-015d-RIA). Empty when the node has no prompt= attr.
	NodePrompt string

	// AgentTaskReAttach signals that this launch is on the re-attach path
	// (daemon restart mid-session). When true, WriteAgentTask skips collision
	// check and returns nil if agent-task.md already exists (CHB-028
	// re-launch semantics).
	AgentTaskReAttach bool

	// PriorVerdictFile is the absolute path to the archived reviewer verdict
	// for the immediately preceding iteration (.harmonik/review.iter-<N-1>.json).
	// Set only for phase = implementer-resume; empty otherwise.
	PriorVerdictFile string

	// PriorVerdictSummary is a short human-readable summary of the prior
	// verdict. Set only for phase = implementer-resume; empty otherwise.
	PriorVerdictSummary string

	// ReviewBaseSHA is the base commit SHA for the diff under review.
	// Set only for phase = reviewer; empty otherwise.
	ReviewBaseSHA string

	// ReviewHeadSHA is the head commit SHA for the diff under review.
	// Set only for phase = reviewer; empty otherwise.
	ReviewHeadSHA string

	// Model is the resolved model alias from the ModelPreference descriptor
	// (EM-012b / HC-055a). When non-empty, --model <model> is appended to argv.
	// The value must satisfy the shape constraint ^[A-Za-z0-9._:/-]+$ and be
	// ≤ 128 chars; violation returns *ModelPreferenceError before LaunchSpec is built.
	// Empty means no model flag is emitted (tool default).
	Model string

	// Effort is the resolved effort level from the ModelPreference descriptor
	// (EM-012b / HC-055a). When non-empty, --effort <effort> is appended to argv.
	// Must be one of {low, medium, high, xhigh, max}; empty means no flag emitted.
	// Violation returns *ModelPreferenceError before LaunchSpec is built.
	Effort string

	// Provider, APIKeyEnv, APIKeyFile, BaseURL, API are the per-bead Pi provider
	// tuple resolved by resolvePiProfile from a `profile:<name>` label
	// (pi-provider-switch, hk-m6uu2). Empty ⇒ harness-global default (C4
	// fallback in PiHarness.LaunchSpec). Zero-value for any non-pi-resolved bead
	// (hk-pkugu harness gate). Only meaningful when the resolved agent type is
	// core.AgentTypePi.
	Provider   string
	APIKeyEnv  string
	APIKeyFile string
	BaseURL    string
	API        string

	// WorktreeRootPath is the absolute path to the harmonik worktrees root
	// directory (e.g. <projectDir>/.harmonik/worktrees). When non-empty,
	// the claude launch-spec builder checks whether WorkspacePath canonicalizes to a
	// path under this prefix; if so, --dangerously-skip-permissions is added
	// to argv per specs/handler-contract.md §4.10 HC-055b.
	//
	// When empty (e.g. in tests that do not need the flag), the path-check is
	// skipped and the flag is not emitted.
	WorktreeRootPath string

	// ExtraContext is an optional operator-supplied free-form string injected
	// into the agent-task.md as an "## Extra Context" section (hk-boiwe).
	// Empty means no section is rendered. Passed through to AgentTaskPayload.
	ExtraContext string

	// BaseBranch is the resolved lands_on branch for this run (hk-mtm0w).
	// Passed into AgentTaskPayload so the implementer sees base_branch in the
	// agent-task header and can rebase against origin/$baseBranch pre-exit.
	// Empty when the caller cannot resolve branching config (non-fatal).
	BaseBranch string

	// Runner is the CommandRunner for materializing the run's launch artifacts
	// (.claude/settings.json, .harmonik/agent-task.md, ~/.claude.json trust).
	// It is the worker's SSHRunner for a REMOTE run — so the three writes land
	// on the WORKER's filesystem where the worktree actually lives — and nil for
	// a LOCAL run, in which case the materialization takes the byte-identical
	// box-A-local os.* path (NFR7). Threaded from workloop's rbc.sshRunner (hk-z8ek).
	Runner tmux.CommandRunner

	// WorkerBinaryPath is the absolute path to harmonik ON THE WORKER, used as the
	// hook "command" field in the worker's .claude/settings.json for a REMOTE run
	// (the hook subprocess is executed on the worker, so a box-A path would not
	// exist there). Empty for LOCAL runs, where daemonBinaryPath (box A's path) is
	// used unchanged. Set by the caller only when runner != nil (hk-z8ek).
	WorkerBinaryPath string
}

// LaunchArtifacts carries the values that the workloop and review-loop
// need after a harness launch-spec builder returns, in addition to the
// handler.LaunchSpec.
type LaunchArtifacts struct {
	// ClaudeSessionID is the Claude session ID minted (or reused) by
	// MintClaudeSessionID for this launch. The caller stores it so it can be
	// passed as PriorClaudeSessID on the next implementer-resume launch.
	ClaudeSessionID string

	// SessionLogPath is the Claude transcript path derived from the workspace
	// and session ID, as reported via the session_log_location message (CHB-018).
	SessionLogPath string

	// HandlerSessionID is a freshly minted UUIDv7 identifying this particular
	// handler session within harmonik's event bus. Distinct from ClaudeSessionID.
	HandlerSessionID string

	// PreExecMsgs holds the 4 ordered pre-exec progress messages (handler_capabilities,
	// session_log_location, skills_provisioned, agent_ready) in compact JSON form.
	// The caller MUST emit these on the bus BEFORE calling handler.Launch per CHB-018.
	PreExecMsgs []json.RawMessage

	// Substrate is the optional tmux-substrate reference for this session.
	// This is always nil; the handler falls back to exec.CommandContext.
	// TODO(hk-gql20.x): wire tmux substrate once component-2 lands.
	Substrate interface{}

	// ResolvedAgentType is the agent_type resolved by the four-tier harness
	// precedence walk (resolveHarness). Set by routedLaunchSpecBuilder (T12,
	// hk-xhawy) so callers can look up the correct Adapter via
	// adapterRegistry.ForAgent(ResolvedAgentType) instead of hardcoding claude-code.
	// Zero value ("") means the caller should default to core.AgentTypeClaudeCode.
	ResolvedAgentType core.AgentType
}

// ArtifactAgentType returns the resolved agent type from LaunchArtifacts,
// falling back to core.AgentTypeClaudeCode when the field is empty (e.g. from a
// legacy test fixture that builds artifacts directly without going through
// routedLaunchSpecBuilder).
//
// Used to look up the correct Adapter via adapterRegistry.ForAgent instead of
// hardcoding core.AgentTypeClaudeCode (T12, hk-xhawy).
//
// Kept a free function rather than a method on LaunchArtifacts: a method would
// read better but is a signature change, which the P2 extraction plan forbids
// inside a pure move. Origin: internal/daemon/workloop.go artifactAgentType,
// moved by plans/2026-07-21-p2-extraction/RT19b-stranded-run-path-helpers.md.
func ArtifactAgentType(a LaunchArtifacts) core.AgentType {
	if a.ResolvedAgentType.Valid() {
		return a.ResolvedAgentType
	}
	return core.AgentTypeClaudeCode
}
