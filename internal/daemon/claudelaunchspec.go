package daemon

// claudelaunchspec.go — buildClaudeLaunchSpec helper (hk-gql20.13).
//
// Threads together all bridge pieces required to launch a Claude Code (or
// harmonik-twin-claude) subprocess for any workflow phase:
//
//   - MintClaudeSessionID — fresh UUIDv7 or resume reuse (CHB-008/009).
//   - DeriveClaudeTranscriptPath — session log path (CHB-018 step 2).
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
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
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

	// provider, apiKeyEnv, apiKeyFile, baseURL, api are the per-bead Pi provider
	// tuple resolved by resolvePiProfile from a `profile:<name>` label
	// (pi-provider-switch, hk-m6uu2). Empty ⇒ harness-global default (C4
	// fallback in PiHarness.LaunchSpec). Zero-value for any non-pi-resolved bead
	// (hk-pkugu harness gate). Only meaningful when the resolved agent type is
	// core.AgentTypePi.
	provider   string
	apiKeyEnv  string
	apiKeyFile string
	baseURL    string
	api        string

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

	// runner is the CommandRunner for materializing the run's launch artifacts
	// (.claude/settings.json, .harmonik/agent-task.md, ~/.claude.json trust).
	// It is the worker's SSHRunner for a REMOTE run — so the three writes land
	// on the WORKER's filesystem where the worktree actually lives — and nil for
	// a LOCAL run, in which case the materialization takes the byte-identical
	// box-A-local os.* path (NFR7). Threaded from workloop's rbc.sshRunner (hk-z8ek).
	runner tmux.CommandRunner

	// workerBinaryPath is the absolute path to harmonik ON THE WORKER, used as the
	// hook "command" field in the worker's .claude/settings.json for a REMOTE run
	// (the hook subprocess is executed on the worker, so a box-A path would not
	// exist there). Empty for LOCAL runs, where daemonBinaryPath (box A's path) is
	// used unchanged. Set by the caller only when runner != nil (hk-z8ek).
	workerBinaryPath string
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
//  2. DeriveClaudeTranscriptPath — session log location for CHB-018 step 2.
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
	sessionLogPath, err := handler.DeriveClaudeTranscriptPath(rc.workspacePath, mintRes.ClaudeSessionID)
	if err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: DeriveClaudeTranscriptPath: %w", err)
	}

	// Step 3 — Materialize .claude/settings.json in the worktree (CHB-001..005).
	// For a LOCAL run (rc.runner == nil) this is the byte-identical box-A-local
	// write (NFR7). For a REMOTE run (rc.runner is the worker's SSHRunner) the
	// settings file is written onto the WORKER's filesystem, where the worktree
	// lives — otherwise the worker's claude launches with no hook and times out
	// at agent_ready (hk-z8ek). The hook "command" field is resolved to the
	// WORKER's harmonik path for remote runs (a box-A path would not exist on the
	// worker); falls back to rc.daemonBinaryPath when workerBinaryPath is unset
	// (hk-kqdpf.6: absolute path, never the bare "harmonik" name).
	settingsHookBinary := rc.daemonBinaryPath
	if rc.runner != nil && rc.workerBinaryPath != "" {
		settingsHookBinary = rc.workerBinaryPath
	}
	if err := workspace.MaterializeClaudeSettingsVia(ctx, rc.runner, rc.workspacePath, settingsHookBinary, sessionLogPath); err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: MaterializeClaudeSettings: %w", err)
	}

	// Step 3a — Pre-seed ~/.claude.json with worktree trust (CHB-029 / WM-040b).
	// MUST be after MaterializeClaudeSettings and BEFORE SubstrateSpawn.
	// Failure is a fatal structural error: an un-trusted session blocks indefinitely.
	// REMOTE run (rc.runner != nil): the trust entry is upserted into the WORKER's
	// ~/.claude.json (the worker is where claude reads trust); LOCAL run: unchanged
	// box-A ~/.claude.json write (NFR7) (hk-z8ek).
	if err := workspace.EnsureWorktreeTrustVia(ctx, rc.runner, rc.workspacePath); err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: EnsureWorktreeTrust: %w", err)
	}

	// Step 3a' — Pre-seed ~/.claude.json["theme"] to suppress the first-run
	// theme-selection modal (hk-oga33). Claude Code >= 2.1.214 renders an
	// interactive "Choose the text style …" onboarding modal at Stage 1 (BEFORE
	// SessionStart) when theme is unset; --dangerously-skip-permissions does NOT
	// suppress it (covers only the trust modal), so a daemon-spawned pane wedges on
	// it and agent_ready times out at 150s. Same ordering + local/remote dispatch as
	// the trust seed. Fatal-structural for the same reason: an un-themed session
	// blocks indefinitely on the modal rather than reaching SessionStart.
	//
	// SUPERSEDED for claude:LOCAL by Step 3a'' (CLAUDE_CONFIG_DIR isolation,
	// hk-8juwz): the shared-global theme seed was live-refuted (fleet writers
	// lost-update it; top-level "theme" is not even the modal-gating key). Left in
	// place as a harmless no-op for now (a follow-up can retire it) and still
	// covers the REMOTE path, which is not yet isolated.
	if err := workspace.EnsureClaudeThemeVia(ctx, rc.runner); err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: EnsureClaudeTheme: %w", err)
	}

	// Step 3a'' — Isolate a PRIVATE per-launch Claude config dir on BOTH the LOCAL
	// and REMOTE paths (hk-8juwz local; hk-qxvc2 remote). Provision a private config
	// dir seeded from the ONBOARDED ~/.claude.json of the machine claude runs on
	// (box A for LOCAL, the WORKER for REMOTE) and set CLAUDE_CONFIG_DIR to it below
	// (Step 5). This relocates claude's config reads off the SHARED global
	// ~/.claude.json that concurrent processes race, so the modal-dismissing
	// onboarding state cannot be lost-updated away and claude reaches SessionStart
	// (→ agent_ready) instead of wedging on the first-run onboarding/theme modal.
	// Auth is Keychain-based (machine-global), so relocation does NOT lose auth.
	// Fatal-structural like the trust seed: an un-isolated launch re-wedges on the
	// modal and times out at agent_ready.
	//
	// LOCAL (rc.runner == nil): PrepareIsolatedClaudeConfigDir seeds from box A's
	// ~/.claude.json. REMOTE (rc.runner != nil): PrepareIsolatedClaudeConfigDirVia
	// runs the same preparation ON THE WORKER, seeding from the WORKER's own
	// ~/.claude.json and returning the worker-absolute dir (the value
	// CLAUDE_CONFIG_DIR must carry, since claude reads it on the worker).
	var isolatedClaudeConfigDir string
	if rc.runner == nil {
		isolatedClaudeConfigDir, err = workspace.PrepareIsolatedClaudeConfigDir(rc.workspacePath)
	} else {
		isolatedClaudeConfigDir, err = workspace.PrepareIsolatedClaudeConfigDirVia(ctx, rc.runner, rc.workspacePath)
	}
	if err != nil {
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: PrepareIsolatedClaudeConfigDir: %w", err)
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
	// REMOTE run (rc.runner != nil): write agent-task.md onto the WORKER's
	// worktree; LOCAL run: unchanged box-A-local write (NFR7) (hk-z8ek).
	if err := workspace.WriteAgentTaskVia(ctx, rc.runner, rc.workspacePath, agentTaskPayload); err != nil {
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
		// HarmonikAgent distinguishes this implementer on the keeper bus so the
		// statusLine helper writes impl-<runID>.ctx rather than captain.ctx (hk-4hk).
		HarmonikAgent: "impl-" + core.RunID(rc.runID).String(),
		BaseEnv:       rc.baseEnv,
	}
	env := handler.ClaudeEnvVars(cfg)

	// Step 5a — Export CLAUDE_CONFIG_DIR for BOTH the local and remote paths
	// (hk-8juwz local; hk-qxvc2 remote). claude v2.1.214 reads CLAUDE_CONFIG_DIR to
	// relocate its config directory to <dir>/.claude.json, off the shared global
	// ~/.claude.json. Appended AFTER ClaudeEnvVars so the substrate carries it into
	// the spawned process env (SubstrateSpawn replaces the pane env with this
	// slice). isolatedClaudeConfigDir is now set on BOTH branches (Step 3a''); for a
	// remote run it is the WORKER-absolute isolated dir the SSH launch exports on
	// the worker. Still gated on non-empty so NFR7 holds if a prepare ever returns
	// "" (no-op). CLAUDE_CONFIG_DIR is not on the CHB-007 forbidden env-var list, so
	// the Step 7 guard passes.
	if isolatedClaudeConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+isolatedClaudeConfigDir)
	}

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

