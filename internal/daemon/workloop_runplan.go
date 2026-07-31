package daemon

// workloop_runplan.go — the run plan: every decision beadRunOne makes BEFORE it
// takes a resource.
//
// A run plan answers ten questions about one dispatched bead: which workflow
// mode and workflow file, which harness, which model and effort, which Pi
// provider profile, which repository the work happens in, which branches the
// merge gate protects, which commit the worktree is cut from, which branch the
// run lands on, and which branch the merge targets. All ten are settled before
// the run holds a worktree, a tunnel port, an agent process, or an ssh session
// on a worker.
//
// WHY THE SEAM IS HERE. Five decisions can REFUSE the bead: an unknown Pi
// profile, a target repo outside the safelist, a start_from ref that does not
// resolve, a lands_on branch the operator protects, and a daemon hook socket
// path too long for a remote run's tunnel to reach. Before the plan existed the
// first four sat inline in a 1700-line function, above the acquisitions but
// with nothing to keep them there. One of them drifted below a worker-slot
// reservation once already and leaked the slot on every refusal until the
// remote path wedged (hk-3hozm). The fifth was BELOW three acquisitions and was
// moved up here. With the decisions in one function that returns a value, a
// refusal cannot move below an acquisition without moving the whole plan.
//
// THE PLAN IS NOT PURE, AND DOES NOT CLAIM TO BE. It stats and reads
// <repo>/.harmonik/branching.yaml. It forks up to two `git rev-parse`
// subprocesses. Its parent-commit answer is time-varying, because
// refs/heads/<branch> moves as sibling runs merge. It reads HARMONIK_CLAUDE_MODEL
// and HARMONIK_CLAUDE_EFFORT from the process environment at call time, which is
// the documented hot-reload path (hk-c5oxy). It emits bead_label_conflict,
// review_bypassed and provider_selected as the tier walks find conflicts. The
// property this file defends is DECIDED BEFORE ANY RESOURCE IS ACQUIRED, not
// purity, and the two are not the same thing.
//
// THE RUN-LEVEL (harness, model, effort) TUPLE IS NOT THE LAST WORD. A DOT run
// recomputes an effective harness per node and re-derives the node's model from
// it (dot_cascade_core.go, and nodeModelForHarness in dot_cascade_helpers.go).
// The plan carries the RUN-level answer. Do not treat it as the node-level
// answer and do not fold the per-node correction into it — that reopens the
// model-leak defect on the node axis (hk-pkugu).
//
// THE PLAN DOES NOT PLACE THE RUN. It carries placement INTENT — LocalOnly and
// WorkerTarget, straight off the dispatched queue item — but it never selects
// and never reserves a worker. workers.SelectWorker and SelectWorkerByName
// decide and reserve inside one mutex-held critical section on purpose. Asking
// the plan to pick a worker would split that section and re-open the race it
// closes.
//
// KNOWN INCONSISTENCY, RECORDED NOT FIXED. The plan refusals reopen the bead
// and emit NO run_failed. The socket-path one emits worker_tunnel_failed, and
// the other four emit nothing at all. Refusals further down the run path ride
// the Run bridge and DO emit run_failed. That split is real and is not defended
// by any spec rule. It is left alone here on purpose: unifying it would ADD
// events to the stream that operator tooling does not expect today.
//
// Spec refs: specs/execution-model.md §4.3 EM-012a (workflow mode / ref),
//
//	EM-012b (model / effort). specs/workspace-model.md §4.2 WM-005b
//	covers start_from and lands_on. specs/beads-integration.md §4.3
//	BI-009b covers the ## Branching parse contract.
//
// Bead refs: hk-3hozm (slot leak the seam makes unrepresentable), hk-ncwb3
// (lands_on protection), hk-xfuc (cross-repo safelist), hk-m6uu2 (Pi profile),
// hk-pkugu (harness-matched model default), hk-mtm0w (lands_on rebase base),
// hk-lgykq (per-bead merge target).

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/workers"
)

