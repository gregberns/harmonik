package claude

// launchspec.go — BuildLaunchSpec helper (hk-gql20.13).
//
// The eleven error prefixes below still read "daemon: buildClaudeLaunchSpec:".
// That is deliberate, not an oversight: P2 unit E1b relocated this file out of
// internal/daemon and the extraction is a PURE MOVE, so every observable string
// is byte-identical to the daemon-side original
// (plans/2026-07-21-p2-extraction/E1b-claude.md §3a, R10). No test asserts on
// them. A prefix-hygiene sweep across the extracted harness packages is a
// follow-up, not part of a relocation.
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
//     Appends --model and --effort when shared.LaunchCtx fields are non-empty (HC-055a).
//   - CheckForbiddenFlags — deny-list guard (CHB-007).
//   - PreExecMessages — 4 ordered pre-exec progress messages (CHB-018).
//
// The helper is twin-blind: the same code path is used whether Binary points to
// "claude" or "harmonik-twin-claude". The Binary field of the returned
// handler.LaunchSpec is opaque to this helper — the caller sets it from
// shared.LaunchCtx.HandlerBinary.
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

	// Step 1 — MintClaudeSessionID (CHB-008, CHB-009).
	mintRes, err := handler.MintClaudeSessionID(string(rc.Phase), rc.PriorClaudeSessID)
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: MintClaudeSessionID: %w", err)
	}

	// Step 2 — Derive Claude transcript path (CHB-018 step 2).
	sessionLogPath, err := handler.DeriveClaudeTranscriptPath(rc.WorkspacePath, mintRes.ClaudeSessionID)
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
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
	settingsHookBinary := rc.DaemonBinaryPath
	if rc.Runner != nil && rc.WorkerBinaryPath != "" {
		settingsHookBinary = rc.WorkerBinaryPath
	}
	if err := workspace.MaterializeClaudeSettingsVia(ctx, rc.Runner, rc.WorkspacePath, settingsHookBinary, sessionLogPath); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: MaterializeClaudeSettings: %w", err)
	}

	// Step 3a — Pre-seed ~/.claude.json with worktree trust (CHB-029 / WM-040b).
	// MUST be after MaterializeClaudeSettings and BEFORE SubstrateSpawn.
	// Failure is a fatal structural error: an un-trusted session blocks indefinitely.
	// REMOTE run (rc.runner != nil): the trust entry is upserted into the WORKER's
	// ~/.claude.json (the worker is where claude reads trust); LOCAL run: unchanged
	// box-A ~/.claude.json write (NFR7) (hk-z8ek).
	if err := workspace.EnsureWorktreeTrustVia(ctx, rc.Runner, rc.WorkspacePath); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
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
	// The seed itself was live-refuted as a modal fix (fleet writers lost-update the
	// shared file; top-level "theme" is not even the modal-gating key), and the modal
	// no longer reproduces on claude v2.1.217 with the operator's normal shared
	// config (see Step 3a''). It is left in place, but it is NOT inert: whenever
	// top-level "theme" is absent, EnsureClaudeTheme takes an exclusive flock and
	// read-modify-writes the operator's real shared ~/.claude.json (see
	// ensureClaudeThemeAt). Since fleet writers can lost-update that key away, this
	// can re-fire across launches. Retiring it is a follow-up; do not describe it as
	// a no-op.
	if err := workspace.EnsureClaudeThemeVia(ctx, rc.Runner); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: EnsureClaudeTheme: %w", err)
	}

	// Step 3a'' — Isolate a PRIVATE per-launch Claude config dir. REMOTE ONLY
	// (rc.runner != nil, hk-qxvc2): PrepareIsolatedClaudeConfigDirVia runs the
	// preparation ON THE WORKER, seeding from the WORKER's own onboarded
	// ~/.claude.json and returning the worker-absolute dir (the value
	// CLAUDE_CONFIG_DIR must carry below in Step 5a, since claude reads it on the
	// worker). Fatal-structural like the trust seed: a failed prepare must not exec
	// claude.
	//
	// LOCAL: DELIBERATELY NOT ISOLATED — do not re-add (hk-8juwz). The local
	// isolation was tried (a964cbcb) and LIVE-REFUTED by an A/B on one daemon with
	// one line toggled: isolation ON → agent_ready_timeout at 150s with the pane
	// parked on the Bypass Permissions modal; isolation OFF → agent_ready in 2.0s,
	// work committed, run completed. Two defects, both fatal. (1) Relocating
	// CLAUDE_CONFIG_DIR moves the WHOLE ~/.claude surface, but only .claude.json was
	// seeded — ~/.claude/settings.json (and its skipDangerousModePermissionPrompt)
	// was dropped, so --dangerously-skip-permissions parked on the bypass modal
	// pre-SessionStart. (2) With the config dir relocated claude reports "Not logged
	// in · Please run /login" and can do NO work; the commit's premise that
	// Keychain-based auth survives relocation is refuted. The onboarding modal the
	// isolation was written to fix no longer reproduces on claude v2.1.217 with the
	// operator's normal shared ~/.claude. A local launch therefore inherits the
	// operator's real config and sets no CLAUDE_CONFIG_DIR at all.
	var isolatedClaudeConfigDir string
	if rc.Runner != nil {
		isolatedClaudeConfigDir, err = workspace.PrepareIsolatedClaudeConfigDirVia(ctx, rc.Runner, rc.WorkspacePath)
		if err != nil {
			return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
				"daemon: buildClaudeLaunchSpec: PrepareIsolatedClaudeConfigDirVia: %w", err)
		}
	}

	// Step 3b — Write per-launch task artifact (CHB-028).
	// MUST be after MaterializeClaudeSettings + EnsureWorktreeTrust and BEFORE SubstrateSpawn.
	// The file carries the bead description for the phase. When rc.beadDescription is empty
	// or whitespace-only (e.g. bead has no body, or --body " "), use the bead title so the
	// file is never structurally empty (hk-lpbu7: TrimSpace closes the whitespace-body livelock
	// where a " " description was non-empty at this layer but rejected by WriteAgentTask).
	taskBody := rc.BeadDescription
	// When the DOT node carries an inline prompt= and the phase is implementer,
	// replace the bead-derived body with the prompt verbatim (WG-040 §I.3,
	// HC-006a §III.3). Bead Title + ID remain in the header for traceability.
	// Reviewer phase: nodePrompt is accepted-but-inert (EM-015d-RIA).
	if rc.NodePrompt != "" && rc.Phase != handlercontract.ReviewLoopPhaseReviewer {
		taskBody = rc.NodePrompt
	}
	if strings.TrimSpace(taskBody) == "" {
		taskBody = rc.BeadTitle
	}
	if taskBody == "" {
		// Last resort: use the bead ID so CHB-028's non-empty invariant is always satisfied.
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
	}
	// REMOTE run (rc.runner != nil): write agent-task.md onto the WORKER's
	// worktree; LOCAL run: unchanged box-A-local write (NFR7) (hk-z8ek).
	if err := workspace.WriteAgentTaskVia(ctx, rc.Runner, rc.WorkspacePath, agentTaskPayload); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: WriteAgentTask: %w", err)
	}

	// Step 4 — Fail-fast if settings.local.json shadows bridge hooks (CHB-024).
	if err := handler.CheckSettingsLocalJSON(rc.WorkspacePath); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: CheckSettingsLocalJSON: %w", err)
	}

	// Step 5 — Build ClaudeEnvConfig and derive the CHB-006 env slice.
	handlerSessUID, err := uuid.NewV7()
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: mint handlerSessionID UUIDv7: %w", err)
	}
	handlerSessionID := handlerSessUID.String()

	// WorkflowID and NodeID: at MVH the bead is the workflow unit, so we
	// synthesise "bead/<beadID>" as the node identifier. WorkflowID reuses the
	// runID's UUID (run is the workflow scope at MVH).
	//
	// TODO(hk-gql20.x): replace with typed WorkflowID / NodeID from a workflow
	// registry once multi-node workflows are introduced.
	nodeID := "bead/" + rc.BeadID
	workflowID := core.WorkflowID(rc.RunID)

	// Build optional ClaudeEnvConfig fields.
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

	// Step 5a — Export CLAUDE_CONFIG_DIR for the REMOTE path only (hk-qxvc2). claude
	// v2.1.214 reads CLAUDE_CONFIG_DIR to relocate its config directory to
	// <dir>/.claude.json, off the shared global ~/.claude.json. Appended AFTER
	// ClaudeEnvVars so the substrate carries it into the spawned process env
	// (SubstrateSpawn replaces the pane env with this slice). isolatedClaudeConfigDir
	// is set ONLY on the remote branch (Step 3a''), where it is the WORKER-absolute
	// isolated dir; on a LOCAL run it stays "" and this is a no-op, so claude
	// inherits the operator's real ~/.claude — the configuration proven green
	// (hk-8juwz; see Step 3a''). CLAUDE_CONFIG_DIR is not on the CHB-007 forbidden
	// env-var list, so the Step 7 guard passes.
	if isolatedClaudeConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+isolatedClaudeConfigDir)
	}

	// Step 6 — Validate ModelPreference fields (HC-055a) before argv construction.
	// Invalid model or effort → typed *ModelPreferenceError; do NOT silently drop.
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
	if rc.Model != "" {
		args = append(args, "--model", rc.Model)
	}
	if rc.Effort != "" {
		args = append(args, "--effort", rc.Effort)
	}
	// HC-055b: emit --dangerously-skip-permissions iff workspacePath canonicalizes
	// to a path under the harmonik worktrees root. This suppresses the interactive
	// trust dialog in operator-daemon launches where the worktree is already
	// operator-sanctioned. The path check is a positive-allowlist match; if
	// EvalSymlinks fails for either path the flag is silently omitted.
	if isHarmonikManagedWorktree(rc.WorkspacePath, rc.WorktreeRootPath) {
		args = append(args, "--dangerously-skip-permissions")
	}

	// Step 7 — Deny-list guard (CHB-007).
	if err := handler.CheckForbiddenFlags(args, env); err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
			"daemon: buildClaudeLaunchSpec: CheckForbiddenFlags: %w", err)
	}

	// Step 8 — Render pre-exec messages (CHB-018).
	runIDStr := rc.RunID.String()
	rawMsgs, err := handler.PreExecMessages(
		runIDStr,
		handlerSessionID,
		nodeID,
		mintRes.ClaudeSessionID,
		sessionLogPath,
		nil, // skills = nil at MVH per design §1 step 9
	)
	if err != nil {
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf(
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
