package daemon

// claudelaunchspec.go — buildClaudeLaunchSpec helper (hk-gql20.13).
//
// Threads together all bridge pieces required to launch a Claude Code (or
// harmonik-twin-claude) subprocess for any workflow phase:
//
//   - MintClaudeSessionID — fresh UUIDv7 or resume reuse (CHB-008/009).
//   - DeriveCIaudeTranscriptPath — session log path (CHB-018 step 2).
//   - MaterializeClaudeSettings — atomic hook-bridge settings write (CHB-001..005).
//   - CheckSettingsLocalJSON — fail-fast if settings.local.json shadows hooks (CHB-024).
//   - ClaudeEnvVars — CHB-006 env-var set.
//   - argv construction — --session-id or --resume per CHB-008 (OQ3: allow-list).
//     Appends --model and --effort when claudeRunCtx fields are non-empty (HC-055a).
//   - CheckForbiddenFlags — deny-list guard (CHB-007).
//   - PreExecMessages — 4 ordered pre-exec progress messages (CHB-018).
//
// The helper is twin-blind: the same code path is used whether Binary points to
// "claude" or "harmonik-twin-claude". The Binary field of the returned
// handler.LaunchSpec is opaque to this helper — the caller sets it from
// claudeRunCtx.handlerBinary.
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.2 CHB-006..009, §4.7 CHB-018..019, §4.9 CHB-024.
//   - specs/handler-contract.md §4.2 HC-055 (flag allow-list), §4.10 HC-055a (ModelPreference invariants).
//   - specs/execution-model.md §4.3 EM-012b (model/effort resolution chain).
//
// Bead: hk-gql20.13, hk-xo03m

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
	"github.com/gregberns/harmonik/internal/workspace"
)