// runPlanRequest names everything the resolver reads.
//
// Env supplies the immutable per-run values (RSM-010) — the bead record, the
// daemon and queue defaults, and the per-item overrides. Emit is the run's
// event emitter, which the tier walks use to report label conflicts. Handles is
// present for ONE reason: a Pi run stamps its resolved provider onto the run
// handle at the same point it emits provider_selected, and both must keep their
// position in the sequence.
//
// PreSelectedWorker is the worker the OUTER dispatch loop already reserved for
// this run, or nil. The plan reads it, and reads nothing else about placement.
// It does not select and does not reserve. It is here so that the one check
// which only matters for a remote run can refuse before the run takes a tunnel
// port or touches the worker over ssh.
type runPlanRequest struct {
	Env               runloop.RunEnv
	Emit              handlercontract.EventEmitter
	Handles           runloop.SharedHandles
	PreSelectedWorker *workers.Worker
}

// runPlanVerdict tells beadRunOne whether the bead may proceed, and when it may
// not, which decision refused it.
type runPlanVerdict string

const (
	// runPlanReady — all ten decisions resolved. The run may acquire resources.
	runPlanReady runPlanVerdict = "ready"

	// runPlanRefusedPiProfile — the bead names a Pi profile that is absent from
	// harnesses.pi.profiles. Launching would ask a provider for a model it does
	// not serve.
	runPlanRefusedPiProfile runPlanVerdict = "refused_pi_profile"

	// runPlanRefusedCrossRepoUnsafe — the bead declares a target_repo that is
	// not in the daemon's allowed_repos safelist.
	runPlanRefusedCrossRepoUnsafe runPlanVerdict = "refused_cross_repo_unsafe"

	// runPlanRefusedStartFrom — the bead's branching config is malformed, or its
	// start_from names a ref that does not resolve in the active repository.
	runPlanRefusedStartFrom runPlanVerdict = "refused_start_from"

	// runPlanRefusedLandsOnProtected — the resolved lands_on is a branch the
	// operator declared off-limits for direct pushes.
	runPlanRefusedLandsOnProtected runPlanVerdict = "refused_lands_on_protected"

	// runPlanRefusedSocketPath — the daemon's hook socket path is too long for
	// the platform to bind or connect, so the reverse tunnel this remote run
	// needs could never carry a hook. Remote runs only.
	runPlanRefusedSocketPath runPlanVerdict = "refused_socket_path"
)

// runPlanRefusal carries exactly what the daemon must say when it refuses a
// bead. The strings are the refusal's whole report: one stderr line and one
// ReopenBead reason. They are data, not a template, so that the report cannot
// drift when the refusal moves.
type runPlanRefusal struct {
	// LogLine is the complete stderr line, newline included.
	LogLine string

	// ReopenReason is the text handed to ReopenBead.
	ReopenReason string

	// Err is the typed error the refusal came from, for callers and tests that
	// want errors.As rather than a string match.
	Err error

	// TunnelFailure, when set, is the worker_tunnel_failed event this refusal
	// emits BETWEEN the stderr line and the reopen. Only the socket-path
	// refusal sets it. It is data rather than a callback so that the order of
	// the three reports stays fixed in one place.
	TunnelFailure *runPlanTunnelFailure
}

// runPlanTunnelFailure is the worker_tunnel_failed payload a refusal owes an
// operator: which run, which bead, which worker, which socket path, and why.
type runPlanTunnelFailure struct {
	RunID      string
	BeadID     string
	WorkerName string
	WorkerHost string
	SocketPath string
	Detail     string
}