// isHarmonikManagedWorktree reports whether workspacePath is an operator-sanctioned
// harmonik worktree, per specs/handler-contract.md §4.10 HC-055b — the positive
// allowlist that gates emission of --dangerously-skip-permissions.
//
// A positive match is returned when EITHER:
//   - (primary) workspacePath canonicalizes (via filepath.EvalSymlinks) to a path
//     under the canonicalized worktreeRootPath, when worktreeRootPath is non-empty
//     and resolvable; OR
//   - (fallback) workspacePath contains a harmonik-managed worktrees path segment
//     (.harmonik/worktrees/ or .harmonik/crew-worktrees/). This covers the case
//     where worktreeRootPath is empty/unthreaded or its canonicalization mismatches
//     the workspace (see the trust-modal fix, hk-5gmkd / HC-056).
//
// If workspacePath's own EvalSymlinks fails (e.g. the dir is not yet created) the
// unresolved path is used for the segment check rather than short-circuiting to
// false. An empty workspacePath always returns false. Note: unlike an earlier
// revision, an empty worktreeRootPath does NOT force false — the segment fallback
// can still match.
func isHarmonikManagedWorktree(workspacePath, worktreeRootPath string) bool {
	if workspacePath == "" {
		return false
	}
	canonWS, err := filepath.EvalSymlinks(workspacePath)
	if err != nil {
		// Fall back to the unresolved path so the segment check below can still
		// match (the worktree dir exists at launch, but be defensive).
		canonWS = workspacePath
	}
	// Primary check: workspacePath canonicalizes under the configured worktree root.
	if worktreeRootPath != "" {
		if canonRoot, rerr := filepath.EvalSymlinks(worktreeRootPath); rerr == nil {
			// Ensure the prefix includes a trailing separator so that a root path
			// that is a prefix of another root path does not produce a false
			// positive. E.g., /foo/bar must not match /foo/barbaz.
			prefix := canonRoot + string(filepath.Separator)
			if strings.HasPrefix(canonWS, prefix) {
				return true
			}
		}
	}
	// Fallback (hk trust-modal fix): any path under a harmonik-managed worktrees
	// directory IS operator-sanctioned, regardless of worktreeRootPath threading or
	// a canonicalization mismatch between the root and the workspace. Without this,
	// the mismatch drops --dangerously-skip-permissions and the bead agent wedges on
	// Claude Code's interactive trust / pre-approved-permissions modal, so
	// SessionStart never fires and the launch times out at agent_ready (HC-056).
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