// claudeRunCtx carries the per-launch inputs to buildClaudeLaunchSpec.
// The caller assembles this from a bead record and daemon configuration; the
// helper treats all fields as read-only.
type claudeRunCtx struct {
	// runID is the UUIDv7 run identifier for this dispatch.
	runID core.RunID

	// beadID is the opaque bead correlation identifier.
	beadID string

	// workspacePath is the absolute path to the worktree assigned to this bead.
	workspacePath string

	// daemonSocket is the UNIX-domain socket path for the hook-relay, typically
	// <ProjectDir>/.harmonik/daemon.sock.
	daemonSocket string

	// workflowMode is the resolved workflow mode for this run (e.g. "single",
	// "review-loop").
	workflowMode core.WorkflowMode

	// phase is the review-loop phase string, or the empty string for single-mode.
	// For review-loop, one of {implementer-initial, implementer-resume, reviewer}.
	phase handlercontract.ReviewLoopPhase

	// iterationCount is the 1-based iteration index for review-loop runs.
	// Zero or negative means this is not a multi-phase run (single-mode).
	iterationCount int

	// priorClaudeSessID is non-nil only for the implementer-resume phase; it
	// carries the Claude session ID minted by the previous implementer-initial
	// launch in the same cycle. All other phases MUST pass nil.
	priorClaudeSessID *string

	// handlerBinary is the resolved path to the handler executable, taken from
	// daemon Config (e.g. "claude" or "/usr/local/bin/harmonik-twin-claude").
	handlerBinary string

	// daemonBinaryPath is the absolute path to the running harmonik binary,
	// resolved via os.Executable() at daemon startup (hk-kqdpf.6). Passed to
	// MaterializeClaudeSettings so the hook "command" field in settings.json
	// references an absolute path rather than the bare "harmonik" name.
	daemonBinaryPath string

	// baseEnv is the base environment inherited from daemon Config.HandlerEnv,
	// which MUST already include HARMONIK_PROJECT_HASH per PL-006a. CHB-006
	// vars are appended (or overwrite) by ClaudeEnvVars.
	baseEnv []string

	// beadTitle is the human-readable bead title from the Beads ledger.
	// Used to populate the "title:" header in the CHB-028 agent-task.md.
	// When empty, beadID is substituted.
	beadTitle string

	// beadDescription is the bead body verbatim from the Beads ledger.
	// Used to populate the "## Task Description" section in agent-task.md
	// per CHB-028. When empty, a placeholder is used so the file is never
	// structurally empty.
	beadDescription string

	// nodePrompt is the optional inline LLM prompt from the DOT node's prompt=
	// attribute (WG-040 §I.3, HC-006a §III.3). When non-empty and phase is
	// implementer-initial or implementer-resume, it REPLACES beadDescription as
	// the Body channel of the agent-task.md (CHB-028). On reviewer phase, it is
	// accepted-but-inert (EM-015d-RIA). Empty when the node has no prompt= attr.
	nodePrompt string

	// agentTaskReAttach signals that this launch is on the re-attach path
	// (daemon restart mid-session). When true, WriteAgentTask skips collision
	// check and returns nil if agent-task.md already exists (CHB-028
	// re-launch semantics).
	agentTaskReAttach bool

	// priorVerdictFile is the absolute path to the archived reviewer verdict
	// for the immediately preceding iteration (.harmonik/review.iter-<N-1>.json).
	// Set only for phase = implementer-resume; empty otherwise.
	priorVerdictFile string

	// priorVerdictSummary is a short human-readable summary of the prior
	// verdict. Set only for phase = implementer-resume; empty otherwise.
	priorVerdictSummary string

	// reviewBaseSHA is the base commit SHA for the diff under review.
	// Set only for phase = reviewer; empty otherwise.
	reviewBaseSHA string

	// reviewHeadSHA is the head commit SHA for the diff under review.
	// Set only for phase = reviewer; empty otherwise.
	reviewHeadSHA string

	// model is the resolved model alias from the ModelPreference descriptor
	// (EM-012b / HC-055a). When non-empty, --model <model> is appended to argv.
	// The value must satisfy the shape constraint ^[A-Za-z0-9._:/-]+$ and be
	// ≤ 128 chars; violation returns *ModelPreferenceError before LaunchSpec is built.
	// Empty means no model flag is emitted (tool default).
	model string

	// effort is the resolved effort level from the ModelPreference descriptor
	// (EM-012b / HC-055a). When non-empty, --effort <effort> is appended to argv.
	// Must be one of {low, medium, high, xhigh, max}; empty means no flag emitted.
	// Violation returns *ModelPreferenceError before LaunchSpec is built.
	effort string

	// worktreeRootPath is the absolute path to the harmonik worktrees root
	// directory (e.g. <projectDir>/.harmonik/worktrees). When non-empty,
	// buildClaudeLaunchSpec checks whether workspacePath canonicalizes to a
	// path under this prefix; if so, --dangerously-skip-permissions is added
	// to argv per specs/handler-contract.md §4.10 HC-055b.
	//
	// When empty (e.g. in tests that do not need the flag), the path-check is
	// skipped and the flag is not emitted.
	worktreeRootPath string

	// extraContext is an optional operator-supplied free-form string injected
	// into the agent-task.md as an "## Extra Context" section (hk-boiwe).
	// Empty means no section is rendered. Passed through to AgentTaskPayload.
	extraContext string

	// baseBranch is the resolved lands_on branch for this run (hk-mtm0w).
	// Passed into AgentTaskPayload so the implementer sees base_branch in the
	// agent-task header and can rebase against origin/$baseBranch pre-exit.
	// Empty when the caller cannot resolve branching config (non-fatal).
	baseBranch string
}