// runPlan is the answer to all ten questions plus the verdict.
//
// Every field is fixed for the run's lifetime EXCEPT as noted on AgentType,
// Model and Effort: a DOT run corrects those per node.
type runPlan struct {
	Verdict runPlanVerdict

	// Refusal is populated only when Verdict is not runPlanReady.
	Refusal runPlanRefusal

	// WorkflowMode is the EM-012a mode, sealed for the run's lifetime.
	WorkflowMode core.WorkflowMode

	// WorkflowRef is the resolved .dot workflow file, or "" to fall through to
	// the project workflow.dot or the embedded standard-bead.dot. This is the
	// RESOLVED value. The per-item value on the request env is a tier-0 input to
	// it and must not be read on the run path.
	WorkflowRef string

	// AgentType, Model and Effort are the RUN-level harness tuple. A DOT run
	// recomputes an effective harness per node and re-derives the node model
	// from it. These three are the run default that per-node resolution starts
	// from, not the final answer for a node.
	AgentType core.AgentType
	Model     string
	Effort    string

	// PiProfile is the resolved Pi provider profile, zero for every non-Pi run
	// and for a Pi run with no profile: label.
	PiProfile projectconfig.PiProfileConfig

	// ActiveRepo is the repository the worktree lives in, commits happen in, and
	// merges push from. It is the project dir for a local bead and the declared
	// target_repo for a cross-repo bead.
	ActiveRepo string

	// MergeProtectBranches is the protect-branch list the merge gate enforces.
	// It is nil for a cross-repo run: the daemon's list names the harmonik
	// project's branches and says nothing about the target repo's.
	MergeProtectBranches []string

	// Branching is the whole branching decision, resolved once: start_from,
	// lands_on, landing strategy and target repo.
	Branching BranchingConfig

	// ParentSHA is the commit the worktree is cut from — start_from resolved
	// against ActiveRepo. Time-varying by nature: it is a branch tip, and
	// sibling runs move branch tips.
	ParentSHA string

	// BaseBranch is the resolved lands_on: the branch the run rebases onto
	// before exit.
	BaseBranch string

	// MergeTarget is the branch the merge lands on — BaseBranch, falling back to
	// the daemon-wide target so the merge is never aimed at an empty ref.
	MergeTarget string

	// LocalOnly and WorkerTarget are placement INTENT off the dispatched queue
	// item. The plan carries them and does not act on them: it selects no worker
	// and reserves no slot.
	LocalOnly    bool
	WorkerTarget string
}

// resolveRunPlan walks the ten decisions in dependency order and returns the
// plan.
//
// Order is load-bearing twice over. The harness must resolve before the model,
// or the model default leaks across harnesses (hk-pkugu). The active repo must
// resolve before the branching config, because the branching config is read out
// of the active repo.
//
// It acquires nothing. It never launches, never claims a bead, never selects a
// worker, and never creates a worktree. On refusal the caller performs the
// report — see refuseRunPlan.
func resolveRunPlan(ctx context.Context, req runPlanRequest) runPlan {
	env := req.Env
	bead := env.BeadRecord
	emit := req.Emit

	plan := runPlan{
		Verdict:      runPlanReady,
		LocalOnly:    env.ItemLocalOnly,
		WorkerTarget: env.ItemWorkerTarget,
	}

	// ── 1. Workflow mode (EM-012a) ─────────────────────────────────────────
	//
	// Four-tier precedence: per-bead label → project config (no-op) → daemon
	// default → dot (hk-30vlb). The per-item override is tier 0: the CLI
	// --review-loop flag writes it onto the queue item, and when it holds a
	// valid mode it wins over the whole walk (hk-hiqrl).
	plan.WorkflowMode = resolveWorkflowMode(ctx, bead, env.WorkflowModeDefault, emit)
	if env.ItemWorkflowMode != "" {
		if candidate := core.WorkflowMode(env.ItemWorkflowMode); candidate.Valid() {
			plan.WorkflowMode = candidate
		}
	}

	// ── 2. Workflow ref (EM-012a) ──────────────────────────────────────────
	//
	// Per-item ref (tier 0, hk-qo9pq) beats the per-bead dot:<name> label (tier
	// 1, hk-30q6). Absence falls through to the project workflow.dot or the
	// embedded standard-bead.dot.
	plan.WorkflowRef = resolveWorkflowRef(bead, env.ItemWorkflowRef)

	// Decisions 3 to 5 answer WHAT runs the bead, decisions 6 to 10 answer
	// WHERE. Each returns false when it refuses, having filled plan.Refusal.
	if !resolveRunPlanHarness(ctx, req, &plan) {
		return plan
	}
	if !resolveRunPlanPlace(ctx, req, &plan) {
		return plan
	}
	resolveRunPlanHookSocket(req, &plan)
	return plan
}

