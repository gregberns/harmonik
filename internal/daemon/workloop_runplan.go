package daemon

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
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

type runPlanRequest struct {
	Env               runloop.RunEnv
	Emit              handlercontract.EventEmitter
	Handles           runloop.SharedHandles
	PreSelectedWorker *workers.Worker
}

type runPlanVerdict string

const (
	runPlanReady runPlanVerdict = "ready"

	runPlanRefusedPiProfile runPlanVerdict = "refused_pi_profile"

	runPlanRefusedCrossRepoUnsafe runPlanVerdict = "refused_cross_repo_unsafe"

	runPlanRefusedStartFrom runPlanVerdict = "refused_start_from"

	runPlanRefusedLandsOnProtected runPlanVerdict = "refused_lands_on_protected"

	runPlanRefusedSocketPath runPlanVerdict = "refused_socket_path"
)

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

type runPlanTunnelFailure struct {
	RunID      string
	BeadID     string
	WorkerName string
	WorkerHost string
	SocketPath string
	Detail     string
}

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

	// Workflow is the complete graph selection used for event emission and DOT
	// execution. WorkflowMode and WorkflowRef mirror its resolved identity.
	Workflow resolvedWorkflow

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

type resolvedWorkflow struct {
	Graph           *dot.Graph
	Descriptor      core.WorkflowDescriptor
	Mode            core.WorkflowMode
	ReviewPolicy    core.ReviewPolicy
	SelectionSource core.WorkflowSelectionSource
	WorkflowRef     string
	RawQueueMode    string
	RawQueueRef     string
}

func (w resolvedWorkflow) Valid() bool {
	if w.Graph == nil || !w.Descriptor.Valid() || w.Mode != core.WorkflowModeDot || !w.ReviewPolicy.Valid() || !w.SelectionSource.Valid() {
		return false
	}
	return core.ValidPolicyBinding(w.Descriptor, w.ReviewPolicy, w.SelectionSource)
}

func resolveWorkflow(ctx context.Context, env runloop.RunEnv, emit handlercontract.EventEmitter) (resolvedWorkflow, error) {
	input := env.ItemWorkflow
	mode := resolveWorkflowModeWithAudit(ctx, env.BeadRecord, env.WorkflowModeDefault, emit, false)
	if candidate := core.WorkflowMode(input.Mode); candidate.Valid() {
		mode = candidate
	}

	if input.Mode == string(core.WorkflowModeSingle) {
		return resolveNoReviewWorkflow(input, core.WorkflowSelectionQueueItemSingleMode)
	}
	if mode == core.WorkflowModeSingle && hasExactWorkflowSingleLabel(env.BeadRecord.Labels) {
		emitReviewBypassed(ctx, emit, env.BeadRecord, workflowLabelPrefix+string(core.WorkflowModeSingle))
		return resolveNoReviewWorkflow(input, core.WorkflowSelectionLegacySingleLabel)
	}

	workflowMode := core.WorkflowModeDot
	workflowRef := resolveWorkflowRef(env.BeadRecord, input.Ref)
	graph, source, err := loadResolvedWorkflowGraph(env.ProjectDir, workflowRef, env.ItemTemplateParams)
	if err != nil {
		return resolvedWorkflow{}, err
	}
	if err := rejectGraphAuthoredReviewPolicy(graph); err != nil {
		return resolvedWorkflow{}, err
	}
	resolved := resolvedWorkflow{
		Graph:           graph,
		Descriptor:      graphDescriptor(graph),
		Mode:            workflowMode,
		ReviewPolicy:    core.ReviewPolicyReviewed,
		SelectionSource: source,
		WorkflowRef:     workflowRef,
		RawQueueMode:    input.Mode,
		RawQueueRef:     input.Ref,
	}
	if !resolved.Valid() {
		return resolvedWorkflow{}, fmt.Errorf("resolved workflow has invalid descriptor or policy")
	}
	return resolved, nil
}

func resolveNoReviewWorkflow(input runloop.QueueWorkflowInput, source core.WorkflowSelectionSource) (resolvedWorkflow, error) {
	graph, err := loadRegisteredEmbeddedGraph(noReviewBeadDescriptor, nil)
	if err != nil {
		return resolvedWorkflow{}, fmt.Errorf("load registered no-review graph: %w", err)
	}
	resolved := resolvedWorkflow{
		Graph:           graph,
		Descriptor:      noReviewBeadDescriptor,
		Mode:            core.WorkflowModeDot,
		ReviewPolicy:    core.ReviewPolicyNoReview,
		SelectionSource: source,
		RawQueueMode:    input.Mode,
		RawQueueRef:     input.Ref,
	}
	if !resolved.Valid() {
		return resolvedWorkflow{}, fmt.Errorf("registered no-review workflow is invalid")
	}
	return resolved, nil
}

func loadResolvedWorkflowGraph(projectDir, workflowRef string, params map[string]string) (*dot.Graph, core.WorkflowSelectionSource, error) {
	if workflowRef != "" {
		path := workflowRef
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectDir, path)
		}
		graph, err := workflow.LoadDotWorkflowWithParams(path, params)
		if err != nil {
			return nil, "", err
		}
		return graph, core.WorkflowSelectionExplicitRef, nil
	}

	defaultPath := filepath.Join(projectDir, "workflow.dot")
	if _, err := os.Stat(defaultPath); err == nil {
		graph, loadErr := workflow.LoadDotWorkflowWithParams(defaultPath, params)
		if loadErr != nil {
			return nil, "", loadErr
		}
		return graph, core.WorkflowSelectionProjectDefault, nil
	} else if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("stat project workflow: %w", err)
	}

	graph, err := loadStandardGraph(params)
	if err != nil {
		return nil, "", err
	}
	return graph, core.WorkflowSelectionEmbeddedDefault, nil
}