// claudeRunArtifacts carries the values that the workloop and review-loop
// need after buildClaudeLaunchSpec returns, in addition to the LaunchSpec.
type claudeRunArtifacts struct {
	// claudeSessionID is the Claude session ID minted (or reused) by
	// MintClaudeSessionID for this launch. The caller stores it so it can be
	// passed as priorClaudeSessID on the next implementer-resume launch.
	claudeSessionID string

	// sessionLogPath is the Claude transcript path derived from the workspace
	// and session ID, as reported via the session_log_location message (CHB-018).
	sessionLogPath string

	// handlerSessionID is a freshly minted UUIDv7 identifying this particular
	// handler session within harmonik's event bus. Distinct from claudeSessionID.
	handlerSessionID string

	// preExecMsgs holds the 4 ordered pre-exec progress messages (handler_capabilities,
	// session_log_location, skills_provisioned, agent_ready) in compact JSON form.
	// The caller MUST emit these on the bus BEFORE calling handler.Launch per CHB-018.
	preExecMsgs []json.RawMessage

	// substrate is the optional tmux-substrate reference for this session.
	// At MVH this is always nil; the handler falls back to exec.CommandContext.
	// TODO(hk-gql20.x): wire tmux substrate once component-2 lands.
	substrate interface{}

	// resolvedAgentType is the agent_type resolved by the four-tier harness
	// precedence walk (resolveHarness). Set by routedLaunchSpecBuilder (T12,
	// hk-xhawy) so callers can look up the correct Adapter via
	// adapterRegistry.ForAgent(resolvedAgentType) instead of hardcoding claude-code.
	// Zero value ("") means the caller should default to core.AgentTypeClaudeCode.
	resolvedAgentType core.AgentType
}