// resolveRunPlanHarness answers what runs the bead: the harness, the model and
// effort, and the Pi provider profile. It fills plan and reports false when the
// bead is refused.
//
// The order inside is load-bearing: the harness resolves first so the compiled
// model default belongs to the harness that will actually run (hk-pkugu), and
// the profile resolves after the harness so a claude- or codex-resolved bead
// yields the zero tuple.
func resolveRunPlanHarness(ctx context.Context, req runPlanRequest, plan *runPlan) bool {
	env := req.Env
	bead := env.BeadRecord
	beadID := bead.BeadID
	emit := req.Emit

	// ── 3. Harness agent type ──────────────────────────────────────────────
	//
	// Resolved QUIETLY — no events. The launch path resolves the same tuple
	// again and emits harness_selected there. Emitting here would double it.
	// It must run before the model walk so the model default belongs to the
	// harness that will actually run (hk-pkugu).
	plan.AgentType = resolveHarnessAgentTypeQuiet(
		bead,
		env.QueueDefaultHarness,
		core.AgentType(""), // node default: a DOT run overrides this per node
		env.DefaultHarness,
	)

	// ── 4. Model and effort (EM-012b) ──────────────────────────────────────
	//
	// Four tiers plus a tier-2.5 env-var read. That read happens HERE, at plan
	// time, not at daemon start: an operator exports HARMONIK_CLAUDE_MODEL and
	// the next dispatch picks it up without a daemon restart (hk-c5oxy).
	plan.Model, plan.Effort = ResolveModelPreference(
		ctx,
		bead.Labels,
		plan.AgentType,
		env.ProjectCfg,
		emit,
		string(beadID),
	)

	// ── 5. Pi provider profile ─────────────────────────────────────────────
	//
	// Runs strictly after the harness, so a claude- or codex-resolved bead
	// yields the zero tuple. An unknown profile reference is fail-loud.
	profile, profErr := resolvePiProfile(
		ctx, bead.Labels, plan.AgentType,
		env.ProjectCfg.Harnesses.Pi, emit, string(beadID),
	)
	if profErr != nil {
		plan.Verdict = runPlanRefusedPiProfile
		plan.Refusal = runPlanRefusal{
			LogLine:      fmt.Sprintf("daemon: workloop: bead %s refused: %v (reopening)\n", beadID, profErr),
			ReopenReason: profErr.Error(),
			Err:          profErr,
		}
		return false
	}
	plan.PiProfile = profile

	// Locked precedence (C3-spec.md §2): the wire-format triple and the
	// credentials arrive together from the profile and are never split. A
	// model: label overrides ONLY the model field, and only when there is
	// exactly one of them.
	if profile != (projectconfig.PiProfileConfig{}) && !hasSingleModelLabel(bead.Labels) {
		plan.Model = profile.Model
	}

	// Carry the resolved Pi provider identity onto the run handle and report it
	// (hk-8ziid.2). Pi runs only: a matched profile's provider wins, else the
	// harness-global default. A non-Pi run leaves the handle unset, which is how
	// a reader tells "not yet resolved" from "resolved to the empty default".
	if plan.AgentType == core.AgentTypePi {
		provider := profile.Provider
		if profile == (projectconfig.PiProfileConfig{}) {
			provider = env.ProjectCfg.Harnesses.Pi.Provider
		}
		if rh, ok := req.Handles.RunRegistry.Get(env.RunID); ok && rh != nil {
			rh.SetResolvedProvider(provider)
		}
		emitProviderSelected(ctx, emit, env.RunID, provider)
	}
	return true
}