func graphDescriptor(graph *dot.Graph) core.WorkflowDescriptor {
	return core.WorkflowDescriptor{
		WorkflowID:      graph.WorkflowID,
		WorkflowVersion: core.WorkflowVersion(graph.Version),
	}
}

func rejectGraphAuthoredReviewPolicy(graph *dot.Graph) error {
	if _, found := graph.UnknownAttrs["review_policy"]; found {
		return fmt.Errorf("workflow graph declares reserved review_policy attribute")
	}
	for _, node := range graph.Nodes {
		if _, found := node.UnknownAttrs["review_policy"]; found {
			return fmt.Errorf("workflow node %q declares reserved review_policy attribute", node.ID)
		}
	}
	for _, edge := range graph.Edges {
		if _, found := edge.UnknownAttrs["review_policy"]; found {
			return fmt.Errorf("workflow edge %q -> %q declares reserved review_policy attribute", edge.FromNodeID, edge.ToNodeID)
		}
	}
	return nil
}

func resolveRunPlan(ctx context.Context, req runPlanRequest) runPlan {
	env := req.Env
	bead := env.BeadRecord
	emit := req.Emit

	plan := runPlan{
		Verdict:      runPlanReady,
		LocalOnly:    env.ItemLocalOnly,
		WorkerTarget: env.ItemWorkerTarget,
	}

	resolved, workflowErr := resolveWorkflow(ctx, env, emit)
	if workflowErr != nil {
		plan.Verdict = runPlanRefusedStartFrom
		plan.Refusal = runPlanRefusal{
			LogLine:      fmt.Sprintf("daemon: workloop: resolve workflow for bead %s: %v (reopening)\n", bead.BeadID, workflowErr),
			ReopenReason: fmt.Sprintf("resolve workflow failed: %v", workflowErr),
			Err:          workflowErr,
		}
		return plan
	}
	plan.Workflow = resolved
	plan.WorkflowRef = resolved.WorkflowRef
	plan.WorkflowMode = resolved.Mode

	if !resolveRunPlanHarness(ctx, req, &plan) {
		return plan
	}
	if !resolveRunPlanPlace(ctx, req, &plan) {
		return plan
	}
	resolveRunPlanHookSocket(req, &plan)
	return plan
}

func resolveRunPlanHarness(ctx context.Context, req runPlanRequest, plan *runPlan) bool {
	env := req.Env
	bead := env.BeadRecord
	beadID := bead.BeadID
	emit := req.Emit

	plan.AgentType = resolveHarnessAgentTypeQuiet(
		bead,
		env.QueueDefaultHarness,
		core.AgentType(""), // node default: a DOT run overrides this per node
		env.DefaultHarness,
	)

	plan.Model, plan.Effort = ResolveModelPreference(
		ctx,
		bead.Labels,
		plan.AgentType,
		env.ProjectCfg,
		emit,
		string(beadID),
	)

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

	if profile != (projectconfig.PiProfileConfig{}) && !hasSingleModelLabel(bead.Labels) {
		plan.Model = profile.Model
	}

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

func resolveRunPlanPlace(ctx context.Context, req runPlanRequest, plan *runPlan) bool {
	env := req.Env
	bead := env.BeadRecord
	beadID := bead.BeadID

	beadBranchCfg, parseErr := parseBranchingSection(bead.Description)
	if parseErr != nil {
		warnBeadBodyParseError(ctx, parseErr)
	}

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

	plan.MergeProtectBranches = env.ProtectBranches
	if plan.ActiveRepo != env.ProjectDir {
		plan.MergeProtectBranches = nil
	}

	branch, branchErr := resolveBranchPlan(ctx, plan.ActiveRepo, string(beadID), beadBranchCfg, env.TargetBranch, parentBeadIDFromRecord(bead))
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

	plan.MergeTarget = plan.BaseBranch
	if plan.MergeTarget == "" {
		plan.MergeTarget = env.TargetBranch
	}

	return true
}

func parentBeadIDFromRecord(bead core.BeadRecord) string {
	for _, edge := range bead.Edges {
		if edge.EdgeKind == core.EdgeKindParentChild && edge.FromBeadID == bead.BeadID {
			return string(edge.ToBeadID)
		}
	}
	return ""
}

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
	plan.Refusal = tunnelRefusal(env.RunID, beadID, *req.PreSelectedWorker, "socket-path", sockPath, lenErr)
}

func tunnelRefusal(runID core.RunID, beadID core.BeadID, worker workers.Worker, stage, sockPath string, cause error) runPlanRefusal {
	return runPlanRefusal{
		LogLine: fmt.Sprintf(
			"daemon: workloop: reverse-tunnel %s bead %s run %s: %v (reopening, not launching)\n",
			stage, beadID, runID.String(), cause),
		ReopenReason: fmt.Sprintf("reverse-tunnel not ready: %v", cause),
		Err:          cause,
		TunnelFailure: &runPlanTunnelFailure{
			RunID:      runID.String(),
			BeadID:     string(beadID),
			WorkerName: worker.Name,
			WorkerHost: worker.Host,
			SocketPath: sockPath,
			Detail:     cause.Error(),
		},
	}
}

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