// buildClaudeLaunchSpec threads together all bridge pieces required to launch
// a Claude Code (or twin) subprocess for any workflow phase.
//
// The sequence follows the design in
// .kerf/projects/gregberns-harmonik/bridge-integration/04-research/component-3-4/design.md §1:
//
//  1. MintClaudeSessionID — mint fresh or reuse (CHB-008/009).
//  2. DeriveCIaudeTranscriptPath — session log location for CHB-018 step 2.
//  3. MaterializeClaudeSettings — atomic hook-bridge settings file (CHB-001..005).
//     3a. EnsureWorktreeTrust — pre-seed ~/.claude.json trust entry (CHB-029/WM-040b).
//     3b. WriteAgentTask — atomic agent-task.md write (CHB-028).
//  4. CheckSettingsLocalJSON — fail-fast on settings.local.json shadow (CHB-024).
//  5. Build ClaudeEnvConfig and call ClaudeEnvVars — CHB-006 env.
//  6. Build argv — --session-id or --resume per CHB-008 (OQ3 allow-list).
//  7. CheckForbiddenFlags — deny-list guard (CHB-007).
//  8. PreExecMessages — render 4 ordered progress messages (CHB-018).
//  9. Return handler.LaunchSpec + claudeRunArtifacts.
//
// Returns a non-nil error (wrapping handler.ErrStructural where applicable)
// if any step fails. The caller MUST NOT call handler.Launch on error.
//
// Spec refs: claude-hook-bridge.md §4.2..4.3, §4.7, §4.9;
// handler-contract.md HC-005, HC-055.
func buildClaudeLaunchSpec(ctx context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	_ = ctx // reserved for future async steps (e.g. skill provisioning)

	// Step 1 — MintClaudeSessionID (CHB-008, CHB-009).
	mintRes, err := handler.MintClaudeSessionID(string(rc.phase), rc.priorClaudeSessID)
	if err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: MintClaudeSessionID: %w", err)
	}

	// Step 2 — Derive Claude transcript path (CHB-018 step 2).
	sessionLogPath := handler.DeriveCIaudeTranscriptPath(rc.workspacePath, mintRes.ClaudeSessionID)

	// Step 3 — Materialize .claude/settings.json in the worktree (CHB-001..005).
	// Pass rc.daemonBinaryPath so the hook "command" field is an absolute path
	// rather than the bare "harmonik" name (hk-kqdpf.6 fix).
	if err := workspace.MaterializeClaudeSettings(rc.workspacePath, rc.daemonBinaryPath, sessionLogPath); err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: MaterializeClaudeSettings: %w", err)
	}

	// Step 3a — Pre-seed ~/.claude.json with worktree trust (CHB-029 / WM-040b).
	// MUST be after MaterializeClaudeSettings and BEFORE SubstrateSpawn.
	// Failure is a fatal structural error: an un-trusted session blocks indefinitely.
	if err := workspace.EnsureWorktreeTrust(rc.workspacePath); err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: EnsureWorktreeTrust: %w", err)
	}

	// Step 3b — Write per-launch task artifact (CHB-028).
	// MUST be after MaterializeClaudeSettings + EnsureWorktreeTrust and BEFORE SubstrateSpawn.
	// The file carries the bead description for the phase. When rc.beadDescription is empty
	// or whitespace-only (e.g. bead has no body, or --body " "), use the bead title so the
	// file is never structurally empty (hk-lpbu7: TrimSpace closes the whitespace-body livelock
	// where a " " description was non-empty at this layer but rejected by WriteAgentTask).
	taskBody := rc.beadDescription
	// When the DOT node carries an inline prompt= and the phase is implementer,
	// replace the bead-derived body with the prompt verbatim (WG-040 §I.3,
	// HC-006a §III.3). Bead Title + ID remain in the header for traceability.
	// Reviewer phase: nodePrompt is accepted-but-inert (EM-015d-RIA).
	if rc.nodePrompt != "" && rc.phase != handlercontract.ReviewLoopPhaseReviewer {
		taskBody = rc.nodePrompt
	}
	if strings.TrimSpace(taskBody) == "" {
		taskBody = rc.beadTitle
	}
	if taskBody == "" {
		// Last resort: use the bead ID so CHB-028's non-empty invariant is always satisfied.
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
			"daemon: buildClaudeLaunchSpec: WriteAgentTask: %w", err)
	}

	// Step 4 — Fail-fast if settings.local.json shadows bridge hooks (CHB-024).
	if err := handler.CheckSettingsLocalJSON(rc.workspacePath); err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: CheckSettingsLocalJSON: %w", err)
	}

	// Step 5 — Build ClaudeEnvConfig and derive the CHB-006 env slice.
	handlerSessUID, err := uuid.NewV7()
	if err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: mint handlerSessionID UUIDv7: %w", err)
	}
	handlerSessionID := handlerSessUID.String()

	// WorkflowID and NodeID: at MVH the bead is the workflow unit, so we
	// synthesise "bead/<beadID>" as the node identifier. WorkflowID reuses the
	// runID's UUID (run is the workflow scope at MVH).
	//
	// TODO(hk-gql20.x): replace with typed WorkflowID / NodeID from a workflow
	// registry once multi-node workflows are introduced.
	nodeID := "bead/" + rc.beadID
	workflowID := core.WorkflowID(core.RunID(rc.runID))

	// Build optional ClaudeEnvConfig fields.
	workflowModeStr := string(rc.workflowMode)
	phaseStr := string(rc.phase)
	iterCountStr := ""
	if rc.iterationCount > 0 {
		iterCountStr = strconv.Itoa(rc.iterationCount)
	}

	cfg := handler.ClaudeEnvConfig{
		RunID:            core.RunID(rc.runID).String(),
		DaemonSocket:     rc.daemonSocket,
		WorkspacePath:    rc.workspacePath,
		HandlerSessionID: handlerSessionID,
		ClaudeSessionID:  mintRes.ClaudeSessionID,
		WorkflowID:       core.WorkflowID(workflowID).String(),
		NodeID:           nodeID,
		WorkflowMode:     workflowModeStr,
		Phase:            phaseStr,
		IterationCount:   iterCountStr,
		BeadID:           rc.beadID,
		BaseEnv:          rc.baseEnv,
	}
	env := handler.ClaudeEnvVars(cfg)

	// Step 6 — Validate ModelPreference fields (HC-055a) before argv construction.
	// Invalid model or effort → typed *ModelPreferenceError; do NOT silently drop.
	if rc.model != "" {
		if err := validateModel(rc.model); err != nil {
			return handler.LaunchSpec{}, claudeRunArtifacts{}, err
		}
	}
	if rc.effort != "" {
		if err := validateEffort(rc.effort); err != nil {
			return handler.LaunchSpec{}, claudeRunArtifacts{}, err
		}
	}

	// Step 6b — Build argv (OQ3 allow-list: --session-id or --resume, then optional
	// --model and --effort per HC-055a, then --dangerously-skip-permissions per HC-055b).
	// CHB-008: use --resume <uuid> for implementer-resume, --session-id <uuid> otherwise.
	// Ordering per HC-055a: --session-id first, then --model, then --effort.
	var args []string
	if mintRes.ResumeMode {
		args = []string{"--resume", mintRes.ClaudeSessionID}
	} else {
		args = []string{"--session-id", mintRes.ClaudeSessionID}
	}
	if rc.model != "" {
		args = append(args, "--model", rc.model)
	}
	if rc.effort != "" {
		args = append(args, "--effort", rc.effort)
	}
	// HC-055b: emit --dangerously-skip-permissions iff workspacePath canonicalizes
	// to a path under the harmonik worktrees root. This suppresses the interactive
	// trust dialog in operator-daemon launches where the worktree is already
	// operator-sanctioned. The path check is a positive-allowlist match; if
	// EvalSymlinks fails for either path the flag is silently omitted.
	if isHarmonikManagedWorktree(rc.workspacePath, rc.worktreeRootPath) {
		args = append(args, "--dangerously-skip-permissions")
	}

	// Step 7 — Deny-list guard (CHB-007).
	if err := handler.CheckForbiddenFlags(args, env); err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: CheckForbiddenFlags: %w", err)
	}

	// Step 8 — Render pre-exec messages (CHB-018).
	runIDStr := core.RunID(rc.runID).String()
	rawMsgs, err := handler.PreExecMessages(
		runIDStr,
		handlerSessionID,
		nodeID,
		mintRes.ClaudeSessionID,
		sessionLogPath,
		nil, // skills = nil at MVH per design §1 step 9
	)
	if err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: PreExecMessages: %w", err)
	}
	preExecMsgs := make([]json.RawMessage, len(rawMsgs))
	for i, b := range rawMsgs {
		preExecMsgs[i] = json.RawMessage(b)
	}

	// Step 9 — Assemble handler.LaunchSpec and return.
	//
	// Binary is opaque to this helper; the caller sets it via rc.handlerBinary.
	// Substrate is nil at MVH; handler falls back to exec.CommandContext.
	spec := handler.LaunchSpec{
		Binary:  rc.handlerBinary,
		Args:    args,
		Env:     env,
		WorkDir: rc.workspacePath,
		Role:    string(rc.phase), // "implementer-initial", "implementer-resume", "reviewer", or "" (single)
	}

	artifacts := claudeRunArtifacts{
		claudeSessionID:   mintRes.ClaudeSessionID,
		sessionLogPath:    sessionLogPath,
		handlerSessionID:  handlerSessionID,
		preExecMsgs:       preExecMsgs,
		substrate:         nil,
		resolvedAgentType: core.AgentTypeClaudeCode,
	}

	return spec, artifacts, nil
}

// isHarmonikManagedWorktree reports whether workspacePath canonicalizes to a
// path under worktreeRootPath per specs/handler-contract.md §4.10 HC-055b.
//
// The check is a positive-allowlist match: both paths are resolved via
// os.EvalSymlinks before comparison. If either resolution fails (e.g. the
// directory has not yet been created) the function returns false and the caller
// omits --dangerously-skip-permissions without error.
//
// An empty worktreeRootPath always returns false (tests that do not populate
// the field should not receive the flag).
func isHarmonikManagedWorktree(workspacePath, worktreeRootPath string) bool {
	if worktreeRootPath == "" || workspacePath == "" {
		return false
	}
	canonRoot, err := filepath.EvalSymlinks(worktreeRootPath)
	if err != nil {
		return false
	}
	canonWS, err := filepath.EvalSymlinks(workspacePath)
	if err != nil {
		return false
	}
	// Ensure the prefix includes a trailing separator so that a root path that
	// is a prefix of another root path does not produce a false positive.
	// E.g., /foo/bar must not match /foo/barbaz.
	prefix := canonRoot + string(filepath.Separator)
	return strings.HasPrefix(canonWS, prefix)
}