// resolveRunPlanPlace answers where the bead runs: the active repository, the
// protect-branch list the merge gate enforces, the parent commit, the branch it
// lands on, and the branch the merge targets. It fills plan and reports false
// when the bead is refused.
//
// The active repo must resolve first: the branching config is read out of it.
func resolveRunPlanPlace(ctx context.Context, req runPlanRequest, plan *runPlan) bool {
	env := req.Env
	bead := env.BeadRecord
	beadID := bead.BeadID

	// ── The bead body is parsed ONCE, here ─────────────────────────────────
	//
	// Decision 6 needs target_repo out of the ## Branching section to pick the
	// active repo, and decisions 8 and 9 need the rest of that same section
	// resolved against the repo decision 6 picks. One parse feeds both.
	//
	// A malformed section is treated as absent per BI-009b and warned once. The
	// pre-plan run path parsed this body three times and warned twice for the
	// same malformed section. One parse and one warning report the same fact.
	beadBranchCfg, parseErr := parseBranchingSection(bead.Description)
	if parseErr != nil {
		warnBeadBodyParseError(ctx, parseErr)
	}

	// ── 6. Active repo (hk-xfuc) ───────────────────────────────────────────
	//
	// The active repo is where the worktree lives, commits happen, and merges
	// push from. A local bead uses the project dir. A cross-repo bead declares
	// target_repo and must pass the allowed_repos safelist first, or an
	// arbitrary path reaches the git commands below.
	//
	// The project dir stays the harmonik project root for everything that is
	// not git: the daemon socket, queue files, the beads adapter, workflow.dot.
	plan.ActiveRepo = env.ProjectDir
	if beadBranchCfg.TargetRepo != "" && beadBranchCfg.TargetRepo != env.ProjectDir {
		if !isInAllowedRepos(beadBranchCfg.TargetRepo, env.AllowedRepos) {
			crErr := &CrossRepoUnsafeError{TargetRepo: beadBranchCfg.TargetRepo, ProjectDir: env.ProjectDir}
			plan.Verdict = runPlanRefusedCrossRepoUnsafe
			plan.Refusal = runPlanRefusal{
				LogLine:      fmt.Sprintf("daemon: workloop: bead %s refused: %v (reopening)\n", beadID, crErr),
				ReopenReason: crErr.Error(),
				Err:          crErr,
			}
			return false
		}
		plan.ActiveRepo = beadBranchCfg.TargetRepo
		slog.InfoContext(ctx, "cross_repo_dispatch",
			"bead_id", string(beadID),
			"active_repo", plan.ActiveRepo,
			"project_dir", env.ProjectDir,
		)
	}

	// ── 7. Effective protect-branches ──────────────────────────────────────
	//
	// The daemon's protect list guards harmonik's own branches. On a cross-repo
	// run it says nothing about the target repo, so pass nil rather than refuse
	// a legitimate branch there that happens to share a name with a protected
	// harmonik branch.
	plan.MergeProtectBranches = env.ProtectBranches
	if plan.ActiveRepo != env.ProjectDir {
		plan.MergeProtectBranches = nil
	}

	// ── 8 and 9. Branching, resolved in ONE pass ───────────────────────────
	//
	// This settles start_from, lands_on, the landing strategy and the parent
	// commit together. The pre-plan run path resolved the same three tiers
	// TWICE — once inside the parent-commit call, which threw lands_on away, and
	// again purely to get lands_on back. Both calls read the same
	// mtime-cached .harmonik/branching.yaml, so a write between them made the
	// two answers disagree.
	//
	// FAILURE SEMANTICS. The two old calls failed differently, and the
	// difference is preserved rather than averaged:
	//
	//   - The first call failing was FATAL: reopen the bead and return. That is
	//     this refusal, with the same reason text.
	//   - The second call failing was NON-FATAL: lands_on stayed empty and the
	//     whole protect check was skipped.
	//
	// The second call was reachable ONLY when the first had already succeeded,
	// which means only inside the disagreement window. One call has no window,
	// so the non-fatal branch has no way to arise. The one behaviour this
	// changes is that a bead landing on a protected branch can no longer slip
	// past the protect check by racing a config write — it is now always
	// checked. That is the safe direction of the two.
	branch, branchErr := resolveBranchPlan(ctx, plan.ActiveRepo, string(beadID), beadBranchCfg, env.TargetBranch)
	if branchErr != nil {
		plan.Verdict = runPlanRefusedStartFrom
		plan.Refusal = runPlanRefusal{
			LogLine:      fmt.Sprintf("daemon: workloop: resolveParentCommit for bead %s: %v (reopening)\n", beadID, branchErr),
			ReopenReason: fmt.Sprintf("resolve start_from failed: %v", branchErr),
			Err:          branchErr,
		}
		return false
	}
	plan.Branching = branch.Config
	plan.ParentSHA = branch.ParentSHA
	plan.BaseBranch = branch.Config.LandsOn

	// Protection gate (hk-ncwb3): a bead may NARROW its landing target but must
	// never widen it to a branch the operator protects. Cross-repo runs skip the
	// check for the same reason decision 7 nils the list.
	if plan.ActiveRepo == env.ProjectDir {
		for _, protected := range env.ProtectBranches {
			if plan.BaseBranch == protected {
				protErr := &LandsOnProtectedError{LandsOn: plan.BaseBranch}
				plan.Verdict = runPlanRefusedLandsOnProtected
				plan.Refusal = runPlanRefusal{
					LogLine:      fmt.Sprintf("daemon: workloop: bead %s refused: %v (reopening)\n", beadID, protErr),
					ReopenReason: protErr.Error(),
					Err:          protErr,
				}
				return false
			}
		}
	}

	// ── 10. Merge target (hk-lgykq) ────────────────────────────────────────
	//
	// The run branch must land on the branch it was rebased onto, not the
	// daemon-wide default. lands_on already carries the three-tier answer and
	// equals the daemon target when the bead declares no override. The fallback
	// stands as a guard: the merge must never be aimed at an empty ref, because
	// the merge fail-closes on one.
	plan.MergeTarget = plan.BaseBranch
	if plan.MergeTarget == "" {
		plan.MergeTarget = env.TargetBranch
	}

	return true
}

// resolveRunPlanHookSocket refuses a remote run whose daemon hook socket path is
// too long for the platform to bind or connect.
//
// The check is a length comparison against a platform constant and depends only
// on the project dir, so it can be answered here. It used to run much later, in
// the tunnel setup, AFTER the run had already reserved a worker slot, allocated
// a tunnel port, and made an ssh round trip to create a directory on the
// worker. All three were taken for a run that could never work.
//
// It stays remote-only, and it stays LAST among the refusals, so that a bead
// which would also fail an earlier decision still reports that earlier reason.
//
// The gate is a pre-selected worker rather than "this run is remote", because
// remote is not yet decided for a run whose worker the fallback path will pick.
// The tunnel setup keeps its own copy of the check for that path. The two never
// both refuse: a pre-selected run that fails here returns before the tunnel
// setup runs at all.
//
// ssh never validates this forward destination when the tunnel starts, only
// when a connection needs forwarding, so a too-long path lets the tunnel come
// up and the readiness probe pass, then swallows every hook connection in
// silence. The run surfaces it much later as an unexplained agent-ready
// timeout. Fail loud instead (hk-ta6dg).
func resolveRunPlanHookSocket(req runPlanRequest, plan *runPlan) {
	if req.PreSelectedWorker == nil {
		return
	}
	env := req.Env
	beadID := env.BeadRecord.BeadID
	sockPath := filepath.Join(env.ProjectDir, ".harmonik", "daemon.sock")
	lenErr := lifecycle.ValidateSocketPathLength(sockPath)
	if lenErr == nil {
		return
	}
	plan.Verdict = runPlanRefusedSocketPath
	plan.Refusal = runPlanRefusal{
		LogLine: fmt.Sprintf(
			"daemon: workloop: reverse-tunnel socket-path bead %s run %s: %v (reopening, not launching)\n",
			beadID, env.RunID.String(), lenErr),
		ReopenReason: fmt.Sprintf("reverse-tunnel not ready: %v", lenErr),
		Err:          lenErr,
		TunnelFailure: &runPlanTunnelFailure{
			RunID:      env.RunID.String(),
			BeadID:     string(beadID),
			WorkerName: req.PreSelectedWorker.Name,
			WorkerHost: req.PreSelectedWorker.Host,
			SocketPath: sockPath,
			Detail:     lenErr.Error(),
		},
	}
}

// refuseRunPlan reports one plan refusal and reopens the bead.
//
// The report is the stderr line the plan built, then the refusal's own event
// when it has one, then a best-effort ReopenBead with the plan's reason. That
// order is the order the daemon used before the plan, and it lives here so it
// is stated once for every refusal.
//
// The reopen is best-effort by design: when it fails the bead stays in_progress
// and an operator reopens it by hand, which is preferable to the daemon
// spinning on a bead it has already refused (hk-s20z).
//
// No run_failed is emitted. See this file's header: that omission is the
// pre-plan behaviour, preserved on purpose.
func refuseRunPlan(ctx context.Context, env runloop.RunEnv, handles runloop.SharedHandles, emit handlercontract.EventEmitter, refusal runPlanRefusal) {
	fmt.Fprint(os.Stderr, refusal.LogLine)
	if tf := refusal.TunnelFailure; tf != nil {
		workers.EmitWorkerTunnelFailedEvent(ctx, tf.RunID, tf.BeadID,
			tf.WorkerName, tf.WorkerHost, tf.SocketPath, tf.Detail, emit.Emit)
	}
	reopenTID, _ := handles.TIDGen.Next()                                     //nolint:errcheck // a failed TID still reopens; the reopen is the report that matters
	_ = handles.BrAdapter.ReopenBead(ctx, env.IntentLogDir, env.BrTimeoutCfg, //nolint:errcheck // best-effort reopen; on failure the bead stays in_progress for manual reopen (hk-s20z)
		env.RunID, reopenTID, env.BeadRecord.BeadID, refusal.ReopenReason)
}
